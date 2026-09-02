// Package api is the public HTTP surface (07-api.md): a clean, versionless,
// OpenAPI-first read API at the root of api.whynoipv6.com — the
// {items,page,meta} / {points,meta} envelopes, RFC 9457 problem+json errors,
// snake_case wire, keyset pagination, per-endpoint-class caching.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// problemBase is the stable, resolvable type-URI prefix (07 §2.5).
const problemBase = "https://whynoipv6.com/problems/"

// Page is the uniform pagination block: always all three fields, cursors
// null when absent, so the type never varies (07 §2.4).
type Page struct {
	NextCursor *string `json:"next_cursor"`
	PrevCursor *string `json:"prev_cursor"`
	HasMore    bool    `json:"has_more"`
}

// Meta is the deliberately thin response metadata (07 §2.4).
type Meta struct {
	AsOf          time.Time `json:"as_of"`
	Generation    int32     `json:"generation"`
	Count         *int64    `json:"count,omitempty"`          // exact — bounded curated sets only
	CountEstimate *int64    `json:"count_estimate,omitempty"` // approximate — everything else
	License       string    `json:"license"`
	Source        string    `json:"source,omitempty"` // time-series only: "confirmed_state"
}

const license = "CC-BY-NC-4.0"

// NewMeta builds the standard meta block from the crawl generation source.
func NewMeta(asOf time.Time, generation int32) Meta {
	return Meta{AsOf: asOf.UTC(), Generation: generation, License: license}
}

// ListEnvelope is collection shape A (07 §2.4).
type ListEnvelope struct {
	Items any  `json:"items"`
	Page  Page `json:"page"`
	Meta  Meta `json:"meta"`
}

// PointsEnvelope is collection shape B: time series, never cursor-paged.
type PointsEnvelope struct {
	Points any  `json:"points"`
	Meta   Meta `json:"meta"`
}

// WriteJSON writes a JSON body with the default headers (07 §1.4).
func WriteJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Error("response encode failed", "err", err.Error())
	}
}

// WriteJSONBody encodes a 200 JSON body under a caller-set Content-Type
// (the JSON-Feed media type, §5.4).
func WriteJSONBody(w http.ResponseWriter, body any) {
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Error("response encode failed", "err", err.Error())
	}
}

// Problem is the RFC 9457 body (07 §2.5). Extensions ride as extra fields.
type Problem struct {
	Type       string       `json:"type"`
	Title      string       `json:"title"`
	Status     int          `json:"status"`
	Detail     string       `json:"detail,omitempty"`
	Instance   string       `json:"instance,omitempty"`
	Errors     []FieldError `json:"errors,omitempty"`      // validation-error only
	RetryAfter *int         `json:"retry_after,omitempty"` // rate-limited only
}

// FieldError is one validation failure (07 §2.5 validation-error).
type FieldError struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

// WriteProblem emits an application/problem+json response whose status
// member equals the HTTP status line.
func WriteProblem(w http.ResponseWriter, r *http.Request, p Problem) { //nolint:gocritic // small fixed struct; by-value keeps the call sites literal
	if p.Instance == "" && r != nil {
		p.Instance = r.URL.Path
	}
	// Errors are never cacheable: override any public cache headers and ETag
	// a handler set on the success path before the failure (RFC 9111 would
	// otherwise let a CDN pin the problem body for the full s-maxage).
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Del("ETag")
	w.Header().Set("Content-Type", "application/problem+json")
	if p.RetryAfter != nil {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", *p.RetryAfter))
	}
	w.WriteHeader(p.Status)
	if err := json.NewEncoder(w).Encode(p); err != nil {
		slog.Error("problem encode failed", "err", err.Error())
	}
}

// The fixed problem constructors (07 §2.5 — the complete type set).

// NotFound emits the 404 not-found problem: an unknown or uncanonicalizable
// domain, country, asn, provider, campaign or resource (07 §2.5).
func NotFound(w http.ResponseWriter, r *http.Request, title, detail string) {
	WriteProblem(w, r, Problem{Type: problemBase + "not-found", Title: title, Status: http.StatusNotFound, Detail: detail})
}

// InvalidParameter emits the 400 invalid-parameter problem: a malformed
// cursor, format, host or request field (07 §2.5).
func InvalidParameter(w http.ResponseWriter, r *http.Request, detail string) {
	WriteProblem(w, r, Problem{Type: problemBase + "invalid-parameter", Title: "Invalid parameter", Status: http.StatusBadRequest, Detail: detail})
}

// invalidParam renders a parameter error, dropping the ErrCursorInvalid
// sentinel prefix the parse helpers wrap it in (07 §2.5).
func invalidParam(w http.ResponseWriter, r *http.Request, err error) {
	InvalidParameter(w, r, strings.TrimPrefix(err.Error(), "invalid cursor: "))
}

// ValidationError emits the 422 validation-error problem: a filter value
// outside its closed enum, with the per-field reasons (07 §2.5).
func ValidationError(w http.ResponseWriter, r *http.Request, errs []FieldError) {
	WriteProblem(w, r, Problem{Type: problemBase + "validation-error", Title: "Validation error",
		Status: http.StatusUnprocessableEntity, Errors: errs})
}

// ScopeRequired emits the 422 scope-required problem: a valid filter value
// that needs an indexed companion scope, named in the detail (07 §3.3).
func ScopeRequired(w http.ResponseWriter, r *http.Request, detail string) {
	WriteProblem(w, r, Problem{Type: problemBase + "scope-required", Title: "Filter requires an indexed scope",
		Status: http.StatusUnprocessableEntity, Detail: detail})
}

