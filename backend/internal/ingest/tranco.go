package ingest

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lasseh/whynoipv6/internal/domain"
	"github.com/lasseh/whynoipv6/internal/postgres"
	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// TrancoConfig carries the sanity-guard knobs (registry: 09-ops.md §2.5).
type TrancoConfig struct {
	MinRows      int
	MaxDelistPct float64
}

// TrancoArchive is one downloaded list artifact.
type TrancoArchive struct {
	Zip          []byte
	ETag         string
	LastModified time.Time
	NotModified  bool // HTTP 304: artifact not propagated yet
}

// TrancoSource fetches the list ID and the zip artifact (06-ingest.md §2.2
// steps 2–3). Implemented by HTTPTrancoSource in production and by fakes in
// tests.
type TrancoSource interface {
	ListID(ctx context.Context) (string, error)
	List(ctx context.Context, etag string) (*TrancoArchive, error)
}

// TrancoOutcome is the terminal state of one import attempt.
type TrancoOutcome string

const (
	TrancoImported          TrancoOutcome = "imported"
	TrancoNoNewList         TrancoOutcome = "no_new_list"
	TrancoAbortedPreviously TrancoOutcome = "aborted_previously"
	TrancoNotModified       TrancoOutcome = "not_modified"
	TrancoAborted           TrancoOutcome = "aborted"
)

// TrancoReport is the attempt summary (the five provenance counters).
type TrancoReport struct {
	Outcome        TrancoOutcome
	ListID         string
	ListDate       time.Time
	LineCount      int
	RejectedCount  int
	DuplicateCount int
	ImportedCount  int
	Delisted       int
	Note           string
}

// TrancoImporter runs the import attempt algorithm (06-ingest.md §2.2).
// Serialization by the JobTrancoImport advisory lock is the caller's duty
// (coordinator/v6ctl — internal/lock, P2.10).
type TrancoImporter struct {
	pool *pgxpool.Pool
	src  TrancoSource
	cfg  TrancoConfig
	etag string // process memory only (§2.2 step 3)
}

// NewTrancoImporter builds the importer over one pool, list source and set of
// sanity-guard knobs (06-ingest.md §2.2).
func NewTrancoImporter(pool *pgxpool.Pool, src TrancoSource, cfg TrancoConfig) *TrancoImporter {
	return &TrancoImporter{pool: pool, src: src, cfg: cfg}
}

// The staging statements run as literal SQL: tranco_staging is a session
// temp table sqlc cannot type (05-schema.md §7, §10.2). The DDL text is the
// one owned by 05-schema.md §7 — the only DDL any binary executes at runtime.
const (
	sqlCreateStaging = `CREATE TEMPORARY TABLE tranco_staging (
  rank INT  NOT NULL,
  host TEXT NOT NULL,
  tld  TEXT NOT NULL
) ON COMMIT DROP`

	sqlStagingCounters = `SELECT count(*), count(*) - count(DISTINCT host) FROM tranco_staging`

	sqlWouldDelist = `SELECT count(*) FROM domain d
WHERE d.rank IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM tranco_staging s WHERE s.host = d.host)`

	// The guarded upsert (06-ingest.md §2.2 step 10). $1 = sentinel asn.id,
	// $2 = sentinel country.id.
	sqlUpsert = `INSERT INTO domain (host, rank, next_check_at, created_by, asn_id, country_id, tld)
SELECT DISTINCT ON (s.host)
       s.host,
       s.rank,
       now() + (random() * interval '24 hours'),
       'tranco',
       $1,
       COALESCE(c.id, $2),
       s.tld
FROM tranco_staging s
LEFT JOIN country c ON c.tld = '.' || upper(substring(s.host from '[^.]+$'))
ORDER BY s.host, s.rank ASC
ON CONFLICT (host) DO UPDATE SET
  rank        = excluded.rank,
  orphaned_at = NULL,
  disabled    = CASE WHEN domain.disabled_reason = 'delisted' THEN false ELSE domain.disabled END,
  disabled_reason = CASE WHEN domain.disabled_reason = 'delisted' THEN NULL ELSE domain.disabled_reason END,
  disabled_at = CASE WHEN domain.disabled_reason = 'delisted' THEN NULL ELSE domain.disabled_at END,
  next_check_at = CASE WHEN domain.disabled_reason IN ('delisted','dead') THEN now() ELSE domain.next_check_at END,
  updated_at  = now()
WHERE domain.rank IS DISTINCT FROM excluded.rank
   OR domain.orphaned_at IS NOT NULL
   OR domain.disabled_reason IN ('delisted','dead')`

	sqlDelist = `UPDATE domain d
SET rank = NULL, updated_at = now()
WHERE d.rank IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM tranco_staging s WHERE s.host = d.host)`
)

