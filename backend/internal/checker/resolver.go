package checker

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

const (
	dnsTimeout     = 5 * time.Second
	maxCNAMEHops   = 10
	defaultUDPSize = 4096
)

// defaultUpstreams are the DNS resolvers used when none are configured.
// Includes both IPv4 and IPv6 addresses for Google, Cloudflare, and Quad9.
var defaultUpstreams = []string{
	"8.8.8.8:53", "1.1.1.1:53", "9.9.9.9:53",
	"[2001:4860:4860::8888]:53", "[2606:4700:4700::1111]:53", "[2620:fe::fe]:53",
}

// Resolver performs DNS queries using miekg/dns with configurable upstreams.
// There is deliberately NO in-process cache: Unbound is the cache
// (01-engine.md §6.2 — do not add one back).
type Resolver struct {
	upstreams      []string
	attemptTimeout time.Duration // per-attempt cap; dnsTimeout when zero
	mu             sync.Mutex    // guards upstream rotation
	idx            int
}

// NewResolver creates a Resolver with the given upstream servers.
// If none are provided, defaults to Google, Cloudflare, and Quad9.
func NewResolver(upstreams []string) *Resolver {
	if len(upstreams) == 0 {
		upstreams = defaultUpstreams
	}
	return &Resolver{upstreams: upstreams}
}

// SetAttemptTimeout caps each individual query attempt at d (default
// dnsTimeout). With a caller context spanning QueryWithRetry, this keeps a
// hanging first attempt from consuming the whole budget so the retry still
// has time to run. Call before first use — not safe concurrently.
func (r *Resolver) SetAttemptTimeout(d time.Duration) { r.attemptTimeout = d }

// nextUpstream returns the next upstream in round-robin order.
func (r *Resolver) nextUpstream() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	upstream := r.upstreams[r.idx%len(r.upstreams)]
	r.idx++
	return upstream
}

// Query sends a DNS query and returns the response.
// It tries UDP first, falling back to TCP on truncation.
func (r *Resolver) Query(ctx context.Context, msg *dns.Msg) (*dns.Msg, error) {
	upstream := r.nextUpstream()

	timeout := dnsTimeout
	if r.attemptTimeout > 0 {
		timeout = r.attemptTimeout
	}
	deadline, ok := ctx.Deadline()
	if ok {
		remaining := time.Until(deadline)
		if remaining < timeout {
			timeout = remaining
		}
	}
	if timeout <= 0 {
		return nil, context.DeadlineExceeded
	}

	client := &dns.Client{
		Net:     "udp",
		Timeout: timeout,
		UDPSize: defaultUDPSize,
	}

	resp, _, err := client.ExchangeContext(ctx, msg, upstream)
	if err != nil {
		return nil, fmt.Errorf("dns query failed: %w", err)
	}

	// Retry with TCP if truncated.
	if resp.Truncated {
		client.Net = "tcp"
		resp, _, err = client.ExchangeContext(ctx, msg, upstream)
		if err != nil {
			return nil, fmt.Errorf("dns tcp fallback failed: %w", err)
		}
	}

	return resp, nil
}

// QueryWithRetry sends a DNS query, retrying once on transport error,
// SERVFAIL, or REFUSED. The retry lands on the next upstream via the
// round-robin in Query.
func (r *Resolver) QueryWithRetry(ctx context.Context, msg *dns.Msg) (*dns.Msg, error) {
	resp, err := r.Query(ctx, msg)
	if err != nil || (resp != nil && (resp.Rcode == dns.RcodeServerFailure || resp.Rcode == dns.RcodeRefused)) {
		resp2, err2 := r.Query(ctx, msg)
		if err2 != nil {
			if err != nil {
				return nil, err // return original error
			}
			return resp, nil // return original rcode response
		}
		resp = resp2
	}
	return resp, nil
}

// setEDNS0 adds an EDNS0 OPT record to the query if not already present,
// advertising the default UDP buffer size. This avoids unnecessary TCP
// fallback on responses larger than the 512-byte legacy limit.
func setEDNS0(msg *dns.Msg) {
	if msg.IsEdns0() == nil {
		msg.SetEdns0(defaultUDPSize, false)
	}
}

// LookupAAAA resolves AAAA records for the given FQDN.
// It follows CNAME chains up to maxCNAMEHops.
func (r *Resolver) LookupAAAA(ctx context.Context, name string) (retIPs []net.IP, retCNAMEs []string, retTTL int, retRcode string, retErr error) {
	fqdn := dns.Fqdn(name)
	var ttl uint32

	msg := new(dns.Msg)
	msg.SetQuestion(fqdn, dns.TypeAAAA)
	msg.RecursionDesired = true
	setEDNS0(msg)

	resp, err := r.QueryWithRetry(ctx, msg)
	if err != nil {
		return nil, nil, 0, "", fmt.Errorf("aaaa lookup for %s: %w", name, err)
	}

	rcode := dns.RcodeToString[resp.Rcode]

	if resp.Rcode == dns.RcodeServerFailure {
		return nil, nil, 0, rcode, fmt.Errorf("SERVFAIL for %s", name)
	}
	if resp.Rcode == dns.RcodeNameError {
		return nil, nil, 0, rcode, nil // NXDOMAIN
	}

	var ips []net.IP
	var cnameChain []string
	for _, rr := range resp.Answer {
		switch v := rr.(type) {
		case *dns.AAAA:
			ips = append(ips, v.AAAA)
			if ttl == 0 || v.Hdr.Ttl < ttl {
				ttl = v.Hdr.Ttl
			}
		case *dns.CNAME:
			cnameChain = append(cnameChain, v.Target)
		}
	}

	// If we only got CNAMEs but no AAAA, follow the chain.
	if len(ips) == 0 && len(cnameChain) > 0 {
		target := cnameChain[len(cnameChain)-1]
		seen := make(map[string]bool)
		for hop := 0; hop < maxCNAMEHops && len(ips) == 0; hop++ {
			if seen[target] {
				break // CNAME loop detected
			}
			seen[target] = true
			msg2 := new(dns.Msg)
			msg2.SetQuestion(dns.Fqdn(target), dns.TypeAAAA)
			msg2.RecursionDesired = true
			setEDNS0(msg2)

			resp2, err2 := r.QueryWithRetry(ctx, msg2)
			if err2 != nil {
				break
			}
			var nextTarget string
			for _, rr := range resp2.Answer {
				switch v := rr.(type) {
				case *dns.AAAA:
					ips = append(ips, v.AAAA)
					if ttl == 0 || v.Hdr.Ttl < ttl {
						ttl = v.Hdr.Ttl
					}
				case *dns.CNAME:
					nextTarget = v.Target
					cnameChain = append(cnameChain, v.Target)
				}
			}
			if nextTarget == "" {
				break
			}
			target = nextTarget
		}
	}

	return ips, cnameChain, int(ttl), rcode, nil
}

