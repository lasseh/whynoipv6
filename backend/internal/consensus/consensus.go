// Package consensus implements the multi-resolver quorum wrapper
// (02-observation-model.md §2): the two classification-critical AAAA lookups
// fan out to Cloudflare/Google/Quad9, reduce to symbols, and require a
// 2-of-3 quorum. Nothing on this path is ever cached.
package consensus

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/miekg/dns"

	"github.com/lasseh/whynoipv6/internal/checker"
)

// The three pinned provider names (§2.2). They are the identity of a provider
// everywhere it is reported: breaker state and log attrs all key off these
// exact strings.
const (
	providerCloudflare = "cloudflare"
	providerGoogle     = "google"
	providerQuad9      = "quad9"
)

// providerDef is one pinned public resolver network (§2.2 — not config).
type providerDef struct {
	name      string
	upstreams []string
}

var providerDefs = []providerDef{
	{name: providerCloudflare, upstreams: []string{"1.1.1.1:53", "[2606:4700:4700::1111]:53"}},
	{name: providerGoogle, upstreams: []string{"8.8.8.8:53", "[2001:4860:4860::8888]:53"}},
	{name: providerQuad9, upstreams: []string{"9.9.9.9:53", "[2620:fe::fe]:53"}},
}

// Package constants (§2.3, §2.10 — deliberately not config).
const (
	perAttemptTimeout = 2 * time.Second
	canaryInterval    = 5 * time.Minute
	canaryName        = "one.one.one.one"
)

// perProviderBudget is 2s × (1 attempt + 1 retry); a var only so tests can
// shrink the timeout rows (§2.3 Decision).
var perProviderBudget = 2 * perAttemptTimeout

// Reduced per-resolver symbols and lookup-outcome tokens (§2.4, §2.7, §2.7b).
const (
	symExists   = "exists"
	symEmpty    = "empty"
	symNXDomain = "nxdomain"
	symTimeout  = "timeout"
	symError    = "error"
)

// Config mirrors the consensus.* config keys (registry: 09-ops.md).
type Config struct {
	PerProviderQPS int
	FastLane       FastLaneConfig
	Provider       ProviderConfig
}

// FastLaneConfig mirrors consensus.fastlane_breaker.*.
type FastLaneConfig struct {
	NondefinitiveRate float64
	Window            time.Duration
	MinSamples        int
	RecoverBelow      float64
}

// ProviderConfig mirrors consensus.provider_breaker.*.
type ProviderConfig struct {
	FailureRate    float64
	Window         time.Duration
	MinSamples     int
	RecoveryProbes int
}

type providerState struct {
	name    string
	res     *checker.Resolver
	limiter *rate.Limiter
	window  *window
}

// Resolver implements checker.AAAAResolver with quorum, rate control, and
// the fast-lane/provider breakers.
type Resolver struct {
	cfg    Config
	bulk   *checker.Resolver
	alert  func(ctx context.Context, msg string)
	logger *slog.Logger

	providers []*providerState // fixed order

	mu         sync.Mutex
	dropped    string // name of the dropped provider, "" = none
	canaryOK   int    // consecutive canary successes for the dropped provider
	fastOpen   bool
	fastWindow *window

	// life is the maintenance goroutine's lifetime. Close cancels it, so
	// maintain stops between ticks AND an in-flight webhook POST or canary
	// probe — which derive their deadlines from it — aborts with the
	// process instead of holding Close for its full timeout.
	life     context.Context
	stopLife context.CancelFunc
	wg       sync.WaitGroup
}

// New builds the consensus resolver over the pinned provider table. bulk is
// the shared bulk checker.Resolver (Unbound upstreams) used ONLY for the
// conditional A and CD=1 lookups. alert posts one-line messages to the ops
// webhook.
func New(cfg Config, bulk *checker.Resolver, alert func(ctx context.Context, msg string), logger *slog.Logger) *Resolver {
	return newWithProviders(cfg, providerDefs, bulk, alert, logger)
}

// newWithProviders is New with the provider table injected — the seam the
// package tests point at loopback fakes. Substituting providerState.res
// after the fact instead would skip the SetAttemptTimeout below, so the
// attempt/retry split inside perProviderBudget would never be exercised.
func newWithProviders(cfg Config, defs []providerDef, bulk *checker.Resolver,
	alert func(ctx context.Context, msg string), logger *slog.Logger,
) *Resolver {
	r := &Resolver{
		cfg:        cfg,
		bulk:       bulk,
		alert:      alert,
		logger:     logger,
		fastWindow: newWindow(cfg.FastLane.Window),
	}
	r.life, r.stopLife = context.WithCancel(context.Background())
	for _, def := range defs {
		res := checker.NewResolver(def.upstreams)
		// Cap each attempt at perAttemptTimeout so perProviderBudget (2×)
		// genuinely covers one attempt plus one retry (§2.3): without it a
		// hanging first attempt eats the whole budget and the retry never runs.
		res.SetAttemptTimeout(perAttemptTimeout)
		r.providers = append(r.providers, &providerState{
			name:    def.name,
			res:     res,
			limiter: rate.NewLimiter(rate.Limit(cfg.PerProviderQPS), cfg.PerProviderQPS),
			window:  newWindow(cfg.Provider.Window),
		})
	}
	r.wg.Add(1)
	go r.maintain()
	return r
}

