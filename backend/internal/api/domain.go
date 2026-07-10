package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/lasseh/whynoipv6/internal/domain"
	"github.com/lasseh/whynoipv6/internal/postgres"
	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// StatusObject is the per-dimension status/provenance pair (07 §4.1):
// value is the 4-value enum or JSON null when never confirmed.
type StatusObject struct {
	Value *string    `json:"value"`
	Since *time.Time `json:"since"`
}

// StatusBlock groups the six confirmed dimensions.
type StatusBlock struct {
	Base      StatusObject `json:"base"`
	WWW       StatusObject `json:"www"`
	NS        StatusObject `json:"ns"`
	MX        StatusObject `json:"mx"`
	Conn      StatusObject `json:"conn"`
	Resources StatusObject `json:"resources"`
}

// CountryRef / ASNRef / ProviderRef are the embedded pivot objects (07 §4.2).
type CountryRef struct {
	Code string  `json:"code"`
	Name string  `json:"name"`
	TLD  *string `json:"tld,omitempty"` // detail representation only
}

type ASNRef struct {
	Number int64  `json:"number"`
	Name   string `json:"name"`
}

type ProviderRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// DomainSummary is the §4.2 list row.
type DomainSummary struct {
	Host            string       `json:"host"`
	Rank            *int32       `json:"rank"`
	Kind            string       `json:"kind"`
	Parent          *string      `json:"parent"`
	Classification  string       `json:"classification"`
	ClassFlags      []string     `json:"class_flags"`
	Gold            bool         `json:"gold"`
	Status          StatusBlock  `json:"status"`
	TLD             *string      `json:"tld"`
	Country         CountryRef   `json:"country"`
	ASN             ASNRef       `json:"asn"`
	DNSProvider     *ProviderRef `json:"dns_provider"`
	HostingProvider *string      `json:"hosting_provider"`
	LastCheckedAt   *time.Time   `json:"last_checked_at"`
}

func statusObj(value *string, since *time.Time) StatusObject {
	return StatusObject{Value: value, Since: since}
}

func summaryFromRow(r *postgres.DomainRow) DomainSummary {
	flags := r.ClassFlags
	if flags == nil {
		flags = []string{}
	}
	s := DomainSummary{
		Host:           r.Host,
		Rank:           r.Rank,
		Kind:           r.Kind,
		Parent:         r.Parent,
		Classification: r.Classification,
		ClassFlags:     flags,
		Gold:           r.Gold,
		Status: StatusBlock{
			Base:      statusObj(r.BaseStatus, r.BaseSince),
			WWW:       statusObj(r.WWWStatus, r.WWWSince),
			NS:        statusObj(r.NSStatus, r.NSSince),
			MX:        statusObj(r.MXStatus, r.MXSince),
			Conn:      statusObj(r.ConnStatus, r.ConnSince),
			Resources: statusObj(r.ResStatus, r.ResSince),
		},
		TLD:             r.TLD,
		Country:         CountryRef{Code: r.CountryCode, Name: r.CountryName},
		ASN:             ASNRef{Number: r.ASNNumber, Name: r.ASNName},
		HostingProvider: r.Hosting,
		LastCheckedAt:   r.LastCheckedAt,
	}
	if r.ProviderID != nil && r.ProviderName != nil {
		s.DNSProvider = &ProviderRef{ID: *r.ProviderID, Name: *r.ProviderName}
	}
	return s
}

// Closed filter vocabularies (07 §3.3) — values outside these are
// validation-error; membership is what makes literal emission safe.
const (
	classHero         = "hero"
	classSinner       = "sinner"
	flagBrokenV6      = "broken_v6"
	statusSupported   = "supported"
	statusUnsupported = "unsupported"
)

var (
	classValues = map[string]bool{classHero: true, "partial": true, classSinner: true, "inactive": true, "unknown": true}
	flagValues  = map[string]bool{flagBrokenV6: true, "www_missing": true, "ns_missing": true, "mail_missing": true, "resources_v4only": true}
	statusVals  = map[string]bool{statusSupported: true, statusUnsupported: true, "no_record": true, "not_applicable": true}
	statusDims  = []string{"base", "www", "ns", "mx", "conn", "resources"}
)