// RateLimited emits the 429 rate-limited problem, carrying retry_after in
// the body and the Retry-After header (07 §2.5, §6.3).
func RateLimited(w http.ResponseWriter, r *http.Request, retryAfter int) {
	WriteProblem(w, r, Problem{Type: problemBase + "rate-limited", Title: "Rate limit exceeded",
		Status: http.StatusTooManyRequests, RetryAfter: &retryAfter})
}

// UnsupportedMediaType emits the 415 unsupported-media-type problem: a
// POST /check body that is not JSON (07 §2.5).
func UnsupportedMediaType(w http.ResponseWriter, r *http.Request) {
	WriteProblem(w, r, Problem{Type: problemBase + "unsupported-media-type", Title: "Unsupported media type",
		Status: http.StatusUnsupportedMediaType})
}

// ManifestUnavailable emits the 503 manifest-unavailable problem: the
// /datasets manifest is missing or unparseable — the only 503 (07 §2.5).
func ManifestUnavailable(w http.ResponseWriter, r *http.Request) {
	WriteProblem(w, r, Problem{Type: problemBase + "manifest-unavailable", Title: "Dataset manifest unavailable",
		Status: http.StatusServiceUnavailable})
}

// InternalError logs the underlying fault (09-ops §13 reserves the error
// level for exactly these) and emits the generic problem — the detail never
// leaks the error text.
func InternalError(w http.ResponseWriter, r *http.Request, err error) {
	// A dead request context means the client hung up (or the server is
	// draining) and whatever query was in flight surfaced the cancellation
	// as its own error. That is not a server fault: an aborted browser
	// fetch would otherwise log ERROR + 500 on every navigation race.
	// Nobody is left to read a problem body, so none is written.
	if r.Context().Err() != nil {
		slog.Debug("request canceled mid-flight", "path", r.URL.Path)
		return
	}
	if err != nil {
		slog.Error("internal error", "path", r.URL.Path, "err", err.Error())
	}
	WriteProblem(w, r, Problem{Type: problemBase + "internal-error", Title: "Internal error",
		Status: http.StatusInternalServerError, Detail: "An unexpected error occurred."})
}

// CacheList sets the list/leaderboard/stats class Cache-Control (07 §6.1)
// and the deterministic generation-seeded ETag; reports true when the
// request was satisfied by a 304.
func CacheList(w http.ResponseWriter, r *http.Request, generation int32) bool {
	w.Header().Set("Cache-Control",
		"public, max-age=300, s-maxage=3600, stale-while-revalidate=600, stale-if-error=86400")
	return applyETag(w, r, fmt.Sprintf(`W/"g%d-%s"`, generation, queryFingerprint(r)))
}

// CacheDetail sets the entity-detail class (07 §6.1 row 2): the same
// Cache-Control as the list class, but the ETag is tied to that entity's
// last confirmed transition, not only the daily generation. A transition
// therefore invalidates the CDN copy when it commits rather than at the
// next stats tick. The generation stays in the seed because the detail body
// also carries generation-cadence numbers.
func CacheDetail(w http.ResponseWriter, r *http.Request, generation int32, lastChange time.Time) bool {
	w.Header().Set("Cache-Control",
		"public, max-age=300, s-maxage=3600, stale-while-revalidate=600, stale-if-error=86400")
	return applyETag(w, r, fmt.Sprintf(`W/"d%d-%d-%s"`, generation, lastChange.UnixNano(), queryFingerprint(r)))
}

// CacheChangelog: the live-surface class — ETag from the scope window's
// max(changelog.ts), never the daily generation (07 §6.1).
func CacheChangelog(w http.ResponseWriter, r *http.Request, maxTS time.Time) bool {
	w.Header().Set("Cache-Control", "public, max-age=300")
	return applyETag(w, r, fmt.Sprintf(`W/"cl%d-%s"`, maxTS.UnixNano(), queryFingerprint(r)))
}

// CacheShort: rolling counters that are not generation-scoped (07 §6.1
// deviation, GET /stats/crawler). No ETag — the value moves with every
// checkpoint, and the generation-seeded list class would 304-freeze a
// "last 24 hours" number until the next daily stats tick.
func CacheShort(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "public, max-age=60")
}

// NoStore marks the no-store class (POST /check, in-flight poll, /ip, health).
func NoStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
}

// applyETag sets the (weak — nginx's gzip filter strips strong ETags but
// preserves W/ ones) ETag and answers a matching conditional GET with 304.
func applyETag(w http.ResponseWriter, r *http.Request, etag string) bool {
	w.Header().Set("ETag", etag)
	if ifNoneMatch(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return true
	}
	return false
}

// ifNoneMatch implements the RFC 9110 §13.1.2 rule: the header is a
// comma-separated candidate list compared weakly (W/ prefixes ignored on
// both sides), and "*" matches any current representation.
func ifNoneMatch(header, etag string) bool {
	if header == "" {
		return false
	}
	opaque := strings.TrimPrefix(etag, "W/")
	for cand := range strings.SplitSeq(header, ",") {
		cand = strings.TrimSpace(cand)
		if cand == "*" || strings.TrimPrefix(cand, "W/") == opaque {
			return true
		}
	}
	return false
}

// queryFingerprint keys the ETag by the canonicalized query string.
func queryFingerprint(r *http.Request) string {
	q := r.URL.Query()
	return fmt.Sprintf("%x", hashString(q.Encode()))
}

// hashString is FNV-1a (deterministic across instances; not security).
func hashString(s string) uint64 {
	const offset, prime = 14695981039346656037, 1099511628211
	h := uint64(offset)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime
	}
	return h
}

// generation resolves the envelope meta sources for this request (07 §2.4)
// through the metaSource seam, so detail handlers share the list rim's
// fakeable freshness source.
func (s *Server) generation(ctx context.Context) (int32, time.Time, error) {
	return s.meta.Generation(ctx)
}
