package domain

// Classify implements the deterministic first-match classification ladder,
// the five sub-reason flags, and the saint rule (03-state-machine.md §10),
// evaluated over CONFIRMED values only. nil = never confirmed (NULL).
// Flags are returned in the fixed order broken_v6, www_missing, ns_missing,
// mail_missing, resources_v4only.
func Classify(confirmed map[Dimension]*IPv6Status) (class Classification, flags []string, saint bool) {
	get := func(d Dimension) (IPv6Status, bool) {
		if v := confirmed[d]; v != nil {
			return *v, true
		}
		return "", false
	}

	base, baseOK := get(DimBase)
	www, wwwOK := get(DimWWW)
	ns, _ := get(DimNS)
	conn, _ := get(DimConn)
	mx, mxOK := get(DimMX)
	res, resOK := get(DimResources)

	// The ladder — first match wins; enumerated value sets are exhaustive.
	switch {
	case !baseOK:
		class = ClassUnknown
	case base == StatusNoRecord:
		class = ClassInactive
	case base == StatusUnsupported:
		class = ClassSinner
	case base == StatusSupported &&
		(wwwOK && (www == StatusSupported || www == StatusNotApplicable || www == StatusNoRecord)) &&
		ns == StatusSupported &&
		conn == StatusSupported &&
		(mxOK && (mx == StatusSupported || mx == StatusNotApplicable)):
		class = ClassHero
	default:
		// base = supported (or the unreachable not_applicable), hero bar
		// not met — partial, possibly with zero flags (03 §10 note a).
		class = ClassPartial
	}

	// Flags: only a confirmed `unsupported` on the named dimension sets one.
	if conn == StatusUnsupported {
		flags = append(flags, "broken_v6")
	}
	if www == StatusUnsupported {
		flags = append(flags, "www_missing")
	}
	if ns == StatusUnsupported {
		flags = append(flags, "ns_missing")
	}
	if mx == StatusUnsupported {
		flags = append(flags, "mail_missing")
	}
	if res == StatusUnsupported {
		flags = append(flags, "resources_v4only")
	}

	saint = class == ClassHero && resOK && (res == StatusSupported || res == StatusNotApplicable)
	return class, flags, saint
}

// V6Ready reports the campaign readiness predicate (07 §3.2): confirmed
// base and ns supported, www supported or not_applicable. Strict on NULLs
// — an unconfirmed dimension is never ready. The stats_campaign_daily
// v6_ready counter (db/query/stats.sql) aggregates this same predicate in
// SQL; the two must not drift.
func V6Ready(base, ns, www *IPv6Status) bool {
	return base != nil && *base == StatusSupported &&
		ns != nil && *ns == StatusSupported &&
		www != nil && (*www == StatusSupported || *www == StatusNotApplicable)
}

// IPv6Only folds conn and resources into the derived "IPv6 only" status:
// whether the site presents the same over an IPv6-only connection (03 §10).
// It is ungated by classification — a non-hero domain can still be fully
// usable IPv6-only. nil = not claimable yet; strict by design: a reachable
// site (conn = supported) never yields supported while resources is
// unconfirmed NULL.
func IPv6Only(conn, resources *IPv6Status) *IPv6Status {
	if conn == nil {
		return nil
	}
	switch *conn {
	case StatusUnsupported:
		// broken_v6: publishes AAAA but doesn't answer — definitively not
		// usable IPv6-only, whatever the resources say.
		return new(StatusUnsupported)
	case StatusNotApplicable:
		// No AAAA on base or www — nothing to assess; the base/www
		// statuses tell that story.
		return new(StatusNotApplicable)
	case StatusSupported:
		if resources == nil {
			return nil
		}
		switch *resources {
		case StatusSupported, StatusNotApplicable:
			// not_applicable = confirmed empty required-host set — the
			// same vacuous pass the saint rule accepts.
			return new(StatusSupported)
		case StatusUnsupported:
			return new(StatusUnsupported)
		case StatusNoRecord:
			// no_record never occurs on resources (02 §2 rule 3) — claim
			// nothing on impossible input.
			return nil
		}
	case StatusNoRecord:
		// no_record never occurs on conn (02 §2 rule 3) — claim nothing.
		return nil
	}
	return nil
}
