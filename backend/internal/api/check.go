package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/lasseh/whynoipv6/internal/campaign"
	"github.com/lasseh/whynoipv6/internal/checker"
	"github.com/lasseh/whynoipv6/internal/domain"
	"github.com/lasseh/whynoipv6/internal/observe"
	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// CheckEnvelope is the §5.1.2 job body — every key always present.
type CheckEnvelope struct {
	ID          *int64          `json:"id"` // null only on the domain-side dedupe envelope
	Host        string          `json:"host"`
	Status      string          `json:"status"`
	Cached      bool            `json:"cached"`
	CreatedAt   time.Time       `json:"created_at"`
	CompletedAt *time.Time      `json:"completed_at"`
	Error       *string         `json:"error"`
	Result      json.RawMessage `json:"result"`
	Confirmed   *CheckConfirmed `json:"confirmed"`
}

// CheckConfirmed is the read-time confirmed-state block (§5.1.3).
type CheckConfirmed struct {
	Classification string      `json:"classification"`
	ClassFlags     []string    `json:"class_flags"`
	Saint          bool        `json:"saint"`
	Status         StatusBlock `json:"status"`
	AsOf           *time.Time  `json:"as_of"`
}

// postCheck is POST /check — the API's only write path (§5.1.1): async
// enqueue + poll, never synchronous. Processing order is normative.
func (s *Server) postCheck(w http.ResponseWriter, r *http.Request) {
	NoStore(w)

	// 1. Parse + validate: non-JSON → 415; missing/non-string host → 400.
	var body struct {
		Host *string `json:"host"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) {
			InvalidParameter(w, r, "host must be a string")
			return
		}
		UnsupportedMediaType(w, r)
		return
	}
	if body.Host == nil {
		InvalidParameter(w, r, "the request body needs a host key")
		return
	}
	host, err := domain.Canonicalize(*body.Host)
	if err != nil || reservedTLD(host) {
		InvalidParameter(w, r, "not a valid public domain name")
		return
	}
	// The consumer's ensure-domain PSL evaluation (ICANN section, no
	// wildcard rule) would fail an unknown-TLD host anyway — reject it at
	// the boundary with a real reason instead of a failed job.
	if _, _, err := campaign.PSLParse(host); err != nil {
		InvalidParameter(w, r, "the host is not under a known public-suffix TLD")
		return
	}

	// 2. Rate limit: per-/64-prefix then global (§6.3).
	prefix, ok := ratePrefix(clientIP(r))
	if !ok {
		InvalidParameter(w, r, "client address unavailable")
		return
	}
	ipWin, err := s.q.CheckJobRatePrefix(r.Context(), prefix)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	if int(ipWin.N) >= s.opts.RateIPPerHour {
		rateLimitHeaders(w, s.opts.RateIPPerHour, 0)
		RateLimited(w, r, retryAfter(ipWin.MinCreated))
		return
	}
	globalWin, err := s.q.CheckJobRateGlobal(r.Context())
	if err != nil {
		InternalError(w, r, err)
		return
	}
	if int(globalWin.N) >= s.opts.RateGlobalPerHour {
		rateLimitHeaders(w, s.opts.RateGlobalPerHour, 0)
		RateLimited(w, r, retryAfter(globalWin.MinCreated))
		return
	}
	rateLimitHeaders(w, s.opts.RateIPPerHour, s.opts.RateIPPerHour-int(ipWin.N)-1)

	// 3. Lifecycle re-entry — every POST whose host already has a row,
	// including dedupe hits (§5.1.6).
	if err := s.q.DomainLiveCheckReentry(r.Context(), host); err != nil {
		InternalError(w, r, err)
		return
	}

	// 4. Dedupe, domain-side: a fresh crawl within the window serves a
	// synthetic done envelope from the latest scan_detail. No job row.
	confirmedRow, confErr := s.q.DomainConfirmed(r.Context(), host)
	if confErr == nil && confirmedRow.LastCheckedAt.Valid &&
		time.Since(confirmedRow.LastCheckedAt.Time) < s.opts.DedupeWindow {
		if env, ok := s.dedupeEnvelope(r, &confirmedRow, host); ok {
			WriteJSON(w, http.StatusOK, env)
			return
		}
	}
	if confErr != nil && !errors.Is(confErr, pgx.ErrNoRows) {
		InternalError(w, r, confErr)
		return
	}

	// 5. Dedupe, job-side: a done job within the window replays, cached.
	if job, err := s.q.CheckJobDedupe(r.Context(), db.CheckJobDedupeParams{
		Host: host, DedupeWindow: pgInterval(s.opts.DedupeWindow),
	}); err == nil {
		env := s.jobEnvelope(r, &jobFields{
			ID: job.ID, Host: job.Host, Status: string(job.Status), Result: job.Result,
			Error: job.Error, CreatedAt: job.CreatedAt, CompletedAt: job.CompletedAt,
		})
		env.Cached = true
		WriteJSON(w, http.StatusOK, env)
		return
	}

	// 6. Enqueue.
	requester, err := netip.ParseAddr(clientIP(r))
	if err != nil {
		InvalidParameter(w, r, "client address unavailable")
		return
	}
	ins, err := s.q.CheckJobInsert(r.Context(), db.CheckJobInsertParams{
		Host: host, RequesterIp: requester,
	})
	if err != nil {
		InternalError(w, r, err)
		return
	}
	w.Header().Set("Location", "/check/"+strconv.FormatInt(ins.ID, 10))
	WriteJSON(w, http.StatusAccepted, map[string]any{
		"id": ins.ID, "host": host, "status": "pending",
		"created_at": ins.CreatedAt.Time.UTC(),
	})
}

// getCheck is GET /check/{id} (§5.1.2): non-numeric or missing → 404;
// terminal jobs cache public max-age=60, in-flight stays no-store.
func (s *Server) getCheck(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id < 1 {
		NotFound(w, r, "Check not found", "Check jobs are keyed by their integer id.")
		return
	}
	job, err := s.q.CheckJobByID(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		NotFound(w, r, "Check not found", "No such check job.")
		return
	}
	if err != nil {
		InternalError(w, r, err)
		return
	}
	env := s.jobEnvelope(r, &jobFields{
		ID: job.ID, Host: job.Host, Status: string(job.Status), Result: job.Result,
		Error: job.Error, CreatedAt: job.CreatedAt, CompletedAt: job.CompletedAt,
	})
	if env.Status == "done" || env.Status == "failed" {
		w.Header().Set("Cache-Control", "public, max-age=60")
	} else {
		NoStore(w)
	}
	WriteJSON(w, http.StatusOK, env)
}

// getLatestCheck is GET /check/latest?host= — the read side of the
// shareable /check/{domain} links (§5.1.7): the freshest stored result for a
// host within live_check.link_ttl, from either source the POST dedupe uses —
// the latest crawl of a tracked domain, or the newest done job. Read-only,
// never rate-limited; a miss is 404 and the client decides whether to
// enqueue a fresh check.
func (s *Server) getLatestCheck(w http.ResponseWriter, r *http.Request) {
	host, err := domain.Canonicalize(r.URL.Query().Get("host"))
	if err != nil {
		InvalidParameter(w, r, "not a valid domain name")
		return
	}

	// Source 1 — tracked domain with a crawl inside the TTL (daily cadence
	// means every active tracked domain hits this branch).
	confirmedRow, confErr := s.q.DomainConfirmed(r.Context(), host)
	if confErr == nil && confirmedRow.LastCheckedAt.Valid &&
		time.Since(confirmedRow.LastCheckedAt.Time) < s.opts.LinkTTL {
		if env, ok := s.dedupeEnvelope(r, &confirmedRow, host); ok {
			w.Header().Set("Cache-Control", "public, max-age=60")
			WriteJSON(w, http.StatusOK, env)
			return
		}
	}
	if confErr != nil && !errors.Is(confErr, pgx.ErrNoRows) {
		InternalError(w, r, confErr)
		return
	}

	// Source 2 — the newest done live-check job inside the TTL.
	if job, err := s.q.CheckJobDedupe(r.Context(), db.CheckJobDedupeParams{
		Host: host, DedupeWindow: pgInterval(s.opts.LinkTTL),
	}); err == nil {
		env := s.jobEnvelope(r, &jobFields{
			ID: job.ID, Host: job.Host, Status: string(job.Status), Result: job.Result,
			Error: job.Error, CreatedAt: job.CreatedAt, CompletedAt: job.CompletedAt,
		})
		env.Cached = true
		w.Header().Set("Cache-Control", "public, max-age=60")
		WriteJSON(w, http.StatusOK, env)
		return
	}

	NotFound(w, r, "No recent check", "No stored result for this host inside the retention window.")
}

type jobFields struct {
	ID          int64
	Host        string
	Status      string
	Result      []byte
	Error       *string
	CreatedAt   pgtype.Timestamptz
	CompletedAt pgtype.Timestamptz
}

// jobEnvelope assembles the §5.1.2 body; confirmed is computed at read time.
func (s *Server) jobEnvelope(r *http.Request, j *jobFields) CheckEnvelope {
	id := j.ID
	env := CheckEnvelope{
		ID: &id, Host: j.Host, Status: j.Status,
		CreatedAt:   j.CreatedAt.Time.UTC(),
		CompletedAt: pgTimePtr(j.CompletedAt),
		Error:       j.Error,
	}
	if j.Result != nil {
		env.Result = json.RawMessage(j.Result)
	}
	if row, err := s.q.DomainConfirmed(r.Context(), j.Host); err == nil {
		env.Confirmed = confirmedBlock(&row)
	}
	return env
}

// dedupeEnvelope builds the §5.1.1 step-4 synthetic done envelope from the
// latest scan_detail via the shared mapper.
func (s *Server) dedupeEnvelope(r *http.Request, row *db.DomainConfirmedRow, host string) (CheckEnvelope, bool) {
	raw, err := s.q.LatestScanDetail(r.Context(), row.ID)
	if err != nil {
		return CheckEnvelope{}, false
	}
	var sr checker.ScanResult
	if err := json.Unmarshal(raw, &sr); err != nil {
		return CheckEnvelope{}, false
	}
	scanTS := row.LastCheckedAt.Time.UTC()
	// The stored detail is a real crawl: preflight was necessarily fresh at
	// scan time, so both clock inputs anchor to the scan timestamp.
	links := observe.LiveLinks(r.Context(), s.pool, sr, s.opts.ResourcesEnabled)
	result := observe.MapLiveResult(domain.Kind(row.Kind), sr, scanTS, scanTS, links, s.opts.ResourcesEnabled)
	resultRaw, _ := json.Marshal(result)

	return CheckEnvelope{
		ID: nil, Host: host, Status: "done", Cached: true,
		CreatedAt: scanTS, CompletedAt: &scanTS,
		Result:    resultRaw,
		Confirmed: confirmedBlock(row),
	}, true
}

// confirmedBlock maps the domain row to the §5.1.3 confirmed object; nil
// when nothing is confirmed yet (all six statuses NULL).
func confirmedBlock(row *db.DomainConfirmedRow) *CheckConfirmed {
	sextet := row.Confirmed()
	if sextet.AllNull() {
		return nil
	}
	flags := row.ClassFlags
	if flags == nil {
		flags = []string{}
	}
	return &CheckConfirmed{
		Classification: string(row.Classification),
		ClassFlags:     flags,
		Saint:          row.Saint,
		Status:         statusBlockTyped(&sextet),
		AsOf:           pgTimePtr(row.LastCheckedAt),
	}
}

// ratePrefix keys the limiter: /64 for IPv6, exact address for IPv4
// (§6.3). IPv4-mapped forms are unmapped first — Prefix(32) on the mapped
// 128-bit form would collapse every ::ffff:a.b.c.d client into ::/32.
func ratePrefix(ip string) (netip.Prefix, bool) {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return netip.Prefix{}, false
	}
	addr = addr.Unmap()
	bits := 32
	if addr.Is6() {
		bits = 64
	}
	p, err := addr.Prefix(bits)
	if err != nil {
		return netip.Prefix{}, false
	}
	return p, true
}

// retryAfter = ceil(3600 − (now − min(created_at))) (§5.1.1 step 2).
// min(created_at) scans as an untyped value (time.Time or nil).
func retryAfter(minCreated any) int {
	t, ok := minCreated.(time.Time)
	if !ok {
		return 3600
	}
	secs := 3600 - time.Since(t).Seconds()
	if secs < 1 {
		return 1
	}
	return int(math.Ceil(secs))
}

// rateLimitHeaders emits the structured-field rate-limit headers.
func rateLimitHeaders(w http.ResponseWriter, limit, remaining int) {
	if remaining < 0 {
		remaining = 0
	}
	w.Header().Set("RateLimit", fmt.Sprintf("limit=%d, remaining=%d, reset=3600", limit, remaining))
	w.Header().Set("RateLimit-Policy", fmt.Sprintf("%d;w=3600", limit))
}

func pgInterval(d time.Duration) pgtype.Interval {
	return pgtype.Interval{Microseconds: d.Microseconds(), Valid: true}
}
