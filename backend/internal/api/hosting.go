package api

import (
	"context"
	"net/http"
)

// HostingBody is the §4.6 hosting/CDN league-table row — exact stored
// counters (recomputed daily like asn and dns_provider), count_v4 synthesized.
//
// slug and name are not interchangeable. slug is the stable identifier: it is
// what domain.hosting_provider stores and what /domains?hosting= accepts.
// name exists because the stored values are join keys, not brands — rendering
// them raw gives a league reading "cloudflare / aws / ovh" in lowercase.
type HostingBody struct {
	Slug       string `json:"slug"`
	Name       string `json:"name"`
	CountTotal int32  `json:"count_total"`
	CountV6    int32  `json:"count_v6"`
	CountV4    int32  `json:"count_v4"`
}

// listHosting is GET /hosting — the bounded curated registry, served whole
// with an exact count (07 §4.6, §3.4).
//
// Reads stored counters rather than aggregating live: §4.6 requires a stats
// source for this league, and a per-request GROUP BY over the largest table
// is what that rule exists to prevent. The daily tick recomputes them beside
// the ASN and DNS-provider counters.
func (s *Server) listHosting(w http.ResponseWriter, r *http.Request) {
	ServeWhole(s, w, r, WholeSpec[HostingBody]{
		Fetch: func(ctx context.Context, _ string) ([]HostingBody, error) {
			rows, err := s.q.HostingLeaderboard(ctx)
			if err != nil {
				return nil, err
			}
			items := make([]HostingBody, len(rows))
			for i := range rows {
				items[i] = HostingBody{Slug: rows[i].Slug, Name: rows[i].Name,
					CountTotal: rows[i].CountTotal, CountV6: rows[i].CountV6,
					CountV4: rows[i].CountTotal - rows[i].CountV6}
			}
			return items, nil
		},
		CSV: writeHostingCSV,
	})
}