// validationError carries a field-level validation failure to the handler rim.
type validationError struct{ field, msg string }

func (e validationError) Error() string { return e.field + ": " + e.msg }

// parseDomainFilter validates the §3.3 grammar into the builder filter.
// Unknown-but-well-formed country/ASN values resolve to an impossible id so
// the list is simply empty (the value is not *invalid*).
func (s *Server) parseDomainFilter(r *http.Request, q url.Values) (postgres.DomainListFilter, error) {
	var f postgres.DomainListFilter
	impossible := int32(-1)

	if v := q.Get(paramClass); v != "" {
		if !classValues[v] {
			return f, validationError{paramClass, "must be one of hero, partial, sinner, inactive, unknown"}
		}
		f.Class = v
	}
	if v := q.Get("gold"); v != "" {
		if v != "true" {
			return f, validationError{"gold", "the only accepted value is true"}
		}
		f.Gold = true
	}
	if v := q.Get(paramCountry); v != "" {
		id, err := s.svc.Q.CountryIDByCode(r.Context(), strings.ToUpper(v))
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			f.CountryID = &impossible
		case err != nil:
			return f, fmt.Errorf("country lookup: %w", err)
		default:
			f.CountryID = &id
		}
	}
	if v := q.Get(paramASN); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			return f, validationError{paramASN, "must be a non-negative AS number"}
		}
		id, err := s.svc.Q.ASNIDByNumber(r.Context(), n)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			f.ASNID = &impossible
		case err != nil:
			return f, fmt.Errorf("asn lookup: %w", err)
		default:
			f.ASNID = &id
		}
	}
	if v := q.Get(paramProvider); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id < 1 {
			return f, validationError{paramProvider, "must be a dns_provider id"}
		}
		f.Provider = &id
	}
	f.TLD = q.Get(paramTLD)
	f.Hosting = q.Get("hosting")
	if v := q.Get(paramFlag); v != "" {
		if !flagValues[v] {
			return f, validationError{paramFlag, "unknown flag"}
		}
		f.Flag = v
	}
	for _, dim := range statusDims {
		if v := q.Get(dim); v != "" {
			if !statusVals[v] {
				return f, validationError{dim, "must be an ipv6_status value (supported, unsupported, no_record, not_applicable)"}
			}
			f.StatusDim, f.StatusVal = dim, v
		}
	}
	if v := q.Get("rank_min"); v != "" {
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil || n < 0 {
			return f, validationError{"rank_min", "must be a non-negative integer"}
		}
		m := int32(n)
		f.RankMin = &m
	}
	if v := q.Get("rank_max"); v != "" {
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil || n < 0 {
			return f, validationError{"rank_max", "must be a non-negative integer"}
		}
		m := int32(n)
		f.RankMax = &m
	}
	f.Query = q.Get("q")
	return f, nil
}

// isUnfiltered reports whether the global max(rank) count shortcut applies.
func isUnfiltered(f *postgres.DomainListFilter) bool {
	return f.Class == "" && !f.Gold && f.CountryID == nil && f.ASNID == nil &&
		f.Provider == nil && f.TLD == "" && f.Hosting == "" && f.Flag == "" &&
		f.StatusDim == "" && f.RankMin == nil && f.RankMax == nil && f.Query == ""
}

// listDomains is GET /domains — the general filterable collection.
func (s *Server) listDomains(w http.ResponseWriter, r *http.Request) {
	s.serveDomainList(w, r, nil)
}

// The tier paths (07 §4.4) — preset filtered views over the same leaderboard.
func (s *Server) listHeroes(w http.ResponseWriter, r *http.Request) {
	s.serveDomainList(w, r, url.Values{paramClass: {classHero}})
}

func (s *Server) listSinners(w http.ResponseWriter, r *http.Request) {
	s.serveDomainList(w, r, url.Values{paramClass: {classSinner}})
}

func (s *Server) listGold(w http.ResponseWriter, r *http.Request) {
	s.serveDomainList(w, r, url.Values{"gold": {"true"}})
}

func (s *Server) listAlmost(w http.ResponseWriter, r *http.Request) {
	s.serveDomainList(w, r, url.Values{paramClass: {"partial"}})
}

