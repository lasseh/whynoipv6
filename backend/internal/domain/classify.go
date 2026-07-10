package domain

// Classify implements the deterministic first-match classification ladder,
// the five sub-reason flags, and the gold rule (03-state-machine.md §10),
// evaluated over CONFIRMED values only. nil = never confirmed (NULL).
// Flags are returned in the fixed order broken_v6, www_missing, ns_missing,
// mail_missing, resources_v4only.
func Classify(confirmed map[Dimension]*IPv6Status) (class Classification, flags []string, gold bool) {
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

	gold = class == ClassHero && resOK && (res == StatusSupported || res == StatusNotApplicable)
	return class, flags, gold
}