// Close stops the canary and window-maintenance goroutine.
func (r *Resolver) Close() {
	r.stopLife()
	r.wg.Wait()
}

// FastLaneSuppressed reports whether the fast-lane breaker is open (§2.9).
func (r *Resolver) FastLaneSuppressed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.fastOpen
}

// providerOutcome is one provider's reduced result for one lookup.
type providerOutcome struct {
	name   string
	symbol string // exists|empty|nxdomain|timeout|error
	rcode  string // raw rcode string; "" when no DNS response arrived
	ips    []net.IP
	cnames []string
	ttl    int
}

func validSymbol(s string) bool { return s == symExists || s == symEmpty || s == symNXDomain }

// LookupAAAA implements checker.AAAAResolver (§2.3–§2.7b).
func (r *Resolver) LookupAAAA(ctx context.Context, name string) (checker.AAAAAnswer, error) {
	active := r.activeProviders()
	outcomes := make([]providerOutcome, len(active))

	var wg sync.WaitGroup
	for i, p := range active {
		wg.Go(func() {
			outcomes[i] = r.queryProvider(ctx, p, name)
		})
	}
	wg.Wait()

	// Record provider-breaker samples (failure = non-answer).
	for i, p := range active {
		p.window.add(!validSymbol(outcomes[i].symbol))
	}

	qi := &checker.QuorumInfo{
		PerResolver: map[string]string{},
		Rcodes:      map[string]string{},
	}
	votes := map[string]int{}
	nValid := 0
	for _, o := range outcomes {
		qi.PerResolver[o.name] = o.symbol
		qi.Rcodes[o.name] = o.rcode
		if validSymbol(o.symbol) {
			votes[o.symbol]++
			nValid++
		}
	}
	nActive := len(active)

	quorum := ""
	for sym, n := range votes {
		if n >= 2 {
			quorum = sym
		}
	}

	switch {
	case quorum != "":
		nMatching := votes[quorum]
		qi.Agreement = fmt.Sprintf("%dof%d", nMatching, nActive)
		for _, o := range outcomes {
			if validSymbol(o.symbol) && o.symbol != quorum {
				qi.Disagreed = true
			}
		}
		r.recordFastLane(ctx, true)

		ans := r.selectAnswer(outcomes, quorum)
		ans.Quorum = qi
		if quorum == symEmpty {
			ans.AOutcome, ans.AIP = r.classifyA(ctx, name)
		}
		return ans, nil

	case nValid >= 2:
		// ≥2 valid answers, no two agree → no quorum → inconsistent.
		qi.Agreement = fmt.Sprintf("0of%d", nActive)
		r.recordFastLane(ctx, false)
		return checker.AAAAAnswer{Quorum: qi}, checker.ErrQuorumInconsistent

	default:
		// ≤1 valid answer → quorum unavailable → plain error, except the
		// broken-DNSSEC signature, which triggers the CD=1 rescue (§2.7b).
		qi.Agreement = fmt.Sprintf("0of%d", nActive)
		r.recordFastLane(ctx, false)
		if nValid == 0 && allServfailOrRefused(outcomes) {
			return r.rescueCD(ctx, name, qi, nValid, nActive)
		}
		return checker.AAAAAnswer{Quorum: qi},
			fmt.Errorf("aaaa consensus for %s: %d valid answers from %d providers", name, nValid, nActive)
	}
}

func (r *Resolver) activeProviders() []*providerState {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*providerState, 0, len(r.providers))
	for _, p := range r.providers {
		if p.name != r.dropped {
			out = append(out, p)
		}
	}
	return out
}

// queryProvider runs one provider's lookup under its token + budget (§2.3).
func (r *Resolver) queryProvider(ctx context.Context, p *providerState, name string) providerOutcome {
	if err := p.limiter.Wait(ctx); err != nil {
		return providerOutcome{name: p.name, symbol: symError, rcode: ""}
	}
	pctx, cancel := context.WithTimeout(ctx, perProviderBudget)
	defer cancel()

	ips, cnames, ttl, rcode, err := p.res.LookupAAAA(pctx, name)
	o := providerOutcome{name: p.name, rcode: rcode, ips: ips, cnames: cnames, ttl: ttl}
	o.symbol = reduce(ips, rcode, err)
	return o
}