func (s *Server) listMail(w http.ResponseWriter, r *http.Request) {
	s.serveDomainList(w, r, url.Values{paramClass: {classHero}, "mx": {statusSupported}})
}

// serveDomainList is the shared /domains* engine: preset params override the
// request's, then the §3.3 guardrail, sort/cursor plumbing, the squirrel
// builder, and the §2.4 envelope.
func (s *Server) serveDomainList(w http.ResponseWriter, r *http.Request, preset url.Values) {
	q := r.URL.Query()
	for k, vs := range preset {
		q[k] = vs
	}

	// The §3.3 guardrail. A provider preset comes from the
	// /providers/{id}/domains path, where dns_provider_id is the indexed
	// scope itself (07 §4.6) — not a residual, and it satisfies the scope
	// for one user residual, like the country/asn paths.
	vq := q
	pathScoped := preset.Get(paramProvider) != ""
	if pathScoped {
		vq = url.Values{}
		for k, vs := range q {
			vq[k] = vs
		}
		vq.Del(paramProvider)
	}
	if err := ValidateResiduals(vq, pathScoped); err != nil {
		ScopeRequired(w, r, strings.TrimPrefix(err.Error(), "scope required: "))
		return
	}

	// Sort: ?q= never composes with rank ordering (07 §3.3) — forced to host.
	sortKey := q.Get("sort")
	switch sortKey {
	case "":
		sortKey = SortRank
	case SortRank, SortRankDesc, SortHost:
	default:
		InvalidParameter(w, r, "sort must be rank, -rank, or host")
		return
	}
	if q.Get("q") != "" {
		sortKey = SortHost
	}

	wantCSV, err := parseFormat(q)
	if err != nil {
		InvalidParameter(w, r, err.Error())
		return
	}
	limitCap := MaxLimit
	if wantCSV {
		limitCap = s.opts.CSVMaxRows // §5.5: CSV raises the cap, same view
	}
	limit, err := ParseLimitCap(q, limitCap)
	if err != nil {
		InvalidParameter(w, r, err.Error())
		return
	}
	afterRank, err := ParseAfterRank(q, sortKey)
	if err != nil {
		InvalidParameter(w, r, err.Error())
		return
	}

	filter, err := s.parseDomainFilter(r, q)
	var ve validationError
	if errors.As(err, &ve) {
		ValidationError(w, r, []FieldError{{Field: ve.field, Reason: ve.msg}})
		return
	}
	if err != nil {
		InternalError(w, r)
		return
	}

	generation, asOf, err := s.svc.Generation(r.Context())
	if err != nil {
		InternalError(w, r)
		return
	}
	if CacheList(w, r, generation) {
		return
	}

	fingerprint := FilterFingerprint(q)
	var seek *postgres.DomainSeek
	if token := q.Get(paramCursor); token != "" {
		c, err := DecodeCursor(token, sortKey, fingerprint, generation)
		if err != nil {
			InvalidParameter(w, r, err.Error())
			return
		}
		st, err := c.SeekTuple()
		if err != nil {
			InvalidParameter(w, r, err.Error())
			return
		}
		seek = &postgres.DomainSeek{Rank: st.Rank, ID: st.ID, Host: st.Host}
	}

	rows, err := postgres.ListDomains(r.Context(), s.svc.Pool, &filter, postgres.ListSort(sortKey), seek, afterRank, limit)
	if err != nil {
		InternalError(w, r)
		return
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}

	items := make([]DomainSummary, len(rows))
	for i := range rows {
		items[i] = summaryFromRow(&rows[i])
	}
	if wantCSV {
		writeDomainsCSV(w, items)
		return
	}

	var lastK []any
	if hasMore {
		last := &rows[len(rows)-1]
		if sortKey == SortHost {
			lastK = []any{last.Host}
		} else if last.Rank != nil {
			lastK = []any{*last.Rank, last.ID}
		}
	}

	meta := NewMeta(asOf, generation)
	var est int64
	if isUnfiltered(&filter) {
		est, err = postgres.MaxRank(r.Context(), s.svc.Pool)
	} else {
		est, err = postgres.EstimateDomainListCount(r.Context(), s.svc.Pool, &filter)
	}
	if err != nil {
		InternalError(w, r)
		return
	}
	meta.CountEstimate = &est

	WriteJSON(w, http.StatusOK, ListEnvelope{
		Items: trimFields(items, q.Get("fields")),
		Page:  PageOf(generation, sortKey, fingerprint, hasMore, lastK, nil),
		Meta:  meta,
	})
}

