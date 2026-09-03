package consensus

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
)

// window is a rolling counter ring of one-minute buckets (§2.9/§2.10).
// Buckets expire lazily by timestamp; no ticker needed for counting.
type window struct {
	mu      sync.Mutex
	span    time.Duration
	buckets map[int64]*bucket // key: unix minute
}

type bucket struct{ total, bad int }

func newWindow(span time.Duration) *window {
	return &window{span: span, buckets: map[int64]*bucket{}}
}

func (w *window) add(bad bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	minute := time.Now().Unix() / 60
	b := w.buckets[minute]
	if b == nil {
		b = &bucket{}
		w.buckets[minute] = b
	}
	b.total++
	if bad {
		b.bad++
	}
}

// counts returns (total, bad) over the trailing window and prunes expired
// buckets.
func (w *window) counts() (total, bad int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	cutoff := time.Now().Add(-w.span).Unix() / 60
	for minute, b := range w.buckets {
		if minute < cutoff {
			delete(w.buckets, minute)
			continue
		}
		total += b.total
		bad += b.bad
	}
	return total, bad
}

func (w *window) reset() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buckets = map[int64]*bucket{}
}

// round3 trims a rate to three decimals. The raw quotient ships as
// 0.05263157894736842, which reads as noise next to a 0.05 threshold.
func round3(f float64) float64 { return math.Round(f*1000) / 1000 }

// providerNonAnswers renders each provider's non-answer count over its own
// breaker window (§2.10). The fast-lane rate says the quorum is degraded;
// this says whether one resolver is responsible or all three answered and
// merely disagreed — the first thing an operator needs on a breaker page.
func (r *Resolver) providerNonAnswers() string {
	parts := make([]string, 0, len(r.providers))
	for _, p := range r.providers {
		total, bad := p.window.counts()
		parts = append(parts, fmt.Sprintf("%s %d/%d", p.name, bad, total))
	}
	return strings.Join(parts, " ")
}

// recordFastLane records one consensus-lookup outcome and opens the
// fast-lane breaker when the non-definitive rate crosses the threshold
// (§2.9). Closing is evaluated by the maintenance ticker.
func (r *Resolver) recordFastLane(ctx context.Context, definitive bool) {
	r.fastWindow.add(!definitive)

	r.mu.Lock()
	open := r.fastOpen
	r.mu.Unlock()
	if open {
		return
	}
	total, bad := r.fastWindow.counts()
	rate := float64(bad) / float64(total)
	if total >= r.cfg.FastLane.MinSamples && rate > r.cfg.FastLane.NondefinitiveRate {
		// Re-check under the lock: many lookups cross the threshold in the
		// same instant, and only the one that flips the breaker alerts.
		r.mu.Lock()
		flipped := !r.fastOpen
		r.fastOpen = true
		r.fastOpenedAt = time.Now()
		r.mu.Unlock()
		if !flipped {
			return
		}
		providers := r.providerNonAnswers()
		r.logger.Warn("fastlane breaker open: 2h/6h recheck pull-ins suspended, non-definitive scans fall back to cadence(rank)",
			"nondefinitive", bad,
			"samples", total,
			"rate", round3(rate),
			"threshold", r.cfg.FastLane.NondefinitiveRate,
			"window", r.cfg.FastLane.Window.String(),
			"provider_nonanswers", providers)
		r.alert(ctx, fmt.Sprintf(
			"consensus fast-lane breaker OPEN: %.3f non-definitive over %s (n=%d, threshold %.3f) "+
				"— recheck pull-ins suspended; provider non-answers: %s",
			rate, r.cfg.FastLane.Window, total, r.cfg.FastLane.NondefinitiveRate, providers))
	}
}

// maintain runs the 1-minute breaker-evaluation ticker and the 5-minute
// provider canary (§2.9/§2.10). Stopped by Close.
func (r *Resolver) maintain() {
	defer r.wg.Done()
	evalTick := time.NewTicker(time.Minute)
	canaryTick := time.NewTicker(canaryInterval)
	defer evalTick.Stop()
	defer canaryTick.Stop()

	for {
		select {
		case <-r.life.Done():
			return
		case <-evalTick.C:
			r.evaluateBreakers()
		case <-canaryTick.C:
			r.runCanary()
		}
	}
}

