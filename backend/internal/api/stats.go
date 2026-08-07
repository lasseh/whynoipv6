package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/lasseh/whynoipv6/internal/postgres"
	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// sourceConfirmedState labels every §4.10 series: sourced from the
// confirmed-state stats_* tables so graphs match public lists exactly. The
// measurement-flavored scan_daily_adoption cagg is never exposed (OPEN-5).
const sourceConfirmedState = "confirmed_state"

// sourceTelemetry labels GET /stats/crawler alone. Crawler throughput is
// neither confirmed state nor the measurement cagg — it is how much work the
// fleet did. statsMeta hardcodes confirmed_state for every series, so this
// endpoint deliberately builds its own meta rather than reusing it; labelling
// telemetry as confirmed_state is exactly the conflation §4.10 forbids.
const sourceTelemetry = "telemetry"

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

// sampleWeekly applies the §4.10 weekly interval to any point series: the
// points survive unchanged on the daily interval, otherwise weeklySample
// selects them via the parallel days slice.
// Callers build points and days in lockstep from the same rows; a length
// mismatch is a caller bug and indexes out of range rather than silently
// serving the daily series under the weekly contract.
func sampleWeekly[T any](points []T, days []time.Time, weekly bool) []T {
	if !weekly {
		return points
	}
	sampled := make([]T, 0, len(points))
	for _, i := range weeklySample(days) {
		sampled = append(sampled, points[i])
	}
	return sampled
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
	Saints             *int32 `json:"saints"`
	Disabled           *int32 `json:"disabled"`
	BaseSupported      *int32 `json:"base_supported"`
	WwwSupported       *int32 `json:"www_supported"`
	NsSupported        *int32 `json:"ns_supported"`
	MxSupported        *int32 `json:"mx_supported"`
	ConnSupported      *int32 `json:"conn_supported"`
	ResourcesSupported *int32 `json:"resources_supported"`
	TopHeroes          *int32 `json:"top_heroes"`
	TopNameserver      *int32 `json:"top_nameserver"`
	// The live-set counters (000008). Every field above counts the ranked
	// subset; these five do not, so tracked_total >= domains. NULL on rows
	// snapshotted before the columns existed — clients render an em dash.
	TrackedTotal  *int32 `json:"tracked_total"`
	PtrSupported  *int32 `json:"ptr_supported"`
	PtrGraded     *int32 `json:"ptr_graded"`
	SmtpSupported *int32 `json:"smtp_supported"`
	SmtpGraded    *int32 `json:"smtp_graded"`
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
		FromDay: postgres.Date(from), ToDay: postgres.Date(to),
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
			Saints: rows[i].Saints, Disabled: rows[i].Disabled,
			BaseSupported: rows[i].BaseSupported, WwwSupported: rows[i].WwwSupported,
			NsSupported: rows[i].NsSupported, MxSupported: rows[i].MxSupported,
			ConnSupported: rows[i].ConnSupported, ResourcesSupported: rows[i].ResourcesSupported,
			TopHeroes: rows[i].TopHeroes, TopNameserver: rows[i].TopNameserver,
			TrackedTotal: rows[i].TrackedTotal,
			PtrSupported: rows[i].PtrSupported, PtrGraded: rows[i].PtrGraded,
			SmtpSupported: rows[i].SmtpSupported, SmtpGraded: rows[i].SmtpGraded,
		}
	}
	WriteJSON(w, http.StatusOK, PointsEnvelope{Points: sampleWeekly(points, days, weekly), Meta: meta})
}

// CrawlerStats is GET /stats/crawler: a single resource, so it takes §2.4's
// object-with-sibling-meta shape rather than {points} or {items}.
//
// Two numbers, by design — see the SELECT-list note on CrawlerThroughput in
// metrics.sql for why nothing else in crawler_metrics is public.
type CrawlerStats struct {
	// Checked24h counts check operations attempted in the last 24 hours,
	// not distinct hosts: a host re-checked by a live check, a campaign
	// refresh or a retry counts each time, so this can exceed the tracked
	// domain count. Failed attempts count too — it is work done, not work
	// that succeeded.
	Checked24h int64 `json:"checked_24h"`
	// Latest is the newest checkpoint regardless of the window, so a dead
	// crawler reads as a stale timestamp rather than null. Null only on a
	// fresh install that has never run.
	Latest *time.Time `json:"latest"`
	Meta   Meta       `json:"meta"`
}