// trimFields applies the ?fields= sparse fieldset (07 §3.3) by re-projecting
// each row onto the requested top-level keys; unknown keys simply vanish.
func trimFields(items []DomainSummary, fields string) any {
	if fields == "" {
		return items
	}
	want := map[string]bool{}
	for _, f := range strings.Split(fields, ",") {
		want[strings.TrimSpace(f)] = true
	}
	out := make([]map[string]json.RawMessage, len(items))
	for i := range items {
		raw, _ := json.Marshal(items[i])
		var full map[string]json.RawMessage
		_ = json.Unmarshal(raw, &full)
		row := make(map[string]json.RawMessage, len(want))
		for k := range full {
			if want[k] {
				row[k] = full[k]
			}
		}
		out[i] = row
	}
	return out
}

// DomainDetail is the §4.3 representation.
type DomainDetail struct {
	Host            string           `json:"host"`
	Rank            *int32           `json:"rank"`
	Kind            string           `json:"kind"`
	Parent          *string          `json:"parent"`
	Classification  string           `json:"classification"`
	ClassFlags      []string         `json:"class_flags"`
	Gold            bool             `json:"gold"`
	Status          StatusBlock      `json:"status"`
	Informational   Informational    `json:"informational"`
	TLD             *string          `json:"tld"`
	Country         CountryRef       `json:"country"`
	ASN             ASNRef           `json:"asn"`
	DNSProvider     *ProviderRef     `json:"dns_provider"`
	HostingProvider *string          `json:"hosting_provider"`
	SubdomainCount  int64            `json:"subdomain_count"`
	Disabled        bool             `json:"disabled"`
	LastCheckedAt   *time.Time       `json:"last_checked_at"`
	CreatedAt       time.Time        `json:"created_at"`
	Evidence        *json.RawMessage `json:"evidence,omitempty"`
	Meta            DetailMeta       `json:"meta"`
}

// Informational carries the advisory dimensions after §4.3 masking.
type Informational struct {
	DNSSEC      *string `json:"dnssec"`
	PTR         *string `json:"ptr"`
	SMTP        *string `json:"smtp"`
	Parity      *string `json:"parity"`
	LatencyV4Ms *int32  `json:"latency_v4_ms"`
	LatencyV6Ms *int32  `json:"latency_v6_ms"`
}

// DetailMeta is the freshness block embedded in the detail body.
type DetailMeta struct {
	AsOf       time.Time `json:"as_of"`
	Generation int32     `json:"generation"`
}

// maskObservation applies the §4.3 public-masking rule: error/inconsistent
// never reach the wire; partial is public only where allowPartial.
func maskObservation(o *db.Observation, allowPartial bool) *string {
	if o == nil {
		return nil
	}
	switch *o {
	case db.ObservationError, db.ObservationInconsistent:
		return nil
	case db.ObservationPartial:
		if !allowPartial {
			return nil
		}
	case db.ObservationSupported, db.ObservationUnsupported, db.ObservationNoRecord, db.ObservationNotApplicable:
	}
	v := string(*o)
	return &v
}

func statusPtr(v *db.Ipv6Status) *string {
	if v == nil {
		return nil
	}
	s := string(*v)
	return &s
}

func pgTimePtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	u := t.Time.UTC()
	return &u
}

