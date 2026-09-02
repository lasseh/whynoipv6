// Package campaign implements the single campaign-repo sync
// (06-ingest.md §3): YAML parse/validation, uuid-keyed upsert, membership
// diff, uuid-set soft delete, and the bot uuid write-back.
package campaign

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// Config drives one sync run (keys: 09-ops.md §2.6).
type Config struct {
	RepoPath               string
	GitRemote              string
	MaxDomainsPerFile      int
	MaxSubdomainsPerDomain int
	Pull                   bool // git pull --ff-only before parsing (prod: true)
	Push                   bool // commit+push the uuid write-back (prod: true)
}

// Report is the §3.3 step-7 sync report.
type Report struct {
	Created   []string
	Updated   []string
	Renamed   []string
	ReEnabled []string
	Disabled  []string

	MembershipAdds    int
	MembershipRemoves int

	// Curated subdomain lists (subdomains/<apex>.yml). Skipped lists land in
	// RejectedFiles under their repo-relative path.
	CuratedFiles   int  // lists applied
	CuratedAdds    int  // memberships gained
	CuratedRemoves int  // memberships dropped
	CuratedFrozen  bool // a rejected list suspended the removal diff this run

	RejectedFiles map[string]string
	RejectedHosts map[string]string

	WriteBack string // "pushed" | "nothing to push" | "written (push disabled)" | "failed: …"
}

