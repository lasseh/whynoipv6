package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// Sort orderings — each binds a strict total order and a seek-tuple shape
// (07 §3.2).
const (
	SortRank       = "rank"       // (rank, id) — the default and all scoped lists
	SortRankDesc   = "-rank"      // (rank, id) descending
	SortHost       = "host"       // host alone (campaign members, ?q= search)
	SortDependents = "dependents" // (rank IS NULL, rank, id) — null-flag-first

	// The /asns leaderboard orderings (07 §4.6): (count, number) descending;
	// the count rides the Seek.Rank slot, the number rides Seek.ID.
	SortCountV6    = "count_v6"
	SortCountTotal = "count_total"

	// The changelog feeds (07 §4.8): (ts, domain_id, field) descending.
	// ts is stable across generations, so a stale-generation cursor stays
	// exact — no re-anchor adjustment.
	SortChangelog = "-ts"
)

// Limit caps (07 §3.2).
const (
	DefaultLimit = 50
	MaxLimit     = 200
)

// Query-param names shared across the paging machinery.
const (
	paramCursor     = "cursor"
	paramLimit      = "limit"
	paramAfterRank  = "after_rank"
	paramAroundRank = "around_rank"
	paramFlag       = "flag"
	paramTLD        = "tld"
	paramClass      = "class"
	paramCountry    = "country"
	paramASN        = "asn"
	paramProvider   = "provider"
)

// Cursor is the decoded opaque token. D is the direction bit: "" walks
// forward from K, "p" walks backward (the §3.2 bidirectional support) —
// opaque to clients either way.
type Cursor struct {
	V int    `json:"v"`
	G int32  `json:"g"`
	S string `json:"s"`
	F string `json:"f"`
	K []any  `json:"k"`
	D string `json:"d,omitempty"`

	// ReAnchored is set when the cursor's generation differed from the
	// current one: the seek is best-effort on last_rank (07 §3.2 staleness).
	ReAnchored bool `json:"-"`
}

// Backward reports the direction bit.
func (c *Cursor) Backward() bool { return c.D == "p" }

// Seek is the typed seek tuple for the query builder.
type Seek struct {
	Rank     *int32 // rank orderings + dependents (nil on the null tail)
	ID       int64  // tiebreaker (0 after re-anchoring)
	Host     string // host ordering
	RankNull bool   // dependents ordering: the null-flag component
	TS       int64  // changelog ordering: UnixNano event time
	Field    string // changelog ordering: dimension tiebreaker
}

// ErrCursorInvalid → 400 invalid-parameter (malformed token, wrong sort,
// or filter-fingerprint mismatch).
var ErrCursorInvalid = errors.New("invalid cursor")

// EncodeCursor mints the opaque base64url token (forward direction).
func EncodeCursor(g int32, sortKey, fingerprint string, k []any) string {
	return encodeCursorDir(g, sortKey, fingerprint, k, "")
}

func encodeCursorDir(g int32, sortKey, fingerprint string, k []any, dir string) string {
	raw, _ := json.Marshal(Cursor{V: 1, G: g, S: sortKey, F: fingerprint, K: k, D: dir})
	return base64.RawURLEncoding.EncodeToString(raw)
}

// DecodeCursor validates and decodes a token against the request's sort,
// filter fingerprint, and the current generation (staleness → re-anchor).
func DecodeCursor(token, wantSort, wantFingerprint string, currentG int32) (*Cursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, fmt.Errorf("%w: not base64url", ErrCursorInvalid)
	}
	var c Cursor
	if err := json.Unmarshal(raw, &c); err != nil || c.V != 1 {
		return nil, fmt.Errorf("%w: malformed token", ErrCursorInvalid)
	}
	if c.S != wantSort {
		return nil, fmt.Errorf("%w: cursor sort %q does not match request sort %q", ErrCursorInvalid, c.S, wantSort)
	}
	if c.F != wantFingerprint {
		return nil, fmt.Errorf("%w: cursor filters do not match the request", ErrCursorInvalid)
	}
	if c.G != currentG {
		c.ReAnchored = true // best-effort seek on last_rank in the new generation
	}
	return &c, nil
}