// getDomain is GET /domains/{host} (07 §4.3). Disabled and rank-NULL
// entities resolve here even though they are invisible in collections.
func (s *Server) getDomain(w http.ResponseWriter, r *http.Request) {
	host, err := domain.Canonicalize(chi.URLParam(r, "host"))
	if err != nil {
		NotFound(w, r, "Domain not found", "The host is not a valid public domain name.")
		return
	}
	row, err := s.svc.Q.DomainDetailByHost(r.Context(), host)
	if errors.Is(err, pgx.ErrNoRows) {
		NotFound(w, r, "Domain not found", "No such domain: "+host)
		return
	}
	if err != nil {
		InternalError(w, r)
		return
	}

	generation, asOf, err := s.svc.Generation(r.Context())
	if err != nil {
		InternalError(w, r)
		return
	}
	if CacheList(w, r, generation) {
		return
	}

	flags := row.ClassFlags
	if flags == nil {
		flags = []string{}
	}
	d := DomainDetail{
		Host:           row.Host,
		Rank:           row.Rank,
		Kind:           string(row.Kind),
		Parent:         row.Parent,
		Classification: string(row.Classification),
		ClassFlags:     flags,
		Gold:           row.Gold,
		Status: StatusBlock{
			Base:      statusObj(statusPtr(row.BaseStatus), pgTimePtr(row.BaseSince)),
			WWW:       statusObj(statusPtr(row.WwwStatus), pgTimePtr(row.WwwSince)),
			NS:        statusObj(statusPtr(row.NsStatus), pgTimePtr(row.NsSince)),
			MX:        statusObj(statusPtr(row.MxStatus), pgTimePtr(row.MxSince)),
			Conn:      statusObj(statusPtr(row.ConnStatus), pgTimePtr(row.ConnSince)),
			Resources: statusObj(statusPtr(row.ResourcesStatus), pgTimePtr(row.ResourcesSince)),
		},
		Informational: Informational{
			DNSSEC:      maskObservation(row.DnssecObserved, false),
			PTR:         maskObservation(row.PtrObserved, true),
			SMTP:        maskObservation(row.SmtpObserved, false),
			Parity:      maskObservation(row.ParityObserved, true),
			LatencyV4Ms: row.LatencyV4Ms,
			LatencyV6Ms: row.LatencyV6Ms,
		},
		TLD:             row.Tld,
		Country:         CountryRef{Code: row.CountryCode, Name: row.CountryName, TLD: row.CountryTld},
		ASN:             ASNRef{Number: row.AsnNumber, Name: row.AsnName},
		HostingProvider: row.HostingProvider,
		SubdomainCount:  row.SubdomainCount,
		Disabled:        row.Disabled,
		LastCheckedAt:   pgTimePtr(row.LastCheckedAt),
		CreatedAt:       row.CreatedAt.Time.UTC(),
		Meta:            DetailMeta{AsOf: asOf.UTC(), Generation: generation},
	}
	if row.ProviderID != nil && row.ProviderName != nil {
		d.DNSProvider = &ProviderRef{ID: *row.ProviderID, Name: *row.ProviderName}
	}

	if r.URL.Query().Get("include") == "evidence" {
		raw, err := s.svc.Q.LatestScanDetail(r.Context(), row.ID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			InternalError(w, r)
			return
		}
		if raw != nil {
			ev := json.RawMessage(raw)
			d.Evidence = &ev
		}
	}

	WriteJSON(w, http.StatusOK, d)
}

// ShameItem is the §4.4 /shame row.
type ShameItem struct {
	Host    string    `json:"host"`
	Reason  *string   `json:"reason"`
	AddedAt time.Time `json:"added_at"`
}

// listShame is GET /shame — the bounded editorial list: visibility computed
// read-side, no cursor, exact meta.count.
func (s *Server) listShame(w http.ResponseWriter, r *http.Request) {
	generation, asOf, err := s.svc.Generation(r.Context())
	if err != nil {
		InternalError(w, r)
		return
	}
	if CacheList(w, r, generation) {
		return
	}
	rows, err := s.svc.Q.ShameList(r.Context())
	if err != nil {
		InternalError(w, r)
		return
	}
	items := []ShameItem{}
	for i := range rows {
		if rows[i].Visible == nil || !*rows[i].Visible {
			continue
		}
		items = append(items, ShameItem{Host: rows[i].Host, Reason: rows[i].Reason, AddedAt: rows[i].AddedAt.Time.UTC()})
	}
	count := int64(len(items))
	meta := NewMeta(asOf, generation)
	meta.Count = &count
	WriteJSON(w, http.StatusOK, ListEnvelope{Items: items, Page: Page{}, Meta: meta})
}
