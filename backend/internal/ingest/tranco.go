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
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lasseh/whynoipv6/internal/domain"
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

	sqlRankedCount = `SELECT count(*) FROM domain WHERE rank IS NOT NULL`

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
func (ti *TrancoImporter) applyList(ctx context.Context, rep *TrancoReport, rows [][]any, force bool) error {
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
		[]string{"rank", "host", "tld"}, pgx.CopyFromRows(rows)); err != nil {
		return fmt.Errorf("tranco: copy: %w", err)
	}

	var validRows, duplicates int
	if err := tx.QueryRow(ctx, sqlStagingCounters).Scan(&validRows, &duplicates); err != nil {
		return fmt.Errorf("tranco: counters: %w", err)
	}
	rep.DuplicateCount = duplicates

	var rankedCount, wouldDelist int
	if err := tx.QueryRow(ctx, sqlRankedCount).Scan(&rankedCount); err != nil {
		return fmt.Errorf("tranco: ranked count: %w", err)
	}
	if err := tx.QueryRow(ctx, sqlWouldDelist).Scan(&wouldDelist); err != nil {
		return fmt.Errorf("tranco: would-delist: %w", err)
	}

	if !force {
		var note string
		switch {
		case validRows < ti.cfg.MinRows:
			note = fmt.Sprintf("valid rows %d below tranco.min_rows %d", validRows, ti.cfg.MinRows)
		case rankedCount > 0 && float64(wouldDelist)*100.0/float64(rankedCount) > ti.cfg.MaxDelistPct:
			note = fmt.Sprintf("would delist %d of %d ranked rows (> %.1f%%)", wouldDelist, rankedCount, ti.cfg.MaxDelistPct)
		}
		if note != "" {
			_ = tx.Rollback(ctx)
			rep.Outcome, rep.Note = TrancoAborted, note
			slog.Warn("tranco import aborted", "list_id", rep.ListID, "note", note)
			return db.New(ti.pool).TrancoInsertAborted(ctx, db.TrancoInsertAbortedParams{
				ListID:         rep.ListID,
				ListDate:       pgDate(rep.ListDate),
				LineCount:      ptr(int32(rep.LineCount)),
				RejectedCount:  ptr(int32(rep.RejectedCount)),
				DuplicateCount: ptr(int32(rep.DuplicateCount)),
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
		ListDate:       pgDate(rep.ListDate),
		LineCount:      ptr(int32(rep.LineCount)),
		ImportedCount:  ptr(int32(rep.ImportedCount)),
		Delisted:       ptr(int32(rep.Delisted)),
		RejectedCount:  ptr(int32(rep.RejectedCount)),
		DuplicateCount: ptr(int32(rep.DuplicateCount)),
	}); err != nil {
		return fmt.Errorf("tranco: provenance: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("tranco: commit: %w", err)
	}
	rep.Outcome = TrancoImported
	return nil
}

// parseTrancoZip unzips the single inner top-1m.csv and parses rank,domain
// lines (CRLF, no header), canonicalizing hosts and deriving the ICANN tld
// (06-ingest.md §2.2 steps 4–5).
func parseTrancoZip(zipBytes []byte) (rows [][]any, lineCount, rejected int, err error) {
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
	defer rc.Close()

	sc := bufio.NewScanner(rc)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSuffix(sc.Text(), "\r")
		if line == "" {
			continue
		}
		lineCount++
		rankStr, hostStr, ok := strings.Cut(line, ",")
		if !ok {
			rejected++
			continue
		}
		rank, err := strconv.Atoi(rankStr)
		if err != nil || rank <= 0 {
			rejected++
			continue
		}
		host, err := domain.Canonicalize(hostStr)
		if err != nil {
			rejected++
			slog.Debug("tranco line rejected", "line", line, "err", err.Error())
			continue
		}
		rows = append(rows, []any{int32(rank), host, domain.TLD(host)})
	}
	if err := sc.Err(); err != nil && !errors.Is(err, io.EOF) {
		return nil, 0, 0, fmt.Errorf("scan csv: %w", err)
	}
	return rows, lineCount, rejected, nil
}

func ptr[T any](v T) *T { return &v }

func pgDate(t time.Time) pgtype.Date { return pgtype.Date{Time: t, Valid: true} }
