package crawler

import (
	"fmt"
	"time"
)

// ConfigSource is the consumer-side view of the config registry — satisfied
// by *config.Config without importing it.
type ConfigSource interface {
	String(key string) string
	Int(key string) int
	Bool(key string) bool
	Duration(key string) time.Duration
	UnmarshalKey(key string, out any) error
}

// The ConfigFrom constructors below bind the crawler's registry keys
// (09-ops §2.2, §2.4, §2.7) to their config structs — the one place each
// key name meets its field; cmd wiring only assembles.

// ScheduleConfigFrom binds the cadence/recheck keys.
func ScheduleConfigFrom(src ConfigSource) ScheduleConfig {
	return ScheduleConfig{
		CadenceDefault:      src.Duration("cadence.default"),
		RecheckInconsistent: src.Duration("recheck_inconsistent"),
		RecheckError:        src.Duration("recheck_error"),
		RecheckBackoffMax:   src.Duration("recheck_backoff_max"),
		SlowLaneEvery:       src.Duration("lifecycle.slow_lane_every"),
	}
}

// BandsFrom decodes and validates the YAML-only cadence.bands key. It is
// the one binder that can fail: a malformed band must abort startup
// (04 §4 Decision), so it stays out of ScheduleConfigFrom's error-free
// signature and cmd wiring assigns the result.
func BandsFrom(src ConfigSource) ([]Band, error) {
	var bands []Band
	if err := src.UnmarshalKey("cadence.bands", &bands); err != nil {
		return nil, fmt.Errorf("cadence.bands: %w", err)
	}
	if err := ValidateBands(bands); err != nil {
		return nil, err
	}
	return bands, nil
}

// CommitConfigFrom binds the commit machine's keys (schedule included).
func CommitConfigFrom(src ConfigSource) *CommitConfig {
	return &CommitConfig{
		MinConfirmSpacing: src.Duration("anti_flap.min_confirm_spacing"),
		DeadStreak:        int16(src.Int("lifecycle.dead_streak")),
		ResourcesEnabled:  src.Bool("crawler.resources.enabled"),
		Schedule:          ScheduleConfigFrom(src),
	}
}

// FrontierConfigFrom binds the claim-loop keys.
func FrontierConfigFrom(src ConfigSource) FrontierConfig {
	return FrontierConfig{
		BatchSize:     src.Int("claim.batch_size"),
		Order:         src.String("claim.order"),
		EmptyPoll:     src.Duration("claim.empty_poll_interval"),
		WorkerSlots:   src.Int("worker_slots"),
		RetryInterval: src.Duration("preflight.retry_interval"),
	}
}

// TickConfigFrom binds the daily-tick keys (lifecycle sweep + service
// detection + live-check retention).
func TickConfigFrom(src ConfigSource) TickConfig {
	return TickConfig{
		Sweep: SweepConfig{
			LiveCheckLinkage: src.Duration("lifecycle.live_check_linkage"),
			DelistGrace:      src.Duration("lifecycle.delist_grace"),
			SlowLaneEvery:    src.Duration("lifecycle.slow_lane_every"),
		},
		IndegreeThreshold:  int32(src.Int("service_detect.indegree_threshold")),
		LiveCheckRetention: src.Duration("live_check.retention"),
	}
}

// LiveCheckConfigFrom binds the check-job consumer keys.
func LiveCheckConfigFrom(src ConfigSource) LiveCheckConfig {
	return LiveCheckConfig{
		Workers:          src.Int("live_check.workers"),
		JobBudget:        src.Duration("live_check.job_budget"),
		ReclaimAfter:     src.Duration("live_check.reclaim_after"),
		FailAfter:        src.Duration("live_check.fail_after"),
		ResourcesEnabled: src.Bool("crawler.resources.enabled"),
	}
}
