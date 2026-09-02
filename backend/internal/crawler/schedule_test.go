package crawler

import (
	"testing"
	"time"

	"github.com/lasseh/whynoipv6/internal/domain"
)

// TestSchedule (04 §17.3): the two backoff progressions, lane choice,
// breaker-open behavior, slow-lane override.
func TestSchedule(t *testing.T) {
	cfg := testCommitCfg().Schedule

	errWant := []time.Duration{6, 12, 24, 48, 96, 192, 384, 720, 720, 720}
	incWant := []time.Duration{2, 4, 8, 16, 32, 64, 128, 256, 512, 720}
	for i := range 10 {
		streak := int16(i + 1)
		if got := backoff(cfg.RecheckError, cfg.RecheckBackoffMax, streak); got != errWant[i]*time.Hour {
			t.Errorf("error lane streak %d = %v, want %vh", streak, got, errWant[i])
		}
		if got := backoff(cfg.RecheckInconsistent, cfg.RecheckBackoffMax, streak); got != incWant[i]*time.Hour {
			t.Errorf("inconsistent lane streak %d = %v, want %vh", streak, got, incWant[i])
		}
	}

	// Inconsistent beats error in lane choice.
	next := schedule(cfg, false, domain.ObsError, domain.ObsInconsistent, 1, false, seqT0)
	if next.Sub(seqT0) != 2*time.Hour {
		t.Errorf("mixed lanes = %v, want 2h (inconsistent wins)", next.Sub(seqT0))
	}

	// Breaker open: cadence lane despite non-definitive.
	next = schedule(cfg, false, domain.ObsError, domain.ObsSupported, 3, true, seqT0)
	if next.Sub(seqT0) != 24*time.Hour {
		t.Errorf("breaker-open = %v, want cadence 24h", next.Sub(seqT0))
	}

	// Disabled slow-lane override beats everything.
	next = schedule(cfg, true, domain.ObsError, domain.ObsError, 5, false, seqT0)
	if next.Sub(seqT0) != 720*time.Hour {
		t.Errorf("disabled = %v, want 720h slow lane", next.Sub(seqT0))
	}

	// Non-consensus dims never pull in: definitive base+www → cadence.
	next = schedule(cfg, false, domain.ObsSupported, domain.ObsSupported, 0, false, seqT0)
	if next.Sub(seqT0) != 24*time.Hour {
		t.Errorf("definitive = %v, want cadence 24h", next.Sub(seqT0))
	}
}
