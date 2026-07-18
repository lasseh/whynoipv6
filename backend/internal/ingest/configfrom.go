package ingest

// ConfigSource is the consumer-side view of the config registry — satisfied
// by *config.Config without importing it.
type ConfigSource interface {
	Int(key string) int
	Float(key string) float64
}

// TrancoConfigFrom binds the tranco.* import-guard keys (09-ops §2.5).
func TrancoConfigFrom(src ConfigSource) TrancoConfig {
	return TrancoConfig{
		MinRows:      src.Int("tranco.min_rows"),
		MaxDelistPct: src.Float("tranco.max_delist_pct"),
	}
}
