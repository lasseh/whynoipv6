package checker

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

// Engine-internal concurrency constants (compile-time, not config — distinct
// from the crawler-level WORKER_SLOTS constant, 00-overview.md).
const (
	domainTimeout    = 90 * time.Second // whole-scan budget per domain
	concurrencyLimit = 6                // concurrent checks within one domain
)

// Config carries the engine's few runtime-configurable knobs.
type Config struct {
	MaxNSLookups            int  // checks.max_ns_lookups, default 4
	MaxMXLookups            int  // checks.max_mx_lookups, default 5
	EnableResourceDiscovery bool // crawler.resources.enabled, default true
}

// Runner executes a set of checks against a host.
type Runner struct {
	checkers []Checker
	dialer   *SafeDialer
	logger   *slog.Logger
}

// NewRunner creates a runner with all standard checks registered.
// The two AAAA checks take the consensus seam, not the dialer — they only
// resolve, never dial (01-engine.md §10.1).
func NewRunner(cfg Config, aaaa AAAAResolver, dialer *SafeDialer, logger *slog.Logger) *Runner {
	r := &Runner{dialer: dialer, logger: logger}
	r.Register(NewDNSAAAABase(aaaa))
	r.Register(NewDNSAAAAWWW(aaaa))
	r.Register(NewDNSNSIPv6(dialer, cfg.MaxNSLookups))
	r.Register(NewDNSMXIPv6(dialer, cfg.MaxMXLookups))
	r.Register(NewDNSSEC(dialer))
	r.Register(NewHTTPIPv6(dialer))
	r.Register(NewHTTPSIPv6(dialer))
	r.Register(NewTLSIPv6(dialer))
	r.Register(NewResponseParity(dialer))
	if cfg.EnableResourceDiscovery {
		r.Register(NewResourceDiscovery(dialer))
	}
	r.Register(NewSMTPIPv6(dialer))
	r.Register(NewSPFIPv6(dialer))
	r.Register(NewDNSPTRIPv6(dialer))
	r.Register(NewLatencyIPv4(dialer))
	r.Register(NewLatencyIPv6(dialer))
	return r
}

// Register adds a checker to the runner.
func (r *Runner) Register(c Checker) {
	r.checkers = append(r.checkers, c)
}

// Run executes all checks for a host using two-phase execution
// (01-engine.md §10.2).
func (r *Runner) Run(ctx context.Context, host string, kind Kind) ScanResult {
	start := time.Now()
	domainCtx, cancel := context.WithTimeout(ctx, domainTimeout)
	defer cancel()

	results := &sync.Map{}

	// Subdomain www skip: forced not_applicable, check excluded from phase 1.
	wwwSkipped := kind == KindSubdomain
	if wwwSkipped {
		results.Store(NameDNSAAAAWWW, Result{
			Status: StatusNotApplicable,
			Detail: &CommonDetail{Reason: "subdomain entity: www check not applicable"},
		})
	}

	// Phase 1: independent checks. latency_ipv4 is deliberately NOT here
	// (design §2.8 C — moved to phase 2 behind the hasAAAA gate).
	phase1Names := map[string]bool{
		NameDNSAAAABase: true,
		NameDNSAAAAWWW:  !wwwSkipped,
		NameDNSNS:       true,
		NameDNSMX:       true,
		NameDNSSEC:      true,
		NameSPF:         true,
	}

	var phase1Checkers []Checker
	var phase2Checkers []Checker
	for _, c := range r.checkers {
		switch {
		case c.Name() == NameDNSAAAAWWW && wwwSkipped:
			// already stored; excluded from both phases
		case phase1Names[c.Name()]:
			phase1Checkers = append(phase1Checkers, c)
		default:
			phase2Checkers = append(phase2Checkers, c)
		}
	}

	r.runPhase(domainCtx, host, kind, phase1Checkers, results)

	// Phase 2: dependent checks (conditional).
	baseResult := r.getResult(results, NameDNSAAAABase)
	wwwResult := r.getResult(results, NameDNSAAAAWWW)
	mxResult := r.getResult(results, NameDNSMX)

	// Web checks run if either base or www has AAAA. (For subdomains the
	// stored not_applicable www result makes this depend on base alone.)
	hasAAAA := baseResult.Status == StatusSupported || wwwResult.Status == StatusSupported

	var toRun []Checker
	skipReasons := map[string]string{}

	for _, c := range phase2Checkers {
		switch c.Name() {
		case NameHTTP, NameHTTPS, NameTLS, NameLatencyV6, NameResourceDiscovery, NameLatencyV4:
			if !hasAAAA {
				skipReasons[c.Name()] = reasonNoAAAARecord
				continue
			}
			toRun = append(toRun, c)

		case NamePTR:
			// PTR needs apex IPs specifically.
			if baseResult.Status != StatusSupported {
				skipReasons[c.Name()] = reasonNoAAAARecord
				continue
			}
			toRun = append(toRun, c)

		case NameParity:
			if !hasAAAA {
				skipReasons[c.Name()] = reasonNoAAAARecord
				continue
			}
			// Also need A records — check is done inside the checker itself.
			toRun = append(toRun, c)

		case NameSMTP:
			if mxResult.Status != StatusSupported && mxResult.Status != StatusPartial {
				skipReasons[c.Name()] = reasonNoMXWithAAAA
				continue
			}
			toRun = append(toRun, c)

		default:
			toRun = append(toRun, c)
		}
	}

	// Record skipped checks.
	for name, reason := range skipReasons {
		results.Store(name, Result{
			Status: StatusNotApplicable,
			Detail: &CommonDetail{Reason: reason},
		})
	}

	r.runPhase(domainCtx, host, kind, toRun, results)

	// Collect results.
	finalResults := make(map[string]Result)
	results.Range(func(key, value any) bool {
		finalResults[key.(string)] = value.(Result)
		return true
	})

	return ScanResult{
		Domain:    host,
		Results:   finalResults,
		ScannedAt: start,
		Duration:  time.Since(start),
	}
}