// Sync is the ONE sync implementation (06-ingest.md §3.1) — called by
// v6ctl campaign sync and the crawler daily tick. Serialization by the
// JobCampaignSync advisory lock is the caller's duty (internal/lock, P2.10).
func Sync(ctx context.Context, cfg Config, pool *pgxpool.Pool) (*Report, error) {
	rep := &Report{RejectedFiles: map[string]string{}, RejectedHosts: map[string]string{}}

	// The remote is an argv position git parses: a value starting with "-"
	// is an option, a URL may carry a token that the startup summary and
	// git's own error output would print. Credentials belong in the git
	// config or deploy key, never in the registry value.
	if (cfg.Pull || cfg.Push) && !gitRemoteRe.MatchString(cfg.GitRemote) {
		return nil, fmt.Errorf("campaign sync: campaign.git_remote %q must be a remote name, not a URL or an option", cfg.GitRemote)
	}
	if cfg.Pull {
		if out, err := git(ctx, cfg.RepoPath, "pull", "--ff-only", cfg.GitRemote); err != nil {
			return nil, fmt.Errorf("campaign sync: git pull: %w: %s", err, out)
		}
	}

	// Steps 1–2: parse + duplicate-uuid guard, before the import transaction.
	paths, err := ListYAMLFiles(cfg.RepoPath)
	if err != nil {
		return nil, fmt.Errorf("campaign sync: list files: %w", err)
	}
	var files []*File
	for _, p := range paths {
		f, err := ParseFile(p, cfg.MaxDomainsPerFile)
		if err != nil {
			rep.RejectedFiles[filepath.Base(p)] = err.Error()
			continue
		}
		for raw, reason := range f.RejectedHosts {
			rep.RejectedHosts[f.Path+": "+raw] = reason
		}
		files = append(files, f)
	}
	files = dedupeUUIDs(ctx, pool, files, rep)

	// Steps 3–5 in one import transaction.
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("campaign sync: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	ens, err := newEntityEnsurer(ctx, q)
	if err != nil {
		return nil, err
	}

	fileUUIDs := map[string]bool{} // every uuid present in a repo file this run
	for _, f := range files {
		if f.UUID != "" {
			fileUUIDs[f.UUID] = true
		}
	}

	seenUUIDs := []pgtype.UUID{} // all uuids seen this run (incl. generated); empty (not nil) so an
	// all-files-deleted repo still disables every campaign (NULL array skips all)
	writeBack := map[string]string{} // file path -> generated uuid
	for _, f := range files {
		var campaignID int32
		switch {
		case f.UUID != "":
			row, err := q.CampaignByUUID(ctx, mustUUID(f.UUID))
			switch {
			case err == nil:
				if row.SourceFile != nil && *row.SourceFile != f.Path {
					rep.Renamed = append(rep.Renamed, *row.SourceFile+" → "+f.Path)
					slog.Info("campaign renamed", "old", *row.SourceFile, "new", f.Path)
				}
				if row.Disabled {
					rep.ReEnabled = append(rep.ReEnabled, f.Path)
					slog.Info("campaign re-enabled (file re-appeared)", "file", f.Path)
				}
				campaignID, err = q.CampaignUpdateFromFile(ctx, db.CampaignUpdateFromFileParams{
					Uuid: mustUUID(f.UUID), Name: f.Title, Description: f.Description,
					Tags: f.Tags, SourceFile: &f.Path,
				})
				if err != nil {
					return nil, fmt.Errorf("campaign sync: update %s: %w", f.Path, err)
				}
				rep.Updated = append(rep.Updated, f.Path)
			case !isNoRows(err):
				return nil, fmt.Errorf("campaign sync: lookup uuid %s: %w", f.Path, err)
			default:
				// Unknown uuid: the file is new to this DB, so adopt it.
				// The repo assigns uuids (its own `make fix-uuids`) and PR
				// validation pins them (§4.2), so identity is decided there,
				// not here. Nothing in this path can mint one instead —
				// step 6's write-back needs campaign.push, false wherever
				// the sync runs off a mounted checkout — so rejecting would
				// strand the campaign for good.
				campaignID, err = q.CampaignInsert(ctx, db.CampaignInsertParams{
					Uuid: mustUUID(f.UUID), Name: f.Title, Description: f.Description,
					SourceFile: &f.Path, Tags: f.Tags,
				})
				if err != nil {
					return nil, fmt.Errorf("campaign sync: adopt %s: %w", f.Path, err)
				}
				rep.Created = append(rep.Created, f.Path)
			}
			seenUUIDs = append(seenUUIDs, mustUUID(f.UUID))

		default:
			// Step 4: file without uuid — reuse a failed write-back's uuid.
			newUUID := ""
			if prior, err := q.CampaignUUIDBySourceFile(ctx, &f.Path); err == nil {
				if u := uuidString(prior); !fileUUIDs[u] {
					newUUID = u
				}
			}
			if newUUID == "" {
				newUUID = uuid.NewString()
			}
			writeBack[filepath.Join(cfg.RepoPath, f.Path)] = newUUID
			campaignID, err = upsertNoUUID(ctx, q, f, newUUID)
			if err != nil {
				return nil, fmt.Errorf("campaign sync: insert %s: %w", f.Path, err)
			}
			rep.Created = append(rep.Created, f.Path)
			seenUUIDs = append(seenUUIDs, mustUUID(newUUID))
		}

		if err := syncMembers(ctx, q, ens, campaignID, f, rep); err != nil {
			return nil, err
		}
	}

	// Step 5: uuid-set soft delete.
	disabled, err := q.CampaignDisableAbsent(ctx, seenUUIDs)
	if err != nil {
		return nil, fmt.Errorf("campaign sync: disable absent: %w", err)
	}
	for _, d := range disabled {
		rep.Disabled = append(rep.Disabled, d.Name)
		slog.Info("campaign disabled (file removed)", "name", d.Name, "uuid", uuidString(d.Uuid))
	}

	// Step 5b: curated subdomain lists, same transaction.
	if err := syncSubdomains(ctx, q, ens, cfg, rep); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("campaign sync: commit: %w", err)
	}

	// Step 6: uuid write-back after the import transaction commits.
	rep.WriteBack = writeBackUUIDs(ctx, cfg, writeBack)

	slog.Info("campaign sync done",
		"created", len(rep.Created), "updated", len(rep.Updated),
		"renamed", len(rep.Renamed), "re_enabled", len(rep.ReEnabled),
		"disabled", len(rep.Disabled), "member_adds", rep.MembershipAdds,
		"member_removes", rep.MembershipRemoves,
		"curated_files", rep.CuratedFiles, "curated_adds", rep.CuratedAdds,
		"curated_removes", rep.CuratedRemoves, "curated_frozen", rep.CuratedFrozen,
		"rejected_files", len(rep.RejectedFiles), "rejected_hosts", len(rep.RejectedHosts),
		"write_back", rep.WriteBack)
	return rep, nil
}

// upsertNoUUID inserts a new campaign, or updates in place when the reused
// uuid already exists (a previous write-back push failed after commit).
func upsertNoUUID(ctx context.Context, q *db.Queries, f *File, id string) (int32, error) {
	if cid, err := q.CampaignUpdateFromFile(ctx, db.CampaignUpdateFromFileParams{
		Uuid: mustUUID(id), Name: f.Title, Description: f.Description,
		Tags: f.Tags, SourceFile: &f.Path,
	}); err == nil {
		return cid, nil
	} else if !isNoRows(err) {
		return 0, err
	}
	return q.CampaignInsert(ctx, db.CampaignInsertParams{
		Uuid: mustUUID(id), Name: f.Title, Description: f.Description,
		SourceFile: &f.Path, Tags: f.Tags,
	})
}

