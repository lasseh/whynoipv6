//go:build integration

package crawler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/lasseh/whynoipv6/internal/domain"
	"github.com/lasseh/whynoipv6/internal/postgres/pgtest"
)

// TestCommitRejectsNULInDetails pins the root cause of the adsb.lol commit
// failure against real DDL. A NUL in the details payload makes jsonb reject
// the batch with SQLSTATE 22P05, and FlushCommit's rollback then discards the
// domain UPDATE and the scan row too — the whole scan, not just the details.
// This is what checker.sanitizeText and the buildDetails guard prevent; the
// clean half of the test proves the sanitized text commits.
func TestCommitRejectsNULInDetails(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()
	c := NewCommitter(pool, testCommitCfg(false))

	snap := claimOne(t, pool)
	obs := stableObs(domain.DimBase, domain.ObsSupported)
	tt := time.Now().UTC().Truncate(time.Microsecond)

	// Spelled byte-by-byte: a Go literal of the escape is the thing under test.
	nulEsc := string([]byte{'\\', 'u', '0', '0', '0', '0'})
	banner := `220 ` + nulEsc + `Rmail.katia.sh ESMTP`

	_, err := c.Commit(ctx, &CommitInput{
		Snapshot: snap, Obs: obs,
		Attribution: &Attribution{AsnID: snap.AsnID, CountryID: snap.CountryID},
		Details:     []byte(`{"results":{"smtp_ipv6":{"banner":"` + banner + `"}}}`),
		DurationMS:  1234, T: tt,
	})
	if err == nil {
		t.Fatal("commit with a NUL in details succeeded; jsonb should have rejected it")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "22P05" {
		t.Fatalf("want SQLSTATE 22P05, got %v", err)
	}

	// Nothing was written: the rollback discarded the whole unit.
	var scans int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM scan WHERE domain_id = $1`, snap.ID).Scan(&scans); err != nil {
		t.Fatalf("count scans: %v", err)
	}
	if scans != 0 {
		t.Errorf("scan rows = %d after a failed commit, want 0", scans)
	}

	// The sanitized banner — NUL dropped, everything else intact — commits.
	res, err := c.Commit(ctx, &CommitInput{
		Snapshot: snap, Obs: obs,
		Attribution: &Attribution{AsnID: snap.AsnID, CountryID: snap.CountryID},
		Details:     []byte(`{"results":{"smtp_ipv6":{"banner":"220 Rmail.katia.sh ESMTP"}}}`),
		DurationMS:  1234, T: tt,
	})
	if err != nil || res.LeaseLost {
		t.Fatalf("sanitized commit: err=%v leaseLost=%v", err, res.LeaseLost)
	}

	var stored string
	if err := pool.QueryRow(ctx,
		`SELECT details->'results'->'smtp_ipv6'->>'banner' FROM scan_detail WHERE domain_id = $1`,
		snap.ID).Scan(&stored); err != nil {
		t.Fatalf("read back banner: %v", err)
	}
	if stored != "220 Rmail.katia.sh ESMTP" {
		t.Errorf("stored banner = %q", stored)
	}
}