// runPhase executes a set of checkers concurrently with a limit.
func (r *Runner) runPhase(ctx context.Context, host string, kind Kind, checkers []Checker, results *sync.Map) {
	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(concurrencyLimit)

	for _, c := range checkers {
		g.Go(func() error {
			r.runCheck(gCtx, host, kind, c, results)
			return nil
		})
	}

	_ = g.Wait()
}

// runCheck runs a single check with panic recovery.
func (r *Runner) runCheck(ctx context.Context, host string, kind Kind, c Checker, results *sync.Map) {
	start := time.Now()

	defer func() {
		if rec := recover(); rec != nil {
			r.logger.Error("check panicked",
				"domain", host,
				"check", c.Name(),
				"panic", fmt.Sprintf("%v", rec),
			)
			results.Store(c.Name(), Result{
				Status:  StatusError,
				Detail:  &CommonDetail{Error: fmt.Sprintf("internal error: %v", rec)},
				Latency: time.Since(start),
			})
		}
	}()

	if ctx.Err() != nil {
		// This check never ran: the domain budget expired while it waited for
		// one of the concurrencyLimit slots. That is the starvation review
		// issue 68 asked about, and it was previously silent — the result
		// carries "scan cancelled" but the scan_detail payload keeps only the
		// status, so nothing downstream could name the check that lost.
		//
		// It should not fire. Worst case both phases run to their declared
		// timeouts: phase 1 is 6 checks under a limit of 6 (30s, all
		// parallel) and phase 2 is 9 under 6, whose worst-order makespan is
		// 40s — 70s against a 90s domainTimeout. If this line appears in
		// production, that arithmetic is wrong somewhere and the check name
		// says where to look.
		r.logger.Warn("check starved: domain budget expired before it ran",
			"domain", host,
			"check", c.Name(),
			"err", ctx.Err().Error(),
		)
		results.Store(c.Name(), Result{
			Status: StatusError,
			Detail: &CommonDetail{Error: "scan cancelled"},
		})
		return
	}

	result, err := c.Check(ctx, host, kind)
	if err != nil {
		r.logger.Error("check failed",
			"domain", host,
			"check", c.Name(),
			"err", err.Error(),
			"duration", time.Since(start),
		)
		results.Store(c.Name(), Result{
			Status:  StatusError,
			Detail:  &CommonDetail{Error: err.Error()},
			Latency: time.Since(start),
		})
		return
	}

	attrs := []any{
		"domain", host,
		"check", c.Name(),
		"status", result.Status,
		"duration", result.Latency,
	}
	if result.Status == StatusError && result.Detail != nil {
		if errMsg := result.Detail.common().Error; errMsg != "" {
			attrs = append(attrs, "err", errMsg)
		}
	}
	r.logger.Debug("check completed", attrs...)
	results.Store(c.Name(), result)
}

func (r *Runner) getResult(results *sync.Map, name string) Result {
	val, ok := results.Load(name)
	if !ok {
		return Result{Status: StatusError}
	}
	return val.(Result)
}
