package crawler

import (
	"time"

	"github.com/lasseh/whynoipv6/internal/domain"
)

// ScheduleConfig carries the §9/§5 scheduling knobs (registry: 09-ops.md).
type ScheduleConfig struct {
	CadenceDefault      time.Duration // cadence.default, 24h
	RecheckInconsistent time.Duration // recheck_inconsistent, 2h
	RecheckError        time.Duration // recheck_error, 6h
	RecheckBackoffMax   time.Duration // recheck_backoff_max, 720h
	SlowLaneEvery       time.Duration // lifecycle.slow_lane_every, 720h
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
	errorStreak int16, breakerOpen bool, t time.Time,
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
		return t.Add(cfg.CadenceDefault)
	}
}
