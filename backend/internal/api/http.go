// Package api is the public HTTP surface (07-api.md): a clean, versionless,
// OpenAPI-first read API at the root of api.whynoipv6.com — the
// {items,page,meta} / {points,meta} envelopes, RFC 9457 problem+json errors,
// snake_case wire, keyset pagination, per-endpoint-class caching.
package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
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

func NotFound(w http.ResponseWriter, r *http.Request, title, detail string) {
	WriteProblem(w, r, Problem{Type: problemBase + "not-found", Title: title, Status: http.StatusNotFound, Detail: detail})
}

func InvalidParameter(w http.ResponseWriter, r *http.Request, detail string) {
	WriteProblem(w, r, Problem{Type: problemBase + "invalid-parameter", Title: "Invalid parameter", Status: http.StatusBadRequest, Detail: detail})
}

func ValidationError(w http.ResponseWriter, r *http.Request, errs []FieldError) {
	WriteProblem(w, r, Problem{Type: problemBase + "validation-error", Title: "Validation error",
		Status: http.StatusUnprocessableEntity, Errors: errs})
}

func ScopeRequired(w http.ResponseWriter, r *http.Request, detail string) {
	WriteProblem(w, r, Problem{Type: problemBase + "scope-required", Title: "Filter requires an indexed scope",
		Status: http.StatusUnprocessableEntity, Detail: detail})
}

func RateLimited(w http.ResponseWriter, r *http.Request, retryAfter int) {
	WriteProblem(w, r, Problem{Type: problemBase + "rate-limited", Title: "Rate limit exceeded",
		Status: http.StatusTooManyRequests, RetryAfter: &retryAfter})
}

func NotAcceptable(w http.ResponseWriter, r *http.Request) {
	WriteProblem(w, r, Problem{Type: problemBase + "not-acceptable", Title: "Not acceptable", Status: http.StatusNotAcceptable})
}

func UnsupportedMediaType(w http.ResponseWriter, r *http.Request) {
	WriteProblem(w, r, Problem{Type: problemBase + "unsupported-media-type", Title: "Unsupported media type",
		Status: http.StatusUnsupportedMediaType})
}

func ManifestUnavailable(w http.ResponseWriter, r *http.Request) {
	WriteProblem(w, r, Problem{Type: problemBase + "manifest-unavailable", Title: "Dataset manifest unavailable",
		Status: http.StatusServiceUnavailable})
}

func InternalError(w http.ResponseWriter, r *http.Request) {
	WriteProblem(w, r, Problem{Type: problemBase + "internal-error", Title: "Internal error",
		Status: http.StatusInternalServerError, Detail: "An unexpected error occurred."})
}

// CacheList sets the list/leaderboard/stats class Cache-Control (07 §6.1)
// and the deterministic generation-seeded ETag; reports true when the
// request was satisfied by a 304.
func CacheList(w http.ResponseWriter, r *http.Request, generation int32) bool {
	w.Header().Set("Cache-Control",
		"public, max-age=300, s-maxage=3600, stale-while-revalidate=600, stale-if-error=86400")
	return applyETag(w, r, fmt.Sprintf(`"g%d-%s"`, generation, queryFingerprint(r)))
}

// CacheChangelog: the live-surface class — ETag from the scope window's
// max(changelog.ts), never the daily generation (07 §6.1).
func CacheChangelog(w http.ResponseWriter, r *http.Request, maxTS time.Time) bool {
	w.Header().Set("Cache-Control", "public, max-age=300")
	return applyETag(w, r, fmt.Sprintf(`"cl%d-%s"`, maxTS.UnixNano(), queryFingerprint(r)))
}

// NoStore marks the no-store class (POST /check, in-flight poll, /ip, health).
func NoStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
}

func applyETag(w http.ResponseWriter, r *http.Request, etag string) bool {
	w.Header().Set("ETag", etag)
	if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
		w.WriteHeader(http.StatusNotModified)
		return true
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
