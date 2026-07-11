// Package checker is the scan engine lifted from the reference audit engine
// per 01-engine.md: 15 checks, two-phase conditional execution, SSRF-pinned
// dialing, and the consensus-resolver seam. No scoring, no grades.
package checker

import (
	"context"
	"time"
)

// Kind is the entity kind the engine scans (values match the domain.kind
// enum; the crawler maps the DB value onto this when calling Runner.Run).
type Kind string

const (
	KindApex      Kind = "apex"
	KindSubdomain Kind = "subdomain"
)

// CheckStatus is a bounded set of possible check outcomes.
type CheckStatus string

const (
	StatusSupported     CheckStatus = "supported"
	StatusUnsupported   CheckStatus = "unsupported"
	StatusPartial       CheckStatus = "partial"
	StatusError         CheckStatus = "error"
	StatusNotApplicable CheckStatus = "not_applicable"
)

// Result is the outcome of a single check against a host.
type Result struct {
	// Status is the check outcome. One of the CheckStatus constants.
	Status CheckStatus `json:"status"`

	// Detail is the check's typed payload (detail.go); it serializes under
	// the scan_detail "details" key exactly as the former untyped map did.
	Detail Detail `json:"details,omitempty"`

	// Latency is wall-clock time the check took to execute.
	Latency time.Duration `json:"latency"`
}

// Checker is implemented by every individual check.
// Each implementation is stateless and safe for concurrent use.
type Checker interface {
	// Name returns a unique, stable identifier for this check.
	Name() string

	// Check performs the check against the given host.
	Check(ctx context.Context, host string, kind Kind) (Result, error)
}

// ScanResult contains the results of all checks for a single host.
type ScanResult struct {
	Domain    string            `json:"domain"`
	Results   map[string]Result `json:"results"`
	ScannedAt time.Time         `json:"scanned_at"`
	Duration  time.Duration     `json:"duration"`
}
