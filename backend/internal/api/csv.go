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

// writeCSV emits text/csv with the attachment disposition, cells neutralized
// against spreadsheet formula injection.
func writeCSV(w http.ResponseWriter, filename string, header []string, rows [][]string) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.WriteHeader(http.StatusOK)
	cw := csv.NewWriter(w)
	_ = cw.Write(header)
	for _, row := range rows {
		for i := range row {
			row[i] = csvSanitize(row[i])
		}
	}
	_ = cw.WriteAll(rows)
}

// csvSanitize neutralizes spreadsheet formula injection (OWASP): a cell
// starting with =, +, -, @, tab or CR is prefixed with a single quote so
// Excel/LibreOffice treat it as text, never a formula.
func csvSanitize(cell string) string {
	if cell == "" {
		return cell
	}
	switch cell[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + cell
	}
	return cell
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

// domainCSV is the one declaration of the /domains* CSV projection — the
// §4.2 summary-row fields flattened, each column name adjacent to its cell
// extractor so header and rows cannot misalign (the ConfirmedSextet
// declare-once rule at CSV scale).
var domainCSV = []struct {
	name string
	cell func(*DomainSummary) string
}{
	{"host", func(d *DomainSummary) string { return d.Host }},
	{"rank", func(d *DomainSummary) string { return csvInt32(d.Rank) }},
	{"kind", func(d *DomainSummary) string { return d.Kind }},
	{"parent", func(d *DomainSummary) string { return csvStr(d.Parent) }},
	{"classification", func(d *DomainSummary) string { return d.Classification }},
	{"class_flags", func(d *DomainSummary) string { return strings.Join(d.ClassFlags, ";") }},
	{"saint", func(d *DomainSummary) string { return strconv.FormatBool(d.Saint) }},
	{"base_status", func(d *DomainSummary) string { return csvStr(d.Status.Base.Value) }},
	{"base_since", func(d *DomainSummary) string { return csvTime(d.Status.Base.Since) }},
	{"www_status", func(d *DomainSummary) string { return csvStr(d.Status.WWW.Value) }},
	{"www_since", func(d *DomainSummary) string { return csvTime(d.Status.WWW.Since) }},
	{"ns_status", func(d *DomainSummary) string { return csvStr(d.Status.NS.Value) }},
	{"ns_since", func(d *DomainSummary) string { return csvTime(d.Status.NS.Since) }},
	{"mx_status", func(d *DomainSummary) string { return csvStr(d.Status.MX.Value) }},
	{"mx_since", func(d *DomainSummary) string { return csvTime(d.Status.MX.Since) }},
	{"conn_status", func(d *DomainSummary) string { return csvStr(d.Status.Conn.Value) }},
	{"conn_since", func(d *DomainSummary) string { return csvTime(d.Status.Conn.Since) }},
	{"resources_status", func(d *DomainSummary) string { return csvStr(d.Status.Resources.Value) }},
	{"resources_since", func(d *DomainSummary) string { return csvTime(d.Status.Resources.Since) }},
	{"tld", func(d *DomainSummary) string { return csvStr(d.TLD) }},
	{"country_code", func(d *DomainSummary) string { return d.Country.Code }},
	{"country_name", func(d *DomainSummary) string { return d.Country.Name }},
	{"asn_number", func(d *DomainSummary) string { return strconv.FormatInt(d.ASN.Number, 10) }},
	{"asn_name", func(d *DomainSummary) string { return d.ASN.Name }},
	{"dns_provider_id", func(d *DomainSummary) string {
		if d.DNSProvider == nil {
			return ""
		}
		return strconv.FormatInt(d.DNSProvider.ID, 10)
	}},
	{"dns_provider_name", func(d *DomainSummary) string {
		if d.DNSProvider == nil {
			return ""
		}
		return d.DNSProvider.Name
	}},
	{"hosting_provider", func(d *DomainSummary) string { return csvStr(d.HostingProvider) }},
	{"last_checked_at", func(d *DomainSummary) string { return csvTime(d.LastCheckedAt) }},
}

func writeDomainsCSV(w http.ResponseWriter, items []DomainSummary) {
	header := make([]string, len(domainCSV))
	for i, c := range domainCSV {
		header[i] = c.name
	}
	rows := make([][]string, len(items))
	for i := range items {
		row := make([]string, len(domainCSV))
		for j, c := range domainCSV {
			row[j] = c.cell(&items[i])
		}
		rows[i] = row
	}
	writeCSV(w, "domains.csv", header, rows)
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
