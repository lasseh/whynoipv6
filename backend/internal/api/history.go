package api

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/lasseh/whynoipv6/internal/domain"
	"github.com/lasseh/whynoipv6/internal/postgres"
	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// changelogRetentionDays mirrors the changelog hypertable retention policy
// (05-schema.md — 730 days), surfaced in the history meta block.
const changelogRetentionDays = 730

// HistoryPoint is one §4.9 trajectory sample: confirmed per-dimension state
// reconstructed from the changelog + the ladder classification + the scan
// latency overlay.
type HistoryPoint struct {
	Day            string  `json:"day"`
	Base           *string `json:"base"`
	WWW            *string `json:"www"`
	NS             *string `json:"ns"`
	MX             *string `json:"mx"`
	Conn           *string `json:"conn"`
	Resources      *string `json:"resources"`
	Classification string  `json:"classification"`
	LatencyV4Ms    *int32  `json:"latency_v4_ms"`
	LatencyV6Ms    *int32  `json:"latency_v6_ms"`
}

// HistoryEnvelope is the §4.9 response shape.
type HistoryEnvelope struct {
	Host   string         `json:"host"`
	Points []HistoryPoint `json:"points"`
	Meta   struct {
		RetentionDays int       `json:"retention_days"`
		AsOf          time.Time `json:"as_of"`
	} `json:"meta"`
}

// parseHistoryWindow applies the §4.10 window contract: default to=today
// UTC, from=to−90d, interval daily|weekly, 400 on malformed input. `to` is
// clamped to today — the future holds no confirmed state, and an unbounded
// ?to=9999-12-31 would otherwise drive history's per-day synthesis loop.
func parseHistoryWindow(q url.Values) (from, to time.Time, weekly bool, err error) {
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	to = today
	if v := q.Get("to"); v != "" {
		if to, err = time.Parse("2006-01-02", v); err != nil {
			return from, to, false, errors.New("to must be YYYY-MM-DD")
		}
		if to.After(today) {
			to = today // the future holds no confirmed state
		}
	}
	from = to.AddDate(0, 0, -90)
	if v := q.Get("from"); v != "" {
		if from, err = time.Parse("2006-01-02", v); err != nil {
			return from, to, false, errors.New("from must be YYYY-MM-DD")
		}
	}
	if from.After(to) {
		return from, to, false, errors.New("from must not be after to")
	}
	switch q.Get("interval") {
	case "", "daily":
	case "weekly":
		weekly = true
	default:
		return from, to, false, errors.New("interval must be daily or weekly")
	}
	return from, to, weekly, nil
}

// capHistoryWindow bounds the synthesized window at the documented
// changelog retention (history-only — the stats endpoints share the parser
// but read real rollup rows): a wide `from` cannot make the day loop
// allocate more than changelogRetentionDays points.
func capHistoryWindow(from, to time.Time) time.Time {
	if oldest := to.AddDate(0, 0, -changelogRetentionDays); from.Before(oldest) {
		return oldest
	}
	return from
}

// dimTrack is one dimension's reconstruction input: its ascending
// transitions plus the current confirmed fallback for never-transitioned
// dimensions.
type dimTrack struct {
	events   []db.ChangelogReplayRow
	current  *db.Ipv6Status
	since    time.Time
	hasSince bool
}

// valueAt reconstructs the confirmed value at end-of-day d.
func (t *dimTrack) valueAt(d time.Time) *string {
	dayEnd := d.AddDate(0, 0, 1)
	seeded := t.current != nil && t.hasSince && t.since.Before(dayEnd)
	if len(t.events) == 0 {
		// Never transitioned: the current value has held since bootstrap.
		if seeded {
			v := string(*t.current)
			return &v
		}
		return nil
	}
	var val *string
	// State before the first transition is its old_value (held since
	// bootstrap; the window is clamped to created_at, bounding the reach).
	v0 := string(t.events[0].OldValue)
	val = &v0
	var last time.Time
	for i := range t.events {
		if !t.events[i].Ts.Time.Before(dayEnd) {
			break
		}
		v := string(t.events[i].NewValue)
		val = &v
		last = t.events[i].Ts.Time
	}
	// A confirmed flip that wrote no row — a shadow transition (03 §11) or
	// the bootstrap after a Step R reset (03 §6) — leaves the row's
	// (value, since) newer than the last replayed event. From that day on
	// the confirmed value wins, so this trajectory agrees with the status
	// block GET /domains/{host} serves.
	if seeded && t.since.After(last) {
		v := string(*t.current)
		return &v
	}
	return val
}