// getCrawlerStats is GET /stats/crawler — rolling throughput, not a series.
// Not folded into /stats/overview because the lifecycles differ:
// stats_global_daily is a once-a-day snapshot, this is a rolling 24-hour
// window that moves continuously.
func (s *Server) getCrawlerStats(w http.ResponseWriter, r *http.Request) {
	row, err := s.q.CrawlerThroughput(r.Context())
	if err != nil {
		InternalError(w, r, err)
		return
	}
	generation, asOf, err := s.generation(r.Context())
	if err != nil {
		InternalError(w, r, err)
		return
	}
	// as_of answers "when was this number true", which for a rolling counter
	// is the newest checkpoint — not when the daily stats job last ran.
	var latest *time.Time
	if row.Latest.Valid {
		observed := row.Latest.Time.UTC()
		latest, asOf = &observed, observed
	}
	meta := NewMeta(asOf, generation)
	meta.Source = sourceTelemetry
	CacheShort(w)
	WriteJSON(w, http.StatusOK, CrawlerStats{Checked24h: row.Checked24h, Latest: latest, Meta: meta})
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
		CountryID: id, FromDay: postgres.Date(from), ToDay: postgres.Date(to),
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
	WriteJSON(w, http.StatusOK, PointsEnvelope{Points: sampleWeekly(points, days, weekly), Meta: meta})
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
		CampaignID: row.ID, FromDay: postgres.Date(from), ToDay: postgres.Date(to),
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
	WriteJSON(w, http.StatusOK, PointsEnvelope{Points: sampleWeekly(points, days, weekly), Meta: meta})
}

// ChangePoint is a per-day transition tally. Event counts, not state: a
// domain that flips on and off in one day contributes to both columns,
// because the chart is about churn and the net is the visible difference.
type ChangePoint struct {
	Day    string `json:"day"`
	Gained int64  `json:"gained"`
	Lost   int64  `json:"lost"`
}

// getChangeStats is GET /stats/changes — apex IPv6 gained and lost per day.
//
// Cached on the changelog class, not the stats class: the crawler commits
// transitions continuously, and a generation-seeded ETag would 304-freeze
// this endpoint until the next daily stats tick (07 §6.1).
func (s *Server) getChangeStats(w http.ResponseWriter, r *http.Request) {
	from, to, weekly, err := statsWindow(r)
	if err != nil {
		InvalidParameter(w, r, err.Error())
		return
	}
	// Same server-side floor the per-domain history uses: a wide `from`
	// must not walk past the columnstore boundary of a forever-retained
	// hypertable.
	from = capHistoryWindow(from, to)
	maxTS, err := s.q.ChangelogMaxTS(r.Context())
	if err != nil {
		InternalError(w, r, err)
		return
	}
	generation, asOf, err := s.generation(r.Context())
	if err != nil {
		InternalError(w, r, err)
		return
	}
	if CacheChangelog(w, r, maxTS.Time) {
		return
	}
	rows, err := s.q.StatsChangesRange(r.Context(), db.StatsChangesRangeParams{
		FromDay: postgres.TS(from), ToDay: postgres.TS(to),
	})
	if err != nil {
		InternalError(w, r, err)
		return
	}
	points := make([]ChangePoint, len(rows))
	days := make([]time.Time, len(rows))
	for i := range rows {
		days[i] = rows[i].Day.Time
		points[i] = ChangePoint{
			Day:    rows[i].Day.Time.UTC().Format("2006-01-02"),
			Gained: rows[i].Gained, Lost: rows[i].Lost,
		}
	}
	meta := NewMeta(asOf, generation)
	meta.Source = sourceConfirmedState
	WriteJSON(w, http.StatusOK, PointsEnvelope{Points: sampleWeekly(points, days, weekly), Meta: meta})
}

