package checker

import (
	"context"
	"errors"
	"net"
)

// AAAAAnswer is the result of a (possibly quorum'd) AAAA resolution.
type AAAAAnswer struct {
	IPs        []net.IP
	CNAMEChain []string    // full chase, feeds cname_chain + CDN detection
	TTL        int         // min TTL of the returned answer set
	Rcode      string      // "NOERROR", "NXDOMAIN", ...
	Quorum     *QuorumInfo // nil when not quorum-resolved
	AOutcome   string      // "a_present" | "a_absent" | "a_error"; set only when the
	// AAAA quorum result was NOERROR-empty — the conditional
	// bulk-resolver A lookup (02-observation-model.md). Empty otherwise.
	CDOutcome string // "cd_present" | "cd_empty" | "cd_fail"; set only when the AAAA quorum
	// was `error` from all-SERVFAIL/REFUSED and the conditional CD=1
	// (checking-disabled) re-query ran (02-observation-model.md §2.7b —
	// broken-DNSSEC rescue). cd_present ⇒ IPs carry the unvalidated
	// authoritative AAAA and Rcode is set NOERROR. Empty otherwise.
}

// QuorumInfo records the per-resolver breakdown of a consensus lookup.
// This type is single-sourced with 02-observation-model.md §2.1; keep them identical.
// `Rcodes` is required by 03's dead-signal computation.
type QuorumInfo struct {
	PerResolver map[string]string `json:"per_resolver"` // "cloudflare"|"google"|"quad9" → per-resolver symbol:
	// "exists"|"empty"|"nxdomain"|"timeout"|"error"
	// (timeout/error both reduce to the quorum symbol `error`;
	// kept split here for diagnostics)
	Rcodes map[string]string `json:"rcodes"` // same keys → raw rcode string ("NOERROR", "SERVFAIL",
	// "REFUSED", ...); "" when no DNS response was received
	// (transport error / timeout)
	Agreement string `json:"agreement"` // "3of3", "2of3", "2of2"
	Disagreed bool   `json:"disagreed"` // true when an answering resolver's reduced symbol
	// differed from the quorum symbol
}

// ErrQuorumInconsistent is returned when no quorum is reached.
var ErrQuorumInconsistent = errors.New("resolver quorum inconsistent")

// AAAAResolver is the seam consumed by dns_aaaa_base and dns_aaaa_www.
type AAAAResolver interface {
	LookupAAAA(ctx context.Context, name string) (AAAAAnswer, error)
}
