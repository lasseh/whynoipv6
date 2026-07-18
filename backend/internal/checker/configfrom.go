package checker

// ConfigSource is the consumer-side view of the config registry — satisfied
// by *config.Config without importing it.
type ConfigSource interface {
	Int(key string) int
	Bool(key string) bool
}

// ConfigFrom binds the engine's registry keys (09-ops §2.2) to Config —
// the one place these key names meet their fields.
func ConfigFrom(src ConfigSource) Config {
	return Config{
		MaxNSLookups:            src.Int("checks.max_ns_lookups"),
		MaxMXLookups:            src.Int("checks.max_mx_lookups"),
		EnableResourceDiscovery: src.Bool("crawler.resources.enabled"),
	}
}
