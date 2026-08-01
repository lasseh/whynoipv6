package crawler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lasseh/whynoipv6/internal/domain"
	"github.com/lasseh/whynoipv6/internal/postgres"
)

// fakeCommitter builds a Committer over a fake flush adapter — the second
// adapter at the flush seam, so Commit's orchestration (fence outcome,
// result mapping) is unit-testable without Postgres.
func fakeCommitter(t *testing.T, flush func(context.Context, *postgres.CommitUnit) (bool, error)) *Committer {
	t.Helper()
	return &Committer{flush: flush, cfg: testCommitCfg(false)}
}

func commitInput(t *testing.T) *CommitInput {
	t.Helper()
	m := newMachine(t)
	return &CommitInput{
		Snapshot: m.s, Obs: stableObs(domain.DimBase, domain.ObsSupported),
		Attribution: &Attribution{AsnID: 1, CountryID: 1},
		T:           seqT0.Add(24 * time.Hour),
	}
}

func TestCommitLeaseLost(t *testing.T) {
	var got *postgres.CommitUnit
	c := fakeCommitter(t, func(_ context.Context, u *postgres.CommitUnit) (bool, error) {
		got = u
		return true, nil
	})
	res, err := c.Commit(context.Background(), commitInput(t))
	if err != nil {
		t.Fatal(err)
	}
	if !res.LeaseLost || len(res.Transitions) != 0 || res.Bootstraps != 0 {
		t.Errorf("lease-lost result = %+v, want bare LeaseLost", res)
	}
	// The fence input travels inside the typed unit.
	if got.Domain.Lease.Time != seqT0 || !got.Domain.Lease.Valid {
		t.Errorf("unit lease = %+v, want claim time %v", got.Domain.Lease, seqT0)
	}
}

func TestCommitFlushError(t *testing.T) {
	boom := errors.New("boom")
	c := fakeCommitter(t, func(context.Context, *postgres.CommitUnit) (bool, error) {
		return false, boom
	})
	if _, err := c.Commit(context.Background(), commitInput(t)); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
}

func TestCommitSuccessMapsUnit(t *testing.T) {
	c := fakeCommitter(t, func(context.Context, *postgres.CommitUnit) (bool, error) {
		return false, nil
	})
	res, err := c.Commit(context.Background(), commitInput(t))
	if err != nil || res.LeaseLost {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	// A first definitive scan bootstraps every core dimension.
	if res.Bootstraps == 0 {
		t.Error("bootstraps = 0, want > 0")
	}
}