// Import executes one import attempt. force bypasses the aborted-list
// short-circuit and the sanity guard (06-ingest.md §2.4).
func (ti *TrancoImporter) Import(ctx context.Context, force bool) (*TrancoReport, error) {
	q := db.New(ti.pool)

	listID, err := ti.src.ListID(ctx)
	if err != nil {
		return nil, fmt.Errorf("tranco: fetch list id: %w", err)
	}
	rep := &TrancoReport{ListID: listID}

	latest, err := q.TrancoLatestSuccessListID(ctx)
	switch {
	case err == nil && latest == listID:
		rep.Outcome = TrancoNoNewList
		return rep, nil
	case err != nil && !errors.Is(err, pgx.ErrNoRows):
		return nil, fmt.Errorf("tranco: latest list id: %w", err)
	}
	if !force {
		aborted, err := q.TrancoListWasAborted(ctx, listID)
		if err != nil {
			return nil, fmt.Errorf("tranco: aborted check: %w", err)
		}
		if aborted {
			rep.Outcome = TrancoAbortedPreviously
			return rep, nil
		}
	}

	arch, err := ti.src.List(ctx, ti.etag)
	if err != nil {
		return nil, fmt.Errorf("tranco: download: %w", err)
	}
	if arch.NotModified {
		rep.Outcome = TrancoNotModified
		return rep, nil
	}
	rep.ListDate = arch.LastModified.UTC().Truncate(24 * time.Hour)
	if rep.ListDate.IsZero() {
		rep.ListDate = time.Now().UTC().Truncate(24 * time.Hour)
	}

	rows, lineCount, rejected, err := parseTrancoZip(arch.Zip)
	if err != nil {
		return nil, fmt.Errorf("tranco: parse: %w", err)
	}
	rep.LineCount, rep.RejectedCount = lineCount, rejected

	if err := ti.applyList(ctx, rep, rows, force); err != nil {
		return nil, err
	}
	if rep.Outcome == TrancoImported {
		ti.etag = arch.ETag
	}
	slog.Info("tranco import attempt done",
		"outcome", string(rep.Outcome), "list_id", rep.ListID,
		"line_count", rep.LineCount, "imported_count", rep.ImportedCount,
		"delisted", rep.Delisted, "rejected_count", rep.RejectedCount,
		"duplicate_count", rep.DuplicateCount, "note", rep.Note)
	return rep, nil
}

