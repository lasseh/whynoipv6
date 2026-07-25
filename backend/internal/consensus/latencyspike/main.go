//go:build spike

// Command latencyspike is the P1.12 risk-gate harness (throwaway, build tag
// `spike`, never shipped): it fires AAAA lookups at the three consensus
// providers and the compose Unbound instances, measuring per-provider latency
// and a sustained-rate window against the ~24 qps/provider consensus budget
// (00-overview.md PUBLIC_RESOLVER_QPS; design §2.7).
package main

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/miekg/dns"
)

var hosts = []string{
	"google.com.", "facebook.com.", "wikipedia.org.", "cloudflare.com.",
	"amazon.com.", "netflix.com.", "apple.com.", "microsoft.com.",
	"vg.no.", "nrk.no.", "dnb.no.", "bbc.co.uk.", "lemonde.fr.",
	"spiegel.de.", "elpais.com.", "asahi.com.", "gov.uk.", "europa.eu.",
	"whynoipv6.com.", "ripe.net.", "ietf.org.", "kernel.org.",
}

type target struct{ name, addr string }

func main() {
	targets := []target{
		{"cloudflare", "1.1.1.1:53"},
		{"google", "8.8.8.8:53"},
		{"quad9", "9.9.9.9:53"},
		{"unbound1", "127.0.0.1:5301"},
		{"unbound2", "127.0.0.1:5302"},
	}
	fmt.Printf("# Resolver latency spike — %s\n\n", time.Now().UTC().Format(time.RFC3339))

	for _, tg := range targets {
		lat, errs := sequential(tg, 200)
		fmt.Printf("## %s (%s)\n", tg.name, tg.addr)
		if len(lat) == 0 {
			fmt.Printf("UNREACHABLE: %d/%d queries failed\n\n", errs, 200)
			continue
		}
		fmt.Printf("sequential 200 queries: p50 %.1f ms, p95 %.1f ms, p99 %.1f ms, errors %d\n",
			pct(lat, 50), pct(lat, 95), pct(lat, 99), errs)

		ok, tOut, sLat := rated(tg, 24, 10*time.Second)
		fmt.Printf("sustained 24 qps × 10 s: %d ok, %d failed, p50 %.1f ms, p95 %.1f ms\n\n",
			ok, tOut, pct(sLat, 50), pct(sLat, 95))
	}
}

func query(addr, host string) (time.Duration, error) {
	c := &dns.Client{Timeout: 2 * time.Second}
	m := new(dns.Msg)
	m.SetQuestion(host, dns.TypeAAAA)
	start := time.Now()
	_, _, err := c.Exchange(m, addr)
	return time.Since(start), err
}

func sequential(tg target, n int) (lat []float64, errs int) {
	for i := 0; i < n; i++ {
		d, err := query(tg.addr, hosts[i%len(hosts)])
		if err != nil {
			errs++
			continue
		}
		lat = append(lat, float64(d.Microseconds())/1000)
	}
	return lat, errs
}

func rated(tg target, qps int, dur time.Duration) (ok, failed int, lat []float64) {
	var mu sync.Mutex
	var wg sync.WaitGroup
	tick := time.NewTicker(time.Second / time.Duration(qps))
	defer tick.Stop()
	deadline := time.Now().Add(dur)
	i := 0
	for time.Now().Before(deadline) {
		<-tick.C
		i++
		wg.Add(1)
		go func(h string) {
			defer wg.Done()
			d, err := query(tg.addr, h)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed++
				return
			}
			ok++
			lat = append(lat, float64(d.Microseconds())/1000)
		}(hosts[i%len(hosts)])
	}
	wg.Wait()
	return ok, failed, lat
}

func pct(lat []float64, p int) float64 {
	if len(lat) == 0 {
		return 0
	}
	s := append([]float64(nil), lat...)
	sort.Float64s(s)
	idx := len(s) * p / 100
	if idx >= len(s) {
		idx = len(s) - 1
	}
	return s[idx]
}
