package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// sourceConfirmedState labels every §4.10 series: sourced from the
// confirmed-state stats_* tables so graphs match public lists exactly. The
// measurement-flavored scan_daily_adoption cagg is never exposed (OPEN-5).
const sourceConfirmedState = "confirmed_state"

// weeklySample keeps the latest snapshot per ISO week (07 §4.10 — a sample,
// never an average), preserving ascending order. days must be ascending;
// the returned indexes select the surviving rows.
func weeklySample(days []time.Time) []int {
	type week struct{ year, week int }
	var order []week
	latest := map[week]int{}
	for i, d := range days {
		y, w := d.ISOWeek()
		k := week{y, w}
		if _, seen := latest[k]; !seen {
			order = append(order, k)
		}
		latest[k] = i // ascending input → last write is the latest day
	}
	idx := make([]int, len(order))
	for i, k := range order {
		idx[i] = latest[k]
	}
	return idx
}

// statsWindow parses the one §4.10 query contract; shared with history.
func statsWindow(r *http.Request) (from, to time.Time, weekly bool, err error) {
	return parseHistoryWindow(r.URL.Query())
}

func (s *Server) statsMeta(r *http.Request) (Meta, int32, error) {
	generation, asOf, err := s.generation(r.Context())
	if err != nil {
		return Meta{}, 0, err
	}
	m := NewMeta(asOf, generation)
	m.Source = sourceConfirmedState
	return m, generation, nil
}

// GlobalStatsPoint carries the full stats_global_daily payload (07 §4.10).
type GlobalStatsPoint struct {
	Day                string `json:"day"`
	Domains            *int32 `json:"domains"`
	Heroes             *int32 `json:"heroes"`
	Partial            *int32 `json:"partial"`
	Sinners            *int32 `json:"sinners"`
	Inactive           *int32 `json:"inactive"`
	Unknown            *int32 `json:"unknown"`
	Gold               *int32 `json:"gold"`
	Disabled           *int32 `json:"disabled"`
	BaseSupported      *int32 `json:"base_supported"`
	WwwSupported       *int32 `json:"www_supported"`
	NsSupported        *int32 `json:"ns_supported"`
	MxSupported        *int32 `json:"mx_supported"`
	ConnSupported      *int32 `json:"conn_supported"`
	ResourcesSupported *int32 `json:"resources_supported"`
	TopHeroes          *int32 `json:"top_heroes"`
	TopNameserver      *int32 `json:"top_nameserver"`
}

// getStatsOverview is GET /stats/overview — the headline dashboard.
func (s *Server) getStatsOverview(w http.ResponseWriter, r *http.Request) {
	from, to, weekly, err := statsWindow(r)
	if err != nil {
		InvalidParameter(w, r, err.Error())
		return
	}
	meta, generation, err := s.statsMeta(r)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	if CacheList(w, r, generation) {
		return
	}
	rows, err := s.q.StatsGlobalRange(r.Context(), db.StatsGlobalRangeParams{
		FromDay: pgDate(from), ToDay: pgDate(to),
	})
	if err != nil {
		InternalError(w, r, err)
		return
	}
	points := make([]GlobalStatsPoint, len(rows))
	days := make([]time.Time, len(rows))
	for i := range rows {
		days[i] = rows[i].Day.Time
		points[i] = GlobalStatsPoint{
			Day:     rows[i].Day.Time.Format("2006-01-02"),
			Domains: rows[i].Domains, Heroes: rows[i].Heroes, Partial: rows[i].Partial,
			Sinners: rows[i].Sinners, Inactive: rows[i].Inactive, Unknown: rows[i].Unknown,
			Gold: rows[i].Gold, Disabled: rows[i].Disabled,
			BaseSupported: rows[i].BaseSupported, WwwSupported: rows[i].WwwSupported,
			NsSupported: rows[i].NsSupported, MxSupported: rows[i].MxSupported,
			ConnSupported: rows[i].ConnSupported, ResourcesSupported: rows[i].ResourcesSupported,
			TopHeroes: rows[i].TopHeroes, TopNameserver: rows[i].TopNameserver,
		}
	}
	if weekly {
		sampled := make([]GlobalStatsPoint, 0, len(points))
		for _, i := range weeklySample(days) {
			sampled = append(sampled, points[i])
		}
		points = sampled
	}
	WriteJSON(w, http.StatusOK, PointsEnvelope{Points: points, Meta: meta})
}