// SeekTuple converts the raw K payload into the typed seek for the sort.
// Re-anchored cursors zero the id tiebreaker (rank-based orderings).
func (c *Cursor) SeekTuple() (Seek, error) {
	var s Seek
	switch c.S {
	case SortRank, SortRankDesc, SortCountV6, SortCountTotal:
		if len(c.K) != 2 {
			return s, fmt.Errorf("%w: seek tuple shape", ErrCursorInvalid)
		}
		rank, ok1 := jsonInt32(c.K[0])
		id, ok2 := jsonInt64(c.K[1])
		if !ok1 || !ok2 {
			return s, fmt.Errorf("%w: seek tuple types", ErrCursorInvalid)
		}
		s.Rank, s.ID = &rank, id
	case SortHost:
		if len(c.K) != 1 {
			return s, fmt.Errorf("%w: seek tuple shape", ErrCursorInvalid)
		}
		host, ok := c.K[0].(string)
		if !ok {
			return s, fmt.Errorf("%w: seek tuple types", ErrCursorInvalid)
		}
		s.Host = host
	case SortChangelog:
		if len(c.K) != 3 {
			return s, fmt.Errorf("%w: seek tuple shape", ErrCursorInvalid)
		}
		ts, ok1 := jsonInt64(c.K[0])
		id, ok2 := jsonInt64(c.K[1])
		field, ok3 := c.K[2].(string)
		if !ok1 || !ok2 || !ok3 {
			return s, fmt.Errorf("%w: seek tuple types", ErrCursorInvalid)
		}
		s.TS, s.ID, s.Field = ts, id, field
		return s, nil // ts-ordered: exact across generations, never re-anchored
	case SortDependents:
		if len(c.K) != 3 {
			return s, fmt.Errorf("%w: seek tuple shape", ErrCursorInvalid)
		}
		isNull, ok1 := c.K[0].(bool)
		id, ok3 := jsonInt64(c.K[2])
		if !ok1 || !ok3 {
			return s, fmt.Errorf("%w: seek tuple types", ErrCursorInvalid)
		}
		s.RankNull = isNull
		if !isNull {
			rank, ok := jsonInt32(c.K[1])
			if !ok {
				return s, fmt.Errorf("%w: seek tuple types", ErrCursorInvalid)
			}
			s.Rank = &rank
		}
		s.ID = id
	default:
		return s, fmt.Errorf("%w: unknown sort %q", ErrCursorInvalid, c.S)
	}
	if c.ReAnchored {
		s.ID = 0 // seek to the same rank/host boundary in the new generation
	}
	return s, nil
}

// FilterFingerprint hashes the normalized filter set — every query param
// except the paging machinery itself (07 §3.2 `f`).
func FilterFingerprint(q url.Values) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		switch k {
		case paramCursor, paramLimit, paramAfterRank, paramAroundRank:
			continue // paging params are not filters
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		vals := append([]string(nil), q[k]...)
		sort.Strings(vals)
		b.WriteString(k + "=" + strings.Join(vals, ",") + ";")
	}
	return fmt.Sprintf("%x", hashString(b.String()))
}

// ParseLimit applies the default/cap (07 §3.2).
func ParseLimit(q url.Values) (int, error) {
	return ParseLimitCap(q, MaxLimit)
}

// ParseLimitCap is ParseLimit with a caller-set ceiling — the CSV export
// raises the cap to export.csv_max_rows (07 §5.5).
func ParseLimitCap(q url.Values, maxLimit int) (int, error) {
	raw := q.Get(paramLimit)
	if raw == "" {
		return DefaultLimit, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%w: limit must be a positive integer", ErrCursorInvalid)
	}
	if n > maxLimit {
		n = maxLimit
	}
	return n, nil
}

// ParseAfterRank handles the deep-link escape hatch — rank-ordered views
// only; the host ordering has no random-access param (07 §3.2).
func ParseAfterRank(q url.Values, sortKey string) (*int32, error) {
	raw := q.Get(paramAfterRank)
	if raw == "" {
		return nil, nil
	}
	if sortKey != SortRank && sortKey != SortRankDesc {
		return nil, fmt.Errorf("%w: after_rank is not available on the %s ordering", ErrCursorInvalid, sortKey)
	}
	n, err := strconv.ParseInt(raw, 10, 32)
	if err != nil || n < 0 {
		return nil, fmt.Errorf("%w: after_rank must be a non-negative integer", ErrCursorInvalid)
	}
	rank := int32(n)
	return &rank, nil
}

