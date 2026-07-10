//go:build integration

package lock

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lasseh/whynoipv6/internal/postgres/pgtest"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Main(m)) }

// TestLock (04 §17.7): with two pools, exactly one of two simultaneous
// TryRun calls runs fn; the other returns ErrHeld; killing the winner's
// connection frees the lock; Run waits then executes.
func TestLock(t *testing.T) {
	pool1 := pgtest.NewDB(t)
	pool2, err := pgxpool.New(context.Background(), pool1.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool2.Close)
	ctx := context.Background()

	// Two simultaneous TryRun: exactly one runs.
	started := make(chan struct{})
	release := make(chan struct{})
	var ran, held int
	var mu sync.Mutex
	var wg sync.WaitGroup
	body := func(context.Context) error {
		close(started)
		<-release
		return nil
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := TryRun(ctx, pool1, JobDailyTick, body); err == nil {
			mu.Lock()
			ran++
			mu.Unlock()
		}
	}()
	<-started // winner holds the lock
	if err := TryRun(ctx, pool2, JobDailyTick, func(context.Context) error {
		mu.Lock()
		ran++
		mu.Unlock()
		return nil
	}); errors.Is(err, ErrHeld) {
		mu.Lock()
		held++
		mu.Unlock()
	} else {
		t.Errorf("second TryRun = %v, want ErrHeld", err)
	}
	close(release)
	wg.Wait()
	if ran != 1 || held != 1 {
		t.Fatalf("ran=%d held=%d, want 1/1", ran, held)
	}

	// Lock released after fn returns: an immediate TryRun succeeds.
	if err := TryRun(ctx, pool2, JobDailyTick, func(context.Context) error { return nil }); err != nil {
		t.Fatalf("lock not released after fn: %v", err)
	}

	// Run waits out a holder, then executes.
	holderStarted := make(chan struct{})
	holderRelease := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = TryRun(ctx, pool1, JobCampaignSync, func(context.Context) error {
			close(holderStarted)
			<-holderRelease
			return nil
		})
	}()
	<-holderStarted
	go func() { time.Sleep(300 * time.Millisecond); close(holderRelease) }()
	executed := false
	if err := Run(ctx, pool2, JobCampaignSync, 5*time.Second, func(context.Context) error {
		executed = true
		return nil
	}); err != nil || !executed {
		t.Fatalf("Run should wait then execute: executed=%t err=%v", executed, err)
	}
	wg.Wait()

	// Run with an exhausted wait returns the "another X is running" error.
	holderStarted2 := make(chan struct{})
	holderRelease2 := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = TryRun(ctx, pool1, JobTrancoImport, func(context.Context) error {
			close(holderStarted2)
			<-holderRelease2
			return nil
		})
	}()
	<-holderStarted2
	if err := Run(ctx, pool2, JobTrancoImport, 300*time.Millisecond, func(context.Context) error {
		t.Error("fn must not run on wait timeout")
		return nil
	}); err == nil {
		t.Error("Run with exhausted wait must error")
	}
	close(holderRelease2)
	wg.Wait()

	// Killing the winner's connection frees the lock (session-scoped):
	// terminate the holder's backend server-side and observe the lock free.
	victimCtx, victimCancel := context.WithCancel(ctx)
	t.Cleanup(victimCancel)
	victim, err := pgxpool.New(ctx, pool1.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(victim.Close)
	lockHeld := make(chan struct{})
	go func() {
		_ = TryRun(victimCtx, victim, JobDailyTick, func(c context.Context) error {
			close(lockHeld)
			<-c.Done()
			return c.Err()
		})
	}()
	<-lockHeld
	if _, err := pool2.Exec(ctx, `SELECT pg_terminate_backend(pid) FROM pg_locks
		WHERE locktype = 'advisory' AND classid = 60660 AND objid = 1 AND granted`); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		err := TryRun(ctx, pool2, JobDailyTick, func(context.Context) error { return nil })
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("lock not freed after killing the holder's backend")
		}
		time.Sleep(100 * time.Millisecond)
	}
	victimCancel()
}