// LookupA resolves A records for the given name.
func (r *Resolver) LookupA(ctx context.Context, name string) ([]net.IP, error) {
	fqdn := dns.Fqdn(name)

	msg := new(dns.Msg)
	msg.SetQuestion(fqdn, dns.TypeA)
	msg.RecursionDesired = true
	setEDNS0(msg)

	resp, err := r.QueryWithRetry(ctx, msg)
	if err != nil {
		return nil, fmt.Errorf("a lookup for %s: %w", name, err)
	}

	if resp.Rcode != dns.RcodeSuccess {
		return nil, nil
	}

	var ips []net.IP
	for _, rr := range resp.Answer {
		if a, ok := rr.(*dns.A); ok {
			ips = append(ips, a.A)
		}
	}
	return ips, nil
}

// LookupNS resolves NS records for the given name.
func (r *Resolver) LookupNS(ctx context.Context, name string) ([]string, error) {
	fqdn := dns.Fqdn(name)

	msg := new(dns.Msg)
	msg.SetQuestion(fqdn, dns.TypeNS)
	msg.RecursionDesired = true
	setEDNS0(msg)

	resp, err := r.QueryWithRetry(ctx, msg)
	if err != nil {
		return nil, fmt.Errorf("ns lookup for %s: %w", name, err)
	}

	if resp.Rcode != dns.RcodeSuccess {
		return nil, fmt.Errorf("ns lookup for %s returned %s", name, dns.RcodeToString[resp.Rcode])
	}

	var nameservers []string
	for _, rr := range resp.Answer {
		if ns, ok := rr.(*dns.NS); ok {
			nameservers = append(nameservers, ns.Ns)
		}
	}
	return nameservers, nil
}

// LookupMX resolves MX records for the given name, sorted by preference.
func (r *Resolver) LookupMX(ctx context.Context, name string) ([]*dns.MX, string, error) {
	fqdn := dns.Fqdn(name)

	msg := new(dns.Msg)
	msg.SetQuestion(fqdn, dns.TypeMX)
	msg.RecursionDesired = true
	setEDNS0(msg)

	resp, err := r.QueryWithRetry(ctx, msg)
	if err != nil {
		return nil, "", fmt.Errorf("mx lookup for %s: %w", name, err)
	}

	rcode := dns.RcodeToString[resp.Rcode]
	if resp.Rcode != dns.RcodeSuccess {
		return nil, rcode, nil
	}

	var records []*dns.MX
	for _, rr := range resp.Answer {
		if mx, ok := rr.(*dns.MX); ok {
			records = append(records, mx)
		}
	}
	return records, rcode, nil
}

// LookupTXT resolves TXT records for the given name.
func (r *Resolver) LookupTXT(ctx context.Context, name string) ([]string, error) {
	fqdn := dns.Fqdn(name)

	msg := new(dns.Msg)
	msg.SetQuestion(fqdn, dns.TypeTXT)
	msg.RecursionDesired = true
	setEDNS0(msg)

	resp, err := r.QueryWithRetry(ctx, msg)
	if err != nil {
		return nil, fmt.Errorf("txt lookup for %s: %w", name, err)
	}

	if resp.Rcode != dns.RcodeSuccess {
		return nil, nil
	}

	var records []string
	for _, rr := range resp.Answer {
		if txt, ok := rr.(*dns.TXT); ok {
			// Concatenate TXT strings per RFC 7208 section 3.3.
			var full strings.Builder
			for _, s := range txt.Txt {
				full.WriteString(sanitizeText(s))
			}
			records = append(records, full.String())
		}
	}
	return records, nil
}

// LookupPTR resolves PTR records for the given reverse DNS name.
func (r *Resolver) LookupPTR(ctx context.Context, name string) ([]string, error) {
	fqdn := dns.Fqdn(name)

	msg := new(dns.Msg)
	msg.SetQuestion(fqdn, dns.TypePTR)
	msg.RecursionDesired = true
	setEDNS0(msg)

	resp, err := r.QueryWithRetry(ctx, msg)
	if err != nil {
		return nil, fmt.Errorf("ptr lookup for %s: %w", name, err)
	}

	if resp.Rcode != dns.RcodeSuccess {
		return nil, nil
	}

	var names []string
	for _, rr := range resp.Answer {
		if ptr, ok := rr.(*dns.PTR); ok {
			names = append(names, sanitizeText(ptr.Ptr))
		}
	}
	return names, nil
}