// ParseAroundRank handles the centered-window deep link (07 §3.2): the
// ⌈limit/2⌉ rows ranked ≤ N plus the ⌊limit/2⌋ rows ranked > N. Offered on
// the default rank ordering only.
func ParseAroundRank(q url.Values, sortKey string) (*int32, error) {
	raw := q.Get(paramAroundRank)
	if raw == "" {
		return nil, nil
	}
	if sortKey != SortRank {
		return nil, fmt.Errorf("%w: around_rank is only available on the rank ordering", ErrCursorInvalid)
	}
	n, err := strconv.ParseInt(raw, 10, 32)
	if err != nil || n < 0 {
		return nil, fmt.Errorf("%w: around_rank must be a non-negative integer", ErrCursorInvalid)
	}
	rank := int32(n)
	return &rank, nil
}

// Residual params: the unindexed-or-selective predicate class that requires
// an indexed scope (07 §3.3 guardrail).
var residualParams = []string{paramFlag, "base", "www", "ns", "mx", "conn", "resources", paramTLD, paramProvider, "hosting"}

// scopeParams are the indexed prefilters that satisfy the guardrail.
var scopeParams = []string{paramClass, paramCountry, paramASN}

// ErrScopeRequired → 422 scope-required.
var ErrScopeRequired = errors.New("scope required")

// ValidateResiduals enforces the §3.3 guardrail: a residual filter needs an
// indexed scope, and at most ONE residual per request. pathScoped marks
// requests whose route already binds an indexed scope (the
// /providers/{id}/domains path — 07 §4.6).
func ValidateResiduals(q url.Values, pathScoped bool) error {
	var present []string
	for _, p := range residualParams {
		if q.Get(p) != "" {
			present = append(present, p)
		}
	}
	if len(present) == 0 {
		return nil
	}
	if len(present) > 1 {
		return fmt.Errorf("%w: at most one of %s may be combined; use the indexed axes (class, country, asn) instead",
			ErrScopeRequired, strings.Join(present, ", "))
	}
	if pathScoped {
		return nil
	}
	for _, s := range scopeParams {
		if q.Get(s) != "" {
			return nil
		}
	}
	return fmt.Errorf("%w: %s= requires one of class=, country=, or asn=", ErrScopeRequired, present[0])
}

// BuildPage assembles the page block from an N+1 window walk (07 §3.2
// bidirectional): forwardMore = rows exist after lastK (mints next_cursor),
// backwardMore = rows exist before firstK (mints prev_cursor). has_more is
// asserted only when the matching cursor is mintable — an empty page (a
// prev walk past the start) never claims a continuation it cannot encode.
func BuildPage(g int32, sortKey, fingerprint string, forwardMore, backwardMore bool, firstK, lastK []any) Page {
	p := Page{HasMore: forwardMore && lastK != nil}
	if p.HasMore {
		next := encodeCursorDir(g, sortKey, fingerprint, lastK, "")
		p.NextCursor = &next
	}
	if backwardMore && firstK != nil {
		prev := encodeCursorDir(g, sortKey, fingerprint, firstK, "p")
		p.PrevCursor = &prev
	}
	return p
}

// trimWindow applies the N+1 window convention shared by every keyset
// family: forward overflow trims the tail; a backward fetch — already
// re-reversed into display order — carries its overflow row at the FRONT.
// positioned marks a forward page reached via any positioning param, which
// always has at least the row the cursor came from before it.
func trimWindow[T any](rows []T, limit int, backward, positioned bool) (trimmed []T, forwardMore, backwardMore bool) {
	overflow := len(rows) > limit
	if backward {
		if overflow {
			rows = rows[1:]
		}
		return rows, true, overflow
	}
	if overflow {
		rows = rows[:limit]
	}
	return rows, overflow, positioned
}

func jsonInt32(v any) (int32, bool) {
	switch n := v.(type) {
	case float64:
		return int32(n), true
	case int32:
		return n, true
	case int:
		return int32(n), true
	case int64:
		return int32(n), true
	default:
		return 0, false
	}
}

func jsonInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int64:
		return n, true
	case int:
		return int64(n), true
	default:
		return 0, false
	}
}