// applyList runs steps 6–13: stage, count, guard, upsert, delist, provenance.
func (ti *TrancoImporter) applyList(ctx context.Context, rep *TrancoReport, rows []stagedRow, force bool) error {
	tx, err := ti.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("tranco: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)

	if _, err := tx.Exec(ctx, sqlCreateStaging); err != nil {
		return fmt.Errorf("tranco: staging: %w", err)
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"tranco_staging"},
		[]string{"rank", "host", "tld"},
		pgx.CopyFromSlice(len(rows), func(i int) ([]any, error) {
			return []any{rows[i].rank, rows[i].host, rows[i].tld}, nil
		})); err != nil {
		return fmt.Errorf("tranco: copy: %w", err)
	}

	var validRows, duplicates int
	if err := tx.QueryRow(ctx, sqlStagingCounters).Scan(&validRows, &duplicates); err != nil {
		return fmt.Errorf("tranco: counters: %w", err)
	}
	rep.DuplicateCount = duplicates

	rankedCount64, err := q.RankedDomainCount(ctx)
	if err != nil {
		return fmt.Errorf("tranco: ranked count: %w", err)
	}
	rankedCount := int(rankedCount64)
	var wouldDelist int
	if err := tx.QueryRow(ctx, sqlWouldDelist).Scan(&wouldDelist); err != nil {
		return fmt.Errorf("tranco: would-delist: %w", err)
	}

	if !force {
		days := 1.0
		last, err := q.TrancoLastSuccessAt(ctx)
		if err != nil {
			return fmt.Errorf("tranco: last success: %w", err)
		}
		if last.Valid {
			days = time.Since(last.Time).Hours() / 24
		}
		budget := delistBudget(ti.cfg.MaxDelistPct, days)

		var note string
		switch {
		case validRows < ti.cfg.MinRows:
			note = fmt.Sprintf("valid rows %d below tranco.min_rows %d", validRows, ti.cfg.MinRows)
		case rankedCount > 0 && float64(wouldDelist)*100.0/float64(rankedCount) > budget:
			note = fmt.Sprintf("would delist %d of %d ranked rows (> %.1f%% budget after %.1f days stale)",
				wouldDelist, rankedCount, budget, days)
		}
		if note != "" {
			_ = tx.Rollback(ctx)
			rep.Outcome, rep.Note = TrancoAborted, note
			slog.Warn("tranco import aborted", "list_id", rep.ListID, "note", note)
			return db.New(ti.pool).TrancoInsertAborted(ctx, db.TrancoInsertAbortedParams{
				ListID:         rep.ListID,
				ListDate:       postgres.Date(rep.ListDate),
				LineCount:      count32(rep.LineCount),
				RejectedCount:  count32(rep.RejectedCount),
				DuplicateCount: count32(rep.DuplicateCount),
				Note:           &note,
			})
		}
	}

	sentinelASN, err := q.ASNSentinelID(ctx)
	if err != nil {
		return fmt.Errorf("tranco: sentinel asn: %w", err)
	}
	sentinelCountry, err := q.CountrySentinelID(ctx)
	if err != nil {
		return fmt.Errorf("tranco: sentinel country: %w", err)
	}

	tag, err := tx.Exec(ctx, sqlUpsert, sentinelASN, sentinelCountry)
	if err != nil {
		return fmt.Errorf("tranco: upsert: %w", err)
	}
	rep.ImportedCount = int(tag.RowsAffected())

	tag, err = tx.Exec(ctx, sqlDelist)
	if err != nil {
		return fmt.Errorf("tranco: delist: %w", err)
	}
	rep.Delisted = int(tag.RowsAffected())

	if _, err := q.TrancoInsertProvenance(ctx, db.TrancoInsertProvenanceParams{
		ListID:         rep.ListID,
		ListDate:       postgres.Date(rep.ListDate),
		LineCount:      count32(rep.LineCount),
		ImportedCount:  count32(rep.ImportedCount),
		Delisted:       count32(rep.Delisted),
		RejectedCount:  count32(rep.RejectedCount),
		DuplicateCount: count32(rep.DuplicateCount),
	}); err != nil {
		return fmt.Errorf("tranco: provenance: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("tranco: commit: %w", err)
	}
	rep.Outcome = TrancoImported
	return nil
}

// maxDelistCeilingPct caps the scaled delist budget. Waiting long enough
// must not admit a list that is simply broken; --force is the operator
// route past it, and tranco.min_rows still applies underneath.
const maxDelistCeilingPct = 10.0

// delistBudget is the delist allowance for an import that has to absorb
// days worth of churn. tranco.max_delist_pct describes ONE list's normal
// churn (~0.5% in practice), but the comparison runs against a DB frozen
// at the last successful import, which diverges further every day one is
// missed — so an unscaled guard makes the first abort permanent: the gap
// only grows, and no later list can ever pass it. Fractions of a day
// round up to one so a same-day retry gets the plain per-day allowance.
func delistBudget(perDay, days float64) float64 {
	return min(perDay*max(days, 1), maxDelistCeilingPct)
}

// maxTrancoLines bounds the decompressed line count and, since every parsed
// line is staged in memory before COPY, the parse's memory: 2× the ~1M-row
// list (tranco.min_rows already rejects anything below 950k). It also bounds
// the rank value, which narrows to the INT staging column.
const maxTrancoLines = 2_000_000

// maxTrancoLineBytes is the longest CSV line the parser reads whole; a host
// is at most 253 octets, so anything longer is a rejected line, not a
// reason to abort the import.
const maxTrancoLineBytes = 64 * 1024

// stagedRow is one accepted Tranco line, held between the parse and the
// COPY. A compact struct rather than the []any pgx.CopyFromRows takes: at
// ~1M rows the boxed form cost a 72-byte []any plus a heap-boxed int32 per
// line, and this import runs inside the crawler daemon alongside live scan
// workers. CopyFromSlice boxes one row at a time instead.
type stagedRow struct {
	rank      int32
	host, tld string
}

// parseTrancoZip unzips the single inner top-1m.csv and parses rank,domain
// lines (CRLF, no header), canonicalizing hosts and deriving the ICANN tld
// (06-ingest.md §2.2 steps 4–5).
func parseTrancoZip(zipBytes []byte) (rows []stagedRow, lineCount, rejected int, err error) {
	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return nil, 0, 0, fmt.Errorf("unzip: %w", err)
	}
	var csvFile *zip.File
	for _, f := range zr.File {
		if f.Name == "top-1m.csv" {
			csvFile = f
			break
		}
	}
	if csvFile == nil {
		return nil, 0, 0, errors.New("zip has no inner top-1m.csv")
	}
	rc, err := csvFile.Open()
	if err != nil {
		return nil, 0, 0, fmt.Errorf("open inner csv: %w", err)
	}
	defer func() { _ = rc.Close() }()

	// One bounded line at a time: a line longer than the buffer is skipped
	// and counted as rejected (06 §2.2 step 5), not a fatal ErrTooLong that
	// aborts the whole list and retries the same artifact every cycle.
	br := bufio.NewReaderSize(rc, maxTrancoLineBytes)
	for {
		raw, isPrefix, err := br.ReadLine()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, 0, 0, fmt.Errorf("read csv: %w", err)
		}
		if isPrefix {
			for isPrefix {
				if _, isPrefix, err = br.ReadLine(); err != nil {
					break
				}
			}
			lineCount++
			rejected++
			continue
		}
		line := strings.TrimSuffix(string(raw), "\r")
		if line == "" {
			continue
		}
		lineCount++
		if lineCount > maxTrancoLines {
			return nil, 0, 0, fmt.Errorf("inner csv exceeds %d lines", maxTrancoLines)
		}
		rankStr, hostStr, ok := strings.Cut(line, ",")
		if !ok {
			rejected++
			continue
		}
		rank, err := strconv.Atoi(rankStr)
		if err != nil || rank <= 0 || rank > maxTrancoLines {
			rejected++
			continue
		}
		host, err := domain.Canonicalize(hostStr)
		if err != nil {
			rejected++
			slog.Debug("tranco line rejected", "line", line, "err", err.Error())
			continue
		}
		rows = append(rows, stagedRow{
			rank: int32(rank), //nolint:gosec // rank ≤ maxTrancoLines, checked above
			host: host, tld: domain.TLD(host),
		})
	}
	return rows, lineCount, rejected, nil
}

// count32 narrows a report counter for the int32 provenance columns; every
// counter is bounded by maxTrancoLines.
func count32(n int) *int32 {
	v := int32(n) //nolint:gosec // ≤ maxTrancoLines
	return &v
}