// CountryStatsPoint mirrors stats_country_daily.
type CountryStatsPoint struct {
	Day           string `json:"day"`
	Domains       *int32 `json:"domains"`
	Sinners       *int32 `json:"sinners"`
	Partial       *int32 `json:"partial"`
	Heroes        *int32 `json:"heroes"`
	BaseSupported *int32 `json:"base_supported"`
	ConnSupported *int32 `json:"conn_supported"`
}

// getCountryStats is GET /countries/{code}/stats.
func (s *Server) getCountryStats(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	if len(code) != 2 {
		NotFound(w, r, "Country not found", "Country codes are two-letter ISO 3166-1 alpha-2.")
		return
	}
	id, err := s.q.CountryIDByCode(r.Context(), code)
	if errors.Is(err, pgx.ErrNoRows) {
		NotFound(w, r, "Country not found", "No such country: "+strings.ToUpper(code))
		return
	}
	if err != nil {
		InternalError(w, r, err)
		return
	}
	from, to, weekly, err := statsWindow(r)
	if err != nil {
		InvalidParameter(w, r, err.Error())
		return
	}
	meta, generation, err := s.statsMeta(r)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	if CacheList(w, r, generation) {
		return
	}
	rows, err := s.q.StatsCountryRange(r.Context(), db.StatsCountryRangeParams{
		CountryID: id, FromDay: pgDate(from), ToDay: pgDate(to),
	})
	if err != nil {
		InternalError(w, r, err)
		return
	}
	points := make([]CountryStatsPoint, len(rows))
	days := make([]time.Time, len(rows))
	for i := range rows {
		days[i] = rows[i].Day.Time
		points[i] = CountryStatsPoint{
			Day:     rows[i].Day.Time.Format("2006-01-02"),
			Domains: rows[i].Domains, Sinners: rows[i].Sinners, Partial: rows[i].Partial,
			Heroes: rows[i].Heroes, BaseSupported: rows[i].BaseSupported, ConnSupported: rows[i].ConnSupported,
		}
	}
	if weekly {
		sampled := make([]CountryStatsPoint, 0, len(points))
		for _, i := range weeklySample(days) {
			sampled = append(sampled, points[i])
		}
		points = sampled
	}
	WriteJSON(w, http.StatusOK, PointsEnvelope{Points: points, Meta: meta})
}

// CampaignStatsPoint mirrors stats_campaign_daily incl. the v6_ready track.
type CampaignStatsPoint struct {
	Day           string `json:"day"`
	Domains       *int32 `json:"domains"`
	V6Ready       *int32 `json:"v6_ready"`
	Sinners       *int32 `json:"sinners"`
	Partial       *int32 `json:"partial"`
	Heroes        *int32 `json:"heroes"`
	BaseSupported *int32 `json:"base_supported"`
	WwwSupported  *int32 `json:"www_supported"`
	NsSupported   *int32 `json:"ns_supported"`
	MxSupported   *int32 `json:"mx_supported"`
	ConnSupported *int32 `json:"conn_supported"`
}

