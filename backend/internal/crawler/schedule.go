package crawler

import (
	"fmt"
	"time"

	"github.com/lasseh/whynoipv6/internal/domain"
)

// Band is one cadence band (config cadence.bands; 04 §4).
type Band struct {
	MinRank int32         // 0 = no lower bound
	MaxRank int32         // 0 = no upper bound
	Every   time.Duration // > 0
}

// ScheduleConfig carries the §9/§5 scheduling knobs (registry: 09-ops.md).
type ScheduleConfig struct {
	CadenceDefault      time.Duration // cadence.default, 24h
	Bands               []Band        // cadence.bands, YAML-only
	RecheckInconsistent time.Duration // recheck_inconsistent, 2h
	RecheckError        time.Duration // recheck_error, 6h
	RecheckBackoffMax   time.Duration // recheck_backoff_max, 720h
	SlowLaneEvery       time.Duration // lifecycle.slow_lane_every, 720h
}

// ValidateBands is the fail-fast startup validation (04 §4 Decision).
func ValidateBands(bands []Band) error {
	for i, b := range bands {
		if b.MinRank == 0 && b.MaxRank == 0 {
			return fmt.Errorf("cadence.bands[%d]: at least one bound required", i)
		}
		if b.Every <= 0 {
			return fmt.Errorf("cadence.bands[%d]: every must be > 0", i)
		}
		if b.MinRank != 0 && b.MaxRank != 0 && b.MinRank > b.MaxRank {
			return fmt.Errorf("cadence.bands[%d]: min_rank > max_rank", i)
		}
	}
	return nil
}

// cadence returns the base re-check interval for a domain (04 §4).
// rank == nil (unranked rows) always uses Default — bands never match a
// NULL rank. Bands are evaluated in config order; the FIRST match wins.
func cadence(rank *int32, def time.Duration, bands []Band) time.Duration {
	if rank != nil {
		for _, b := range bands {
			if (b.MinRank == 0 || *rank >= b.MinRank) &&
				(b.MaxRank == 0 || *rank <= b.MaxRank) {
				return b.Every
			}
		}
	}
	return def
}

// backoff computes the recheck pull-in interval (04 §5.1). streak is the
// post-increment value (>= 1 here).
func backoff(lane, maxBackoff time.Duration, streak int16) time.Duration {
	if streak >= 10 {
		return maxBackoff // overflow guard
	}
	d := lane * (1 << (streak - 1))
	if d > maxBackoff {
		return maxBackoff
	}
	return d
}

// schedule computes next_check_at per the §5.1 lane selection (first match
// wins), using T as the arithmetic base for determinism. disabled is the
// post-dead-trigger value; errorStreak is the post-step-1 value.
func schedule(cfg ScheduleConfig, disabled bool, baseObs, wwwObs domain.Observation,
	errorStreak int16, rank *int32, breakerOpen bool, t time.Time,
) time.Time {
	baseND := !baseObs.Definitive()
	wwwND := !wwwObs.Definitive()

	switch {
	case disabled:
		return t.Add(cfg.SlowLaneEvery)
	case (baseND || wwwND) && !breakerOpen:
		lane := cfg.RecheckError
		if baseObs == domain.ObsInconsistent || wwwObs == domain.ObsInconsistent {
			lane = cfg.RecheckInconsistent // inconsistent wins over error
		}
		return t.Add(backoff(lane, cfg.RecheckBackoffMax, errorStreak))
	default: // definitive base+www, OR breaker open
		return t.Add(cadence(rank, cfg.CadenceDefault, cfg.Bands))
	}
}