// evaluateBreakers closes the fast lane on recovery and drops a provider
// whose failure rate crossed the threshold.
func (r *Resolver) evaluateBreakers() {
	ctx, cancel := context.WithTimeout(r.life, 10*time.Second)
	defer cancel()

	// Fast-lane close: rate below recover_below over the trailing full
	// window; an idle window (total == 0) counts as recovered.
	r.mu.Lock()
	fastOpen := r.fastOpen
	r.mu.Unlock()
	if fastOpen {
		total, bad := r.fastWindow.counts()
		rate := 0.0
		if total > 0 {
			rate = float64(bad) / float64(total)
		}
		// Both arms log the same "closed", and they mean opposite things:
		// the rate genuinely fell, or nothing was sampled at all. An idle
		// window still counts as recovered (§2.9), but a bare "closed" left
		// an operator unable to tell a recovery from a stalled crawler.
		reason := "rate recovered"
		if total == 0 {
			reason = "no lookups in window"
		}
		if total == 0 || rate < r.cfg.FastLane.RecoverBelow {
			r.mu.Lock()
			openFor := time.Since(r.fastOpenedAt).Round(time.Second)
			r.fastOpen = false
			r.mu.Unlock()
			r.logger.Info("fastlane breaker closed: recheck pull-ins resumed",
				"reason", reason,
				"nondefinitive", bad,
				"samples", total,
				"rate", round3(rate),
				"recover_below", r.cfg.FastLane.RecoverBelow,
				"window", r.cfg.FastLane.Window.String(),
				"open_for", openFor.String())
			r.alert(ctx, fmt.Sprintf(
				"consensus fast-lane breaker closed after %s (%s: %.3f non-definitive over %s, n=%d) "+
					"— recheck pull-ins resumed",
				openFor, reason, rate, r.cfg.FastLane.Window, total))
		}
	}

	// Provider breaker: at most one provider dropped at a time (§2.10).
	for _, p := range r.providers {
		total, bad := p.window.counts()
		if total < r.cfg.Provider.MinSamples || float64(bad)/float64(total) <= r.cfg.Provider.FailureRate {
			continue
		}
		r.mu.Lock()
		switch r.dropped {
		case "":
			r.dropped = p.name
			r.canaryOK = 0
			r.mu.Unlock()
			r.logger.Warn("provider dropped", "provider", p.name)
			r.alert(ctx, fmt.Sprintf("consensus provider breaker: dropped %s (failure rate %.2f over %s)",
				p.name, float64(bad)/float64(total), r.cfg.Provider.Window))
		case p.name:
			r.mu.Unlock() // already dropped
		default:
			other := r.dropped
			r.mu.Unlock()
			r.logger.Warn("second provider over failure threshold; not dropping",
				"provider", p.name, "already_dropped", other)
			r.alert(ctx, fmt.Sprintf(
				"consensus: second provider %s over failure threshold while %s is dropped — NOT dropping; investigate",
				p.name, other))
		}
	}
}

// runCanary probes the dropped provider; recovery_probes consecutive valid
// answers restore it with cleared window counters (§2.10).
func (r *Resolver) runCanary() {
	r.mu.Lock()
	dropped := r.dropped
	r.mu.Unlock()
	if dropped == "" {
		return
	}
	var p *providerState
	for _, cand := range r.providers {
		if cand.name == dropped {
			p = cand
		}
	}
	if p == nil {
		return
	}

	ctx, cancel := context.WithTimeout(r.life, perProviderBudget+5*time.Second)
	defer cancel()
	o := r.queryProvider(ctx, p, canaryName)

	r.mu.Lock()
	restored := false
	if validSymbol(o.symbol) {
		r.canaryOK++
		if r.canaryOK >= r.cfg.Provider.RecoveryProbes {
			r.dropped = ""
			r.canaryOK = 0
			p.window.reset()
			restored = true
		}
	} else {
		r.canaryOK = 0
	}
	r.mu.Unlock()
	if restored {
		// Alert from the maintenance goroutine itself, outside the lock and
		// under the probe's deadline, so Close never leaves a sender behind.
		r.logger.Info("provider restored", "provider", p.name)
		r.alert(ctx, "consensus provider restored: "+p.name)
	}
}