// getCampaignStats is GET /campaigns/{uuid}/stats; unknown or disabled
// campaigns are 404 (07 §4.10).
func (s *Server) getCampaignStats(w http.ResponseWriter, r *http.Request) {
	row, ok := s.campaignByPathUUID(w, r)
	if !ok {
		return
	}
	if row.Disabled {
		NotFound(w, r, "Campaign not found", "This campaign is disabled.")
		return
	}
	from, to, weekly, err := statsWindow(r)
	if err != nil {
		InvalidParameter(w, r, err.Error())
		return
	}
	meta, generation, err := s.statsMeta(r)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	if CacheList(w, r, generation) {
		return
	}
	rows, err := s.q.StatsCampaignRange(r.Context(), db.StatsCampaignRangeParams{
		CampaignID: row.ID, FromDay: pgDate(from), ToDay: pgDate(to),
	})
	if err != nil {
		InternalError(w, r, err)
		return
	}
	points := make([]CampaignStatsPoint, len(rows))
	days := make([]time.Time, len(rows))
	for i := range rows {
		days[i] = rows[i].Day.Time
		points[i] = CampaignStatsPoint{
			Day:     rows[i].Day.Time.Format("2006-01-02"),
			Domains: rows[i].Domains, V6Ready: rows[i].V6Ready,
			Sinners: rows[i].Sinners, Partial: rows[i].Partial, Heroes: rows[i].Heroes,
			BaseSupported: rows[i].BaseSupported, WwwSupported: rows[i].WwwSupported,
			NsSupported: rows[i].NsSupported, MxSupported: rows[i].MxSupported,
			ConnSupported: rows[i].ConnSupported,
		}
	}
	if weekly {
		sampled := make([]CampaignStatsPoint, 0, len(points))
		for _, i := range weeklySample(days) {
			sampled = append(sampled, points[i])
		}
		points = sampled
	}
	WriteJSON(w, http.StatusOK, PointsEnvelope{Points: points, Meta: meta})
}

// ASNStatsPoint maps stats_asn_daily onto the canonical §4.6 wire names:
// v6_domains → count_v6, domains → count_total.
type ASNStatsPoint struct {
	Day        string `json:"day"`
	CountTotal *int32 `json:"count_total"`
	CountV6    *int32 `json:"count_v6"`
	Sinners    *int32 `json:"sinners"`
	Heroes     *int32 `json:"heroes"`
}

// getASNStats is GET /asns/{number}/stats. A leading AS/as prefix is
// stripped; non-numeric after stripping → 400; unknown AS → 404.
func (s *Server) getASNStats(w http.ResponseWriter, r *http.Request) {
	raw := chi.URLParam(r, "number")
	raw = strings.TrimPrefix(strings.TrimPrefix(raw, "AS"), "as")
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 {
		InvalidParameter(w, r, "ASNs are keyed by their AS number (an optional AS prefix is accepted)")
		return
	}
	id, err := s.q.ASNIDByNumber(r.Context(), n)
	if errors.Is(err, pgx.ErrNoRows) {
		NotFound(w, r, "Network not found", "No such ASN: "+strconv.FormatInt(n, 10))
		return
	}
	if err != nil {
		InternalError(w, r, err)
		return
	}
	from, to, weekly, err := statsWindow(r)
	if err != nil {
		InvalidParameter(w, r, err.Error())
		return
	}
	meta, generation, err := s.statsMeta(r)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	if CacheList(w, r, generation) {
		return
	}
	rows, err := s.q.StatsASNRange(r.Context(), db.StatsASNRangeParams{
		AsnID: id, FromDay: pgTS(from), ToDay: pgTS(to),
	})
	if err != nil {
		InternalError(w, r, err)
		return
	}
	points := make([]ASNStatsPoint, len(rows))
	days := make([]time.Time, len(rows))
	for i := range rows {
		days[i] = rows[i].Day.Time
		points[i] = ASNStatsPoint{
			Day:        rows[i].Day.Time.UTC().Format("2006-01-02"),
			CountTotal: rows[i].Domains, CountV6: rows[i].V6Domains,
			Sinners: rows[i].Sinners, Heroes: rows[i].Heroes,
		}
	}
	if weekly {
		sampled := make([]ASNStatsPoint, 0, len(points))
		for _, i := range weeklySample(days) {
			sampled = append(sampled, points[i])
		}
		points = sampled
	}
	WriteJSON(w, http.StatusOK, PointsEnvelope{Points: points, Meta: meta})
}

func pgDate(t time.Time) pgtype.Date {
	return pgtype.Date{Time: t, Valid: true}
}