// reduce classifies one provider's LookupAAAA result (§2.4). The first three
// symbols are valid answers; timeout/error never vote.
func reduce(ips []net.IP, rcode string, err error) string {
	switch {
	case err != nil && isTimeoutErr(err):
		return symTimeout
	case err != nil:
		// Transport error, or SERVFAIL (the lifted LookupAAAA converts
		// SERVFAIL to an error) — non-answer.
		return symError
	case rcode == checker.RcodeNXDomain:
		return symNXDomain
	case rcode == checker.RcodeNoError && len(routableOnly(ips)) > 0:
		return symExists
	case rcode == checker.RcodeNoError:
		return symEmpty
	default:
		// REFUSED, NOTIMP, FORMERR, ... — non-answer, never `empty`.
		return symError
	}
}

func isTimeoutErr(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func routableOnly(ips []net.IP) []net.IP {
	out := make([]net.IP, 0, len(ips))
	for _, ip := range ips {
		if checker.IsGloballyRoutableIPv6(ip) {
			out = append(out, ip)
		}
	}
	return out
}

// selectAnswer returns the ENTIRE answer of the first provider in fixed
// order whose reduced symbol equals the quorum symbol — record sets are
// never merged (§2.6).
func (r *Resolver) selectAnswer(outcomes []providerOutcome, quorum string) checker.AAAAAnswer {
	for _, o := range outcomes {
		if o.symbol != quorum {
			continue
		}
		return checker.AAAAAnswer{
			IPs:        routableOnly(o.ips),
			CNAMEChain: o.cnames,
			TTL:        o.ttl,
			Rcode:      o.rcode,
		}
	}
	return checker.AAAAAnswer{} // unreachable: quorum implies a matching outcome
}

// classifyA is the conditional bulk-resolver A lookup (§2.7), fired only on
// a NOERROR-empty AAAA quorum. Not quorumed; no token bucket (local path).
// The first A address is captured as the v4-only attribution input IP
// (06-ingest.md §6.2 step 2).
func (r *Resolver) classifyA(ctx context.Context, name string) (outcome string, aip net.IP) {
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(name), dns.TypeA)
	msg.RecursionDesired = true
	msg.SetEdns0(4096, false)
	resp, err := r.bulk.QueryWithRetry(ctx, msg)
	switch {
	case err != nil:
		return checker.AOutcomeError, nil
	case resp.Rcode == dns.RcodeSuccess:
		for _, rr := range resp.Answer {
			if a, ok := rr.(*dns.A); ok {
				return checker.AOutcomePresent, a.A
			}
		}
		return checker.AOutcomeAbsent, nil
	case resp.Rcode == dns.RcodeNameError:
		return checker.AOutcomeAbsent, nil // NXDOMAIN contradicting the AAAA NOERROR → domain's favor
	default:
		return checker.AOutcomeError, nil
	}
}

// allServfailOrRefused detects the broken-DNSSEC signature (§2.7b): at least
// one explicit SERVFAIL/REFUSED rcode, every non-empty rcode in that set,
// and no valid answer (checked by the caller).
func allServfailOrRefused(outcomes []providerOutcome) bool {
	sawExplicit := false
	for _, o := range outcomes {
		if o.rcode == "" {
			continue // timeout/transport — does not qualify, but does not disqualify
		}
		if o.rcode != checker.RcodeServfail && o.rcode != checker.RcodeRefused {
			return false
		}
		sawExplicit = true
	}
	return sawExplicit
}

// rescueCD issues the single CD=1 (checking-disabled) AAAA re-query through
// the bulk resolver (§2.7b — broken-DNSSEC rescue).
func (r *Resolver) rescueCD(ctx context.Context, name string, qi *checker.QuorumInfo, nValid, nActive int) (checker.AAAAAnswer, error) {
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(name), dns.TypeAAAA)
	msg.RecursionDesired = true
	msg.CheckingDisabled = true
	msg.SetEdns0(4096, false)

	resp, err := r.bulk.QueryWithRetry(ctx, msg)
	if err != nil || resp == nil || (resp.Rcode != dns.RcodeSuccess && resp.Rcode != dns.RcodeNameError) {
		return checker.AAAAAnswer{Quorum: qi, CDOutcome: checker.CDOutcomeFail},
			fmt.Errorf("aaaa consensus for %s: %d valid answers from %d providers", name, nValid, nActive)
	}

	var ips []net.IP
	for _, rr := range resp.Answer {
		if aaaa, ok := rr.(*dns.AAAA); ok {
			ips = append(ips, aaaa.AAAA)
		}
	}
	routable := routableOnly(ips)
	if len(routable) > 0 {
		return checker.AAAAAnswer{IPs: routable, Rcode: checker.RcodeNoError, CDOutcome: checker.CDOutcomePresent, Quorum: qi}, nil
	}
	outcome, aip := r.classifyA(ctx, name)
	return checker.AAAAAnswer{
		Rcode: checker.RcodeNoError, CDOutcome: checker.CDOutcomeEmpty,
		AOutcome: outcome, AIP: aip, Quorum: qi,
	}, nil
}