// syncMembers diffs one campaign's membership set (§3.3 step 3).
func syncMembers(ctx context.Context, q *db.Queries, ens *entityEnsurer, campaignID int32, f *File, rep *Report) error {
	desired := make([]int64, 0, len(f.Hosts))
	seen := map[int64]bool{}
	for _, host := range f.Hosts {
		id, existed, err := ens.ensure(ctx, host)
		if err != nil {
			rep.RejectedHosts[f.Path+": "+host] = err.Error()
			continue
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		desired = append(desired, id)

		added, err := q.CampaignAddMember(ctx, db.CampaignAddMemberParams{
			CampaignID: campaignID, DomainID: id,
		})
		if err != nil {
			return fmt.Errorf("campaign sync: add member %s: %w", host, err)
		}
		if added > 0 {
			rep.MembershipAdds += int(added)
			if existed {
				// Re-entry rule on membership addition to an existing row.
				if err := q.DomainMembershipReEntry(ctx, id); err != nil {
					return fmt.Errorf("campaign sync: re-entry %s: %w", host, err)
				}
			}
		}
	}
	removed, err := q.CampaignRemoveMembersNotIn(ctx, db.CampaignRemoveMembersNotInParams{
		CampaignID: campaignID, DomainIds: desired,
	})
	if err != nil {
		return fmt.Errorf("campaign sync: remove members: %w", err)
	}
	rep.MembershipRemoves += int(removed)
	return nil
}

// syncSubdomains applies the curated subdomain lists (subdomains/<apex>.yml).
// Membership is the only lifecycle lever this ingress has: it never disables a
// row, it drops the curated_subdomain entry and lets the daily sweep run the
// 30-day grace (04 §8).
func syncSubdomains(ctx context.Context, q *db.Queries, ens *entityEnsurer, cfg Config, rep *Report) error {
	paths, err := ListSubdomainFiles(cfg.RepoPath)
	if err != nil {
		return fmt.Errorf("campaign sync: list subdomain files: %w", err)
	}

	rejected := 0
	lists := make([]*SubdomainFile, 0, len(paths))
	claims := map[string]int{} // apex -> how many files name it
	for _, p := range paths {
		f, err := ParseSubdomainFile(p, cfg.MaxSubdomainsPerDomain)
		if err != nil {
			rep.RejectedFiles[subdomainReportKey(p)] = err.Error()
			rejected++
			continue
		}
		lists = append(lists, f)
		claims[f.Apex]++
	}

	listed := []int64{} // every domain id listed this run; empty (not nil) so an
	// emptied subdomains/ directory drops all membership
	for _, f := range lists {
		// One parent, one file. The canonical-filename rule stops two
		// *spellings* of one apex but not nrk.no.yml beside nrk.no.yaml, and
		// picking a winner by filename order would let a new file quietly
		// supersede an established one. Reject every claimant instead: with
		// removals suspended below, that freezes the apex rather than letting
		// either file win.
		if claims[f.Apex] > 1 {
			rep.RejectedFiles[f.Path] = "apex " + f.Apex + " is listed by more than one file — merge them"
			rejected++
			continue
		}
		// The apex must already be tracked: this ingress adds subdomains to the
		// index, it is not a side door for new apexes.
		parent, err := q.DomainByHost(ctx, f.Apex)
		switch {
		case isNoRows(err):
			rep.RejectedFiles[f.Path] = "apex " + f.Apex + " is not tracked — it must enter via Tranco or a campaign first"
			continue
		case err != nil:
			return fmt.Errorf("campaign sync: lookup apex %s: %w", f.Apex, err)
		case parent.Disabled:
			// Skip means leave it alone. Its hosts stay in the membership set,
			// or the removal below would start their delist grace as a side
			// effect of an operator disabling the parent.
			rep.RejectedFiles[f.Path] = "apex " + f.Apex + " is disabled"
			keep, err := q.CuratedSubdomainIDsByParent(ctx, &parent.ID)
			if err != nil {
				return fmt.Errorf("campaign sync: listed children of %s: %w", f.Apex, err)
			}
			listed = append(listed, keep...)
			continue
		}
		ids, err := applySubdomainFile(ctx, q, ens, f, parent.ID, rep)
		if err != nil {
			return err
		}
		listed = append(listed, ids...)
		rep.CuratedFiles++
	}

	// A rejected file leaves its hosts out of `listed`, so running the diff
	// would unlist them over a typo and start a 30-day grace. Removals wait
	// until the repo reads clean again. The two skips above are not rejections:
	// an unknown apex owns no rows, and a disabled one keeps the rows it has.
	if rejected > 0 {
		rep.CuratedFrozen = true
		slog.Warn("curated subdomain removals skipped: some lists were rejected",
			"rejected_files", rejected)
		return nil
	}
	removed, err := q.CuratedSubdomainRemoveNotIn(ctx, listed)
	if err != nil {
		return fmt.Errorf("campaign sync: remove curated subdomains: %w", err)
	}
	rep.CuratedRemoves = int(removed)
	return nil
}

// applySubdomainFile ensures one list's child rows and their membership,
// returning the domain ids it listed.
func applySubdomainFile(ctx context.Context, q *db.Queries, ens *entityEnsurer, f *SubdomainFile, parentID int64, rep *Report) ([]int64, error) {
	_, tld, err := PSLParse(f.Apex)
	if err != nil {
		return nil, fmt.Errorf("campaign sync: psl %s: %w", f.Apex, err)
	}
	ids := make([]int64, 0, len(f.Hosts))
	for _, host := range f.Hosts {
		// created_by='curated' only sticks on rows this ingress creates; a host
		// already known from another ingress keeps its origin and just gains
		// membership.
		id, existed, err := ens.ensureRow(ctx, host, "subdomain", &parentID, "curated", tld)
		if err != nil {
			return nil, fmt.Errorf("campaign sync: ensure %s: %w", host, err)
		}
		if existed {
			// ensureRow leaves an existing row's shape alone, so a host the
			// live check created before its apex was tracked still has
			// parent_id NULL — which would keep it off the very page this
			// feature exists to fill. An existing apex row (a Tranco-ranked
			// host under a private-section suffix) keeps its kind (06 §3.4
			// step 3b): relinking would flip it to a subdomain.
			row, err := q.DomainByHost(ctx, host)
			if err != nil {
				return nil, fmt.Errorf("campaign sync: re-read %s: %w", host, err)
			}
			if row.Kind == db.DomainKindApex {
				rep.RejectedHosts[f.Path+": "+host] = "already tracked as an apex; not listed as a subdomain"
				continue
			}
			if _, err := q.DomainLinkParent(ctx, db.DomainLinkParentParams{
				ID: id, ParentID: &parentID,
			}); err != nil {
				return nil, fmt.Errorf("campaign sync: link %s to parent: %w", host, err)
			}
		}
		ids = append(ids, id)

		added, err := q.CuratedSubdomainAdd(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("campaign sync: add curated %s: %w", host, err)
		}
		if added > 0 {
			rep.CuratedAdds += int(added)
			if existed {
				// Re-entry rule: a delisted row re-listed by a file comes back.
				if err := q.DomainMembershipReEntry(ctx, id); err != nil {
					return nil, fmt.Errorf("campaign sync: re-entry %s: %w", host, err)
				}
			}
		}
	}
	return ids, nil
}

// dedupeUUIDs applies the §3.3 step-2 duplicate-uuid guard.
func dedupeUUIDs(ctx context.Context, pool *pgxpool.Pool, files []*File, rep *Report) []*File {
	byUUID := map[string][]*File{}
	for _, f := range files {
		if f.UUID != "" {
			byUUID[f.UUID] = append(byUUID[f.UUID], f)
		}
	}
	q := db.New(pool)
	reject := map[string]bool{}
	for id, group := range byUUID {
		if len(group) == 1 {
			continue
		}
		row, err := q.CampaignByUUID(ctx, mustUUID(id))
		keep := ""
		if err == nil && row.SourceFile != nil {
			keep = *row.SourceFile
		}
		kept := false
		for _, f := range group {
			if f.Path == keep && !kept {
				kept = true
				continue
			}
			reject[f.Path] = true
			rep.RejectedFiles[f.Path] = "duplicate uuid " + id
		}
	}
	out := files[:0]
	for _, f := range files {
		if !reject[f.Path] {
			out = append(out, f)
		}
	}
	return out
}

// writeBackUUIDs inserts generated uuid lines and makes the single bot
// commit (§3.3 step 6). A file that could not be written is reported, not
// folded into "nothing to push".
func writeBackUUIDs(ctx context.Context, cfg Config, pending map[string]string) string {
	if len(pending) == 0 {
		return "nothing to push"
	}
	changed := false
	var failures []string
	for path, id := range pending {
		ok, err := insertUUIDLine(path, id)
		if err != nil {
			slog.Warn("uuid write-back failed", "file", path, "err", err.Error())
			failures = append(failures, filepath.Base(path)+": "+err.Error())
			continue
		}
		changed = changed || ok
	}
	report := func(s string) string {
		if len(failures) > 0 {
			return "failed: write " + strings.Join(failures, "; ") + "; " + s
		}
		return s
	}
	if !changed {
		return report("nothing to push")
	}
	if !cfg.Push {
		return report("written (push disabled)")
	}
	if out, err := git(ctx, cfg.RepoPath, "commit", "-am", "chore: assign campaign uuids [skip ci]"); err != nil {
		return report("failed: commit: " + strings.TrimSpace(out))
	}
	if out, err := git(ctx, cfg.RepoPath, "push", cfg.GitRemote); err != nil {
		// Non-fast-forward: rebase and retry once.
		if _, err := git(ctx, cfg.RepoPath, "pull", "--rebase", cfg.GitRemote); err == nil {
			if _, err := git(ctx, cfg.RepoPath, "push", cfg.GitRemote); err == nil {
				return report("pushed")
			}
		}
		slog.Warn("uuid write-back push failed", "err", strings.TrimSpace(out))
		return report("failed: push: " + strings.TrimSpace(out))
	}
	return report("pushed")
}

// yamlIndented reports a continuation line of a block scalar or wrapped value.
func yamlIndented(l string) bool { return strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t") }

// insertUUIDLine adds `uuid: <id>` after the description key (and its block
// continuation lines, blank lines inside a block scalar included), or fills
// an empty `uuid:` key in place, preserving the rest of the file
// byte-for-byte. The result is parsed before it is written: a splice that
// would not read back as the uuid is an error, never a committed file.
func insertUUIDLine(path, id string) (bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	lines := strings.SplitAfter(string(raw), "\n")
	insertAt := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "uuid:") {
			if strings.TrimSpace(strings.TrimPrefix(l, "uuid:")) != "" {
				return false, nil // already assigned
			}
			// `uuid:` with no value (the make fix-uuids placeholder): fill it.
			lines[i] = "uuid: " + id + "\n"
			return true, writeCheckedYAML(path, strings.Join(lines, ""), id)
		}
		if insertAt < 0 && strings.HasPrefix(l, "description:") {
			insertAt = i + 1
			for insertAt < len(lines) {
				if yamlIndented(lines[insertAt]) {
					insertAt++
					continue
				}
				if strings.TrimSpace(lines[insertAt]) == "" {
					// A blank line is part of a block scalar when an indented
					// line follows it; otherwise the value ended here.
					j := insertAt + 1
					for j < len(lines) && strings.TrimSpace(lines[j]) == "" {
						j++
					}
					if j < len(lines) && yamlIndented(lines[j]) {
						insertAt = j
						continue
					}
				}
				break
			}
		}
	}
	if insertAt < 0 {
		return false, fmt.Errorf("no description: line in %s", path)
	}
	out := strings.Join(lines[:insertAt], "") + "uuid: " + id + "\n" + strings.Join(lines[insertAt:], "")
	return true, writeCheckedYAML(path, out, id)
}

// writeCheckedYAML writes the spliced file only if it decodes with the uuid
// the splice meant to add.
func writeCheckedYAML(path, content, id string) error {
	var probe struct {
		UUID string `yaml:"uuid"`
	}
	if err := yaml.Unmarshal([]byte(content), &probe); err != nil || probe.UUID != id {
		return fmt.Errorf("uuid splice would corrupt %s (got uuid %q): file left unchanged", filepath.Base(path), probe.UUID)
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// gitTimeout bounds every git call the sync makes; the tick runs them under
// a context with no deadline while holding two advisory locks.
const gitTimeout = 2 * time.Minute

// gitRemoteRe is the shape of a remote name: never a leading "-", never a
// URL with userinfo.
var gitRemoteRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)

func git(ctx context.Context, repo string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	// SIGTERM lets git release its index lock; the default Kill can leave
	// .git/index.lock behind and wedge every later pull.
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 5 * time.Second
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func mustUUID(s string) pgtype.UUID {
	u := uuid.MustParse(s)
	return pgtype.UUID{Bytes: u, Valid: true}
}

func uuidString(u pgtype.UUID) string {
	return uuid.UUID(u.Bytes).String()
}

func isNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
