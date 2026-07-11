package checker

import (
	"context"
	"strings"
	"testing"

	"github.com/miekg/dns"
)

// fakeZone is a scripted DNS server: a flat record set answered by
// (name, qtype), with per-name NXDOMAIN/SERVFAIL overrides and an optional AD
// flag for the DNSSEC path. It backs the real *Resolver so the DNS-only checks
// (MX/NS/PTR/SPF/DNSSEC) run their production resolve paths over canned data.
type fakeZone struct {
	rrs      []dns.RR
	nx       map[string]bool
	servfail map[string]bool
	ad       bool // AuthenticatedData bit set on every reply
}

// newZone parses zone-file RR strings (e.g. "example.org. 3600 IN AAAA 2001:db8::1").
func newZone(t *testing.T, rrs ...string) *fakeZone {
	t.Helper()
	z := &fakeZone{nx: map[string]bool{}, servfail: map[string]bool{}}
	for _, s := range rrs {
		rr, err := dns.NewRR(s)
		if err != nil {
			t.Fatalf("bad RR %q: %v", s, err)
		}
		z.rrs = append(z.rrs, rr)
	}
	return z
}

func (z *fakeZone) handler(w dns.ResponseWriter, r *dns.Msg) {
	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true
	m.AuthenticatedData = z.ad
	q := r.Question[0]
	name := strings.ToLower(q.Name)
	switch {
	case z.servfail[name]:
		m.Rcode = dns.RcodeServerFailure
	case z.nx[name]:
		m.Rcode = dns.RcodeNameError
	default:
		for _, rr := range z.rrs {
			if strings.EqualFold(rr.Header().Name, q.Name) && rr.Header().Rrtype == q.Qtype {
				m.Answer = append(m.Answer, rr)
			}
		}
	}
	_ = w.WriteMsg(m)
}

// zoneDialer wires a SafeDialer to a real Resolver pointed at the scripted zone.
func zoneDialer(t *testing.T, z *fakeZone) *SafeDialer {
	t.Helper()
	addr := startFakeDNS(t, z.handler)
	return NewSafeDialer(NewResolver([]string{addr}))
}

// scriptAAAA is a canned AAAAResolver: it returns a pre-set answer (and
// optional error) per queried name, driving the two consensus-seam checks
// (dns_aaaa_base, dns_aaaa_www) without any DNS I/O.
type scriptAAAA struct {
	ans map[string]AAAAAnswer
	err map[string]error
}

func newScriptAAAA() *scriptAAAA {
	return &scriptAAAA{ans: map[string]AAAAAnswer{}, err: map[string]error{}}
}

func (s *scriptAAAA) LookupAAAA(_ context.Context, name string) (AAAAAnswer, error) {
	return s.ans[name], s.err[name]
}
