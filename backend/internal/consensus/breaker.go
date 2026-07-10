package consensus

import (
	"context"
	"fmt"
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
	min := time.Now().Unix() / 60
	b := w.buckets[min]
	if b == nil {
		b = &bucket{}
		w.buckets[min] = b
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
	for min, b := range w.buckets {
		if min < cutoff {
			delete(w.buckets, min)
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
	if total >= r.cfg.FastLane.MinSamples && float64(bad)/float64(total) > r.cfg.FastLane.NondefinitiveRate {
		r.mu.Lock()
		r.fastOpen = true
		r.mu.Unlock()
		msg := fmt.Sprintf("consensus fast-lane breaker OPEN: nondefinitive rate %.3f over %s (n=%d)",
			float64(bad)/float64(total), r.cfg.FastLane.Window, total)
		r.logger.Warn("fastlane breaker open", "rate", float64(bad)/float64(total), "n", total)
		r.alert(ctx, msg)
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
		case <-r.stop:
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Fast-lane close: rate below recover_below over the trailing full
	// window; an idle window (total == 0) counts as recovered.
	r.mu.Lock()
	fastOpen := r.fastOpen
	r.mu.Unlock()
	if fastOpen {
		total, bad := r.fastWindow.counts()
		if total == 0 || float64(bad)/float64(total) < r.cfg.FastLane.RecoverBelow {
			r.mu.Lock()
			r.fastOpen = false
			r.mu.Unlock()
			r.logger.Info("fastlane breaker closed")
			r.alert(ctx, "consensus fast-lane breaker closed")
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

	ctx, cancel := context.WithTimeout(context.Background(), perProviderBudget+5*time.Second)
	defer cancel()
	o := r.queryProvider(ctx, p, canaryName)

	r.mu.Lock()
	defer r.mu.Unlock()
	if !validSymbol(o.symbol) {
		r.canaryOK = 0
		return
	}
	r.canaryOK++
	if r.canaryOK >= r.cfg.Provider.RecoveryProbes {
		r.dropped = ""
		r.canaryOK = 0
		p.window.reset()
		r.logger.Info("provider restored", "provider", p.name)
		go r.alert(context.Background(), "consensus provider restored: "+p.name)
	}
}
