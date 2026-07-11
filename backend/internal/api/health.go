package api

import (
	"net/http"
	"net/netip"
)

// livez: 200 whenever the process runs; no dependency checks (07 §2.7).
func (s *Server) livez(w http.ResponseWriter, _ *http.Request) {
	NoStore(w)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// readyz: 200 only when Postgres is reachable (07 §2.7).
func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	NoStore(w)
	if err := s.pool.Ping(r.Context()); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("db unreachable"))
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// ip echoes the RealIP-derived client address (07 §4.12): bracketless, with
// family derived server-side; always no-store.
func (s *Server) ip(w http.ResponseWriter, r *http.Request) {
	NoStore(w)
	addrStr := clientIP(r)
	family := "ipv4"
	if addr, err := netip.ParseAddr(addrStr); err == nil && addr.Is6() && !addr.Is4In6() {
		family = "ipv6"
	}
	WriteJSON(w, http.StatusOK, map[string]string{"ip": addrStr, "family": family})
}