// Network series sizing. Seven small multiples is what the panel draws; the
// cap bounds a response that is one series per network wide.
const (
	defaultNetworks = 7
	maxNetworks     = 10
)

// parseNetworkLimit takes the house limit semantics — 400 on a non-positive
// or unparseable value, silently clamped above the ceiling — with this
// route's own default rather than the collection default of 50.
func parseNetworkLimit(r *http.Request) (int, error) {
	if r.URL.Query().Get("limit") == "" {
		return defaultNetworks, nil
	}
	return ParseLimitCap(r.URL.Query(), maxNetworks)
}

// NetworkTrend is one network's series. `asn` is the stable key; `name` is
// for display only and is NOT unique — two entries may legitimately carry the
// same name, and a consumer that groups on it will merge unrelated networks
// (see StatsTopNetworks in stats.sql).
type NetworkTrend struct {
	ASN    int64          `json:"asn"`
	Name   string         `json:"name"`
	Points []NetworkPoint `json:"points"`
}

// NetworkPoint is the §4.6 count pair and nothing else. Deliberately not
// ASNStatsPoint: this query does not select the tier counters, and emitting
// them as null would claim they were measured and empty.
type NetworkPoint struct {
	Day        string `json:"day"`
	CountTotal *int32 `json:"count_total"`
	CountV6    *int32 `json:"count_v6"`
}

// NetworkStats is grouped series rather than flat points: each network
// needs its own box, so {points} would force the client to re-group.
type NetworkStats struct {
	Networks []NetworkTrend `json:"networks"`
	Meta     Meta           `json:"meta"`
}

// getNetworkStats is GET /stats/networks — the top-N networks in one request.
//
// Counts are served, never a precomputed share. Coverage was still growing
// over the early window (AS13335 held 35k domains on the first crawl day
// against 324k now), so a share moves when the denominator moves; exposing
// count_total lets the client see that rather than hiding it behind a
// percentage the panel then tells readers not to trust.
func (s *Server) getNetworkStats(w http.ResponseWriter, r *http.Request) {
	from, to, weekly, err := statsWindow(r)
	if err != nil {
		InvalidParameter(w, r, err.Error())
		return
	}
	limit, err := parseNetworkLimit(r)
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
	rows, err := s.q.StatsTopNetworks(r.Context(), db.StatsTopNetworksParams{
		FromDay: postgres.TS(from), ToDay: postgres.TS(to), TopN: int32(limit),
	})
	if err != nil {
		InternalError(w, r, err)
		return
	}
	// Rows arrive ordered by network rank then day, so a network's run is
	// contiguous and grouping is a single pass. Keyed on the AS number: two
	// adjacent runs can share a name and must stay separate boxes.
	networks := make([]NetworkTrend, 0, limit)
	var days [][]time.Time
	for _, row := range rows {
		if len(networks) == 0 || networks[len(networks)-1].ASN != row.Asn {
			networks = append(networks, NetworkTrend{ASN: row.Asn, Name: row.Name})
			days = append(days, nil)
		}
		i := len(networks) - 1
		networks[i].Points = append(networks[i].Points, NetworkPoint{
			Day:        row.Day.Time.UTC().Format("2006-01-02"),
			CountTotal: row.Domains, CountV6: row.V6Domains,
		})
		days[i] = append(days[i], row.Day.Time)
	}
	for i := range networks {
		networks[i].Points = sampleWeekly(networks[i].Points, days[i], weekly)
	}
	WriteJSON(w, http.StatusOK, NetworkStats{Networks: networks, Meta: meta})
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
		AsnID: id, FromDay: postgres.TS(from), ToDay: postgres.TS(to),
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
	WriteJSON(w, http.StatusOK, PointsEnvelope{Points: sampleWeekly(points, days, weekly), Meta: meta})
}
