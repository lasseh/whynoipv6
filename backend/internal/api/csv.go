package api

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// parseFormat handles the ?format= negotiation (07 §5.5): csv is a query
// param — never Accept negotiation — so the CDN cache key stays clean and
// a shared link reproduces the same view.
func parseFormat(q url.Values) (csvWanted bool, err error) {
	switch q.Get("format") {
	case "", "json":
		return false, nil
	case "csv":
		return true, nil
	default:
		return false, fmt.Errorf("%w: format must be json or csv", ErrCursorInvalid)
	}
}

// writeCSV emits text/csv with the attachment disposition.
func writeCSV(w http.ResponseWriter, filename string, header []string, rows [][]string) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.WriteHeader(http.StatusOK)
	cw := csv.NewWriter(w)
	_ = cw.Write(header)
	_ = cw.WriteAll(rows)
}

func csvStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func csvInt32(p *int32) string {
	if p == nil {
		return ""
	}
	return strconv.FormatInt(int64(*p), 10)
}

func csvTime(p *time.Time) string {
	if p == nil {
		return ""
	}
	return p.UTC().Format(time.RFC3339)
}

// domainCSVHeader is the defined /domains* column set — the §4.2
// summary-row fields flattened.
var domainCSVHeader = []string{
	"host", "rank", "kind", "parent", "classification", "class_flags", "gold",
	"base_status", "base_since", "www_status", "www_since",
	"ns_status", "ns_since", "mx_status", "mx_since",
	"conn_status", "conn_since", "resources_status", "resources_since",
	"tld", "country_code", "country_name", "asn_number", "asn_name",
	"dns_provider_id", "dns_provider_name", "hosting_provider", "last_checked_at",
}

func writeDomainsCSV(w http.ResponseWriter, items []DomainSummary) {
	rows := make([][]string, len(items))
	for i := range items {
		d := &items[i]
		providerID, providerName := "", ""
		if d.DNSProvider != nil {
			providerID = strconv.FormatInt(d.DNSProvider.ID, 10)
			providerName = d.DNSProvider.Name
		}
		st := func(o StatusObject) (string, string) { return csvStr(o.Value), csvTime(o.Since) }
		base, baseSince := st(d.Status.Base)
		www, wwwSince := st(d.Status.WWW)
		ns, nsSince := st(d.Status.NS)
		mx, mxSince := st(d.Status.MX)
		conn, connSince := st(d.Status.Conn)
		res, resSince := st(d.Status.Resources)
		rows[i] = []string{
			d.Host, csvInt32(d.Rank), d.Kind, csvStr(d.Parent), d.Classification,
			strings.Join(d.ClassFlags, ";"), strconv.FormatBool(d.Gold),
			base, baseSince, www, wwwSince, ns, nsSince, mx, mxSince,
			conn, connSince, res, resSince,
			csvStr(d.TLD), d.Country.Code, d.Country.Name,
			strconv.FormatInt(d.ASN.Number, 10), d.ASN.Name,
			providerID, providerName, csvStr(d.HostingProvider), csvTime(d.LastCheckedAt),
		}
	}
	writeCSV(w, "domains.csv", domainCSVHeader, rows)
}

func writeCountriesCSV(w http.ResponseWriter, items []CountryBody) {
	rows := make([][]string, len(items))
	for i := range items {
		c := &items[i]
		rows[i] = []string{c.Code, c.Name, csvStr(c.TLD),
			strconv.FormatInt(int64(c.Sites), 10), strconv.FormatInt(int64(c.V6Sites), 10),
			strconv.FormatFloat(c.Percent, 'f', 2, 64)}
	}
	writeCSV(w, "countries.csv", []string{"code", "name", "tld", "sites", "v6_sites", "percent"}, rows)
}

func writeASNsCSV(w http.ResponseWriter, items []ASNBody) {
	rows := make([][]string, len(items))
	for i := range items {
		a := &items[i]
		rows[i] = []string{strconv.FormatInt(a.Number, 10), a.Name,
			strconv.FormatInt(int64(a.CountTotal), 10), strconv.FormatInt(int64(a.CountV6), 10),
			strconv.FormatInt(int64(a.CountV4), 10)}
	}
	writeCSV(w, "asns.csv", []string{"number", "name", "count_total", "count_v6", "count_v4"}, rows)
}

func writeProvidersCSV(w http.ResponseWriter, items []ProviderBody) {
	rows := make([][]string, len(items))
	for i := range items {
		p := &items[i]
		rows[i] = []string{strconv.FormatInt(p.ID, 10), p.Name,
			strconv.FormatInt(int64(p.CountTotal), 10), strconv.FormatInt(int64(p.CountV6), 10),
			strconv.FormatInt(int64(p.CountV4), 10)}
	}
	writeCSV(w, "providers.csv", []string{"id", "name", "count_total", "count_v6", "count_v4"}, rows)
}

func writeChangelogCSV(w http.ResponseWriter, items []ChangelogItem) {
	rows := make([][]string, len(items))
	for i := range items {
		c := &items[i]
		rows[i] = []string{c.TS.Format(time.RFC3339), c.Host, c.Field, c.OldValue, c.NewValue}
	}
	writeCSV(w, "changelog.csv", []string{"ts", "host", "field", "old_value", "new_value"}, rows)
}
