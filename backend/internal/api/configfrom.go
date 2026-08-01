package api

import "time"

// ConfigSource is the consumer-side view of the config registry — satisfied
// by *config.Config without importing it.
type ConfigSource interface {
	Int(key string) int
	Bool(key string) bool
	Duration(key string) time.Duration
	StringSlice(key string) []string
}

// OptionsFrom binds the API's registry keys (09-ops §2.7, §2.10). The
// global deployment fields (PublicBaseURL, DatasetsDir) are typed config
// fields the caller sets afterwards.
func OptionsFrom(src ConfigSource) Options {
	return Options{
		CSVMaxRows:        src.Int("export.csv_max_rows"),
		RateIPPerHour:     src.Int("live_check.rate_ip_per_hour"),
		RateGlobalPerHour: src.Int("live_check.rate_global_per_hour"),
		DedupeWindow:      src.Duration("live_check.dedupe_window"),
		LinkTTL:           src.Duration("live_check.link_ttl"),
		ResourcesEnabled:  src.Bool("crawler.resources.enabled"),
		BadgeCacheTTL:     src.Duration("badge.cache_ttl"),
		ManifestCacheTTL:  src.Duration("datasets.manifest_cache_ttl"),
		FeedRecentWindow:  src.Int("feed.recent_window"),
		TrustedProxies:    src.StringSlice("api.trusted_proxies"),
	}
}
