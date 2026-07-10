package domain

// Pure enum types mirroring the DB enums (03-state-machine.md §16;
// 05-schema.md §3). This package has zero non-stdlib deps.

// Dimension is one measured facet of a domain's IPv6 posture (core six).
type Dimension string

const (
	DimBase      Dimension = "base"
	DimWWW       Dimension = "www"
	DimNS        Dimension = "ns"
	DimMX        Dimension = "mx"
	DimConn      Dimension = "conn"
	DimResources Dimension = "resources"
)

// Dimensions is the fixed core-dimension order (base, www, ns, mx, conn,
// resources) used wherever a deterministic iteration is needed.
var Dimensions = []Dimension{DimBase, DimWWW, DimNS, DimMX, DimConn, DimResources}

// IPv6Status is the public 4-valued confirmed status model.
type IPv6Status string

const (
	StatusSupported     IPv6Status = "supported"
	StatusUnsupported   IPv6Status = "unsupported"
	StatusNoRecord      IPv6Status = "no_record"
	StatusNotApplicable IPv6Status = "not_applicable"
)

// Observation is the internal 7-valued per-scan outcome; partial/error/
// inconsistent never reach public output.
type Observation string

const (
	ObsSupported     Observation = "supported"
	ObsPartial       Observation = "partial"
	ObsUnsupported   Observation = "unsupported"
	ObsNoRecord      Observation = "no_record"
	ObsNotApplicable Observation = "not_applicable"
	ObsError         Observation = "error"
	ObsInconsistent  Observation = "inconsistent"
)

// Definitive reports whether an observation can advance confirmed state
// (NOT error/inconsistent — 00-overview.md glossary).
func (o Observation) Definitive() bool {
	return o != ObsError && o != ObsInconsistent
}

// Classification is the materialized public verdict.
type Classification string

const (
	ClassUnknown  Classification = "unknown"
	ClassInactive Classification = "inactive"
	ClassSinner   Classification = "sinner"
	ClassPartial  Classification = "partial"
	ClassHero     Classification = "hero"
)

// Kind is the entity kind (apex eTLD+1 vs campaign subdomain).
type Kind string

const (
	KindApex      Kind = "apex"
	KindSubdomain Kind = "subdomain"
)

// DisabledReason is the lifecycle disable cause.
type DisabledReason string

const (
	DisabledDead     DisabledReason = "dead"
	DisabledService  DisabledReason = "service"
	DisabledManual   DisabledReason = "manual"
	DisabledDelisted DisabledReason = "delisted"
)

// ConfirmN returns the anti-flap threshold for a dimension
// (03-state-machine.md §2): base/www/ns/mx → 2; conn/resources → 3.
func ConfirmN(d Dimension) int16 {
	if d == DimConn || d == DimResources {
		return 3
	}
	return 2
}
