package config

// registryDefaults returns the compiled-in default for every config key in
// the consolidated registry (09-ops.md §2). DATABASE_URL is the sole
// no-default key: it is required and validated in Load, never defaulted.
// Env-var name = dotted key path, upper-cased, "." → "_".
//
//nolint:goconst // a literal data table; repeated duration strings are values, not constants
func registryDefaults(binary string) map[string]any {
	logLevel := "info"
	if binary == "v6ctl" {
		logLevel = "warn"
	}
	return map[string]any{
		// §2.1 Global deployment keys (UPPERCASE env).
		"API_LISTEN":      "[::1]:8080",
		"GEOIP_PATH":      "/var/lib/GeoIP",
		"IPINFO_TOKEN":    "", // secret; only `v6ctl geoip update` reads it
		"DATASETS_DIR":    "/var/lib/whynoipv6/datasets",
		"PUBLIC_BASE_URL": "https://api.whynoipv6.com",
		"LOG_LEVEL":       logLevel,

		// §2.2 Crawler engine & scheduling.
		"claim.batch_size":                  200,
		"claim.empty_poll_interval":         "10s",
		"claim.order":                       "rank",
		"worker_slots":                      64,
		"cadence.default":                   "24h",
		"recheck_inconsistent":              "2h",
		"recheck_error":                     "6h",
		"recheck_backoff_max":               "720h",
		"anti_flap.min_confirm_spacing":     "12h",
		"preflight.probe_host":              "one.one.one.one:443",
		"preflight.retry_interval":          "60s",
		"service_detect.indegree_threshold": 100,
		"resolver.bulk_upstreams":           []string{"127.0.0.1:53", "127.0.0.1:5353"},
		"checks.max_ns_lookups":             4,
		"checks.max_mx_lookups":             5,
		"crawler.resources.enabled":         true,

		// §2.3 Consensus resolver.
		"consensus.per_provider_qps":                    15,
		"consensus.fastlane_breaker.nondefinitive_rate": 0.05,
		"consensus.fastlane_breaker.window":             "15m",
		"consensus.fastlane_breaker.min_samples":        500,
		"consensus.fastlane_breaker.recover_below":      0.02,
		"consensus.provider_breaker.failure_rate":       0.50,
		"consensus.provider_breaker.window":             "15m",
		"consensus.provider_breaker.min_samples":        200,
		"consensus.provider_breaker.recovery_probes":    3,

		// §2.4 Lifecycle.
		"lifecycle.dead_streak":        7,
		"lifecycle.slow_lane_every":    "720h",
		"lifecycle.delist_grace":       "720h",
		"lifecycle.live_check_linkage": "168h",

		// §2.5 Tranco import.
		"tranco.min_rows":         950000,
		"tranco.max_delist_pct":   2.0,
		"tranco.import_at":        "23:15",
		"tranco.retry_interval":   "2h",
		"tranco.stale_warn_after": "48h",

		// §2.6 Campaign sync.
		"campaign.repo_path":            "/srv/whynoipv6-campaign",
		"campaign.git_remote":           "origin",
		"campaign.pull":                 true,
		"campaign.push":                 true,
		"campaign.max_domains_per_file": 5000,

		// Curated subdomain lists: subdomains/<apex>.yml in the same repo.
		"campaign.max_subdomains_per_domain": 20,

		// §2.7 Live check.
		"live_check.workers":              4,
		"live_check.job_budget":           "60s",
		"live_check.reclaim_after":        "5m",
		"live_check.fail_after":           "15m",
		"live_check.retention":            "720h",
		"live_check.rate_ip_per_hour":     10,
		"live_check.rate_global_per_hour": 500,
		"live_check.dedupe_window":        "1h",
		"live_check.link_ttl":             "168h",

		// §2.8 Ops / observability.
		"ops.webhook_url":              "",
		"ops.healthcheck_url":          "",
		"ops.healthcheck_tick_url":     "",
		"ops.healthcheck_min_interval": "60s",
		"taillight.url":                "",
		"taillight.api_key":            "",
		"taillight.log_level":          "",

		// §2.9 Unbound stats.
		"unbound_stats.control": "unbound-control",

		// §2.10 API serving — badge, datasets, feeds, CSV export.
		"badge.cache_ttl":             "24h",
		"datasets.manifest_cache_ttl": "5m",
		"datasets.retention_days":     90,
		"feed.recent_window":          50,
		"export.csv_max_rows":         10000,
		"api.trusted_proxies":         []string{"127.0.0.0/8", "::1/128"},

		// §2.11 DNS-provider mapping.
		"dns_provider.seed_path":        "",
		"dns_provider.refresh_interval": "24h",
	}
}