// getDomainHistory is GET /domains/{host}/history (07 §4.9): the confirmed
// trajectory replayed from the changelog — never raw scan observations —
// with the scan latency overlay.
func (s *Server) getDomainHistory(w http.ResponseWriter, r *http.Request) {
	host, err := domain.Canonicalize(chi.URLParam(r, "host"))
	if err != nil {
		NotFound(w, r, "Domain not found", "The host is not a valid public domain name.")
		return
	}
	row, err := s.q.DomainDetailByHost(r.Context(), host)
	if errors.Is(err, pgx.ErrNoRows) {
		NotFound(w, r, "Domain not found", "No such domain: "+host)
		return
	}
	if err != nil {
		InternalError(w, r, err)
		return
	}
	from, to, weekly, err := parseHistoryWindow(r.URL.Query())
	if err != nil {
		InvalidParameter(w, r, err.Error())
		return
	}

	generation, asOf, err := s.generation(r.Context())
	if err != nil {
		InternalError(w, r, err)
		return
	}
	if CacheList(w, r, generation) {
		return
	}

	out := HistoryEnvelope{Host: row.Host, Points: []HistoryPoint{}}
	out.Meta.RetentionDays = changelogRetentionDays
	out.Meta.AsOf = asOf.UTC()

	replay, err := s.q.ChangelogReplay(r.Context(), row.ID)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	// Baseline seeding (07 §4.9, Decision 2026-07-11): bootstrap confirmations
	// (NULL→value) write no changelog row, so a pure replay renders every
	// never-flipped domain as points:[] forever. Seed each dimension from the
	// domain row's confirmed (value, *_since) pair (done per-dimension in
	// dimTrack.valueAt). Only a domain with neither changelog transitions nor
	// any confirmed status has nothing to render → points:[].
	sextet := row.Confirmed()
	hasSeed := false
	for i, st := range sextet.Status {
		if st != nil && sextet.Since[i].Valid {
			hasSeed = true
			break
		}
	}
	if len(replay) == 0 && !hasSeed {
		WriteJSON(w, http.StatusOK, out)
		return
	}

	// Clamp the window start to the entity's existence — the pre-first-
	// transition backfill (old_value) must not reach before creation —
	// and to the changelog retention.
	created := row.CreatedAt.Time.UTC().Truncate(24 * time.Hour)
	if from.Before(created) {
		from = created
	}
	from = capHistoryWindow(from, to)

	tracks := map[string]*dimTrack{}
	for _, dim := range statusDims {
		tracks[dim] = &dimTrack{}
	}
	for i := range replay {
		if t, ok := tracks[replay[i].Field]; ok {
			t.events = append(t.events, replay[i])
		}
	}
	for i, dim := range statusDims {
		t := tracks[dim]
		t.current = sextet.Status[i]
		t.since, t.hasSince = sextet.Since[i].Time.UTC(), sextet.Since[i].Valid
	}

	latency, err := s.q.ScanLatencyDaily(r.Context(), db.ScanLatencyDailyParams{
		DomainID: row.ID, FromTs: postgres.TS(from), ToTs: postgres.TS(to.AddDate(0, 0, 1)),
	})
	if err != nil {
		InternalError(w, r, err)
		return
	}
	latByDay := make(map[string]db.ScanLatencyDailyRow, len(latency))
	for i := range latency {
		latByDay[latency[i].Day.Time.Format("2006-01-02")] = latency[i]
	}

	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		if r.Context().Err() != nil {
			return // client gone / timeout: stop synthesizing points
		}
		if weekly && d.Weekday() != time.Monday {
			continue // weekly samples the state at each ISO week boundary
		}
		p := HistoryPoint{Day: d.Format("2006-01-02")}
		confirmed := map[domain.Dimension]*domain.IPv6Status{}
		set := func(dim string, dst **string, key domain.Dimension) {
			v := tracks[dim].valueAt(d)
			*dst = v
			if v != nil {
				s := domain.IPv6Status(*v)
				confirmed[key] = &s
			}
		}
		set("base", &p.Base, domain.DimBase)
		set("www", &p.WWW, domain.DimWWW)
		set("ns", &p.NS, domain.DimNS)
		set("mx", &p.MX, domain.DimMX)
		set("conn", &p.Conn, domain.DimConn)
		set("resources", &p.Resources, domain.DimResources)
		class, _, _ := domain.Classify(confirmed)
		p.Classification = string(class)
		if l, ok := latByDay[p.Day]; ok {
			p.LatencyV4Ms, p.LatencyV6Ms = l.LatencyV4Ms, l.LatencyV6Ms
		}
		out.Points = append(out.Points, p)
	}
	WriteJSON(w, http.StatusOK, out)
}
