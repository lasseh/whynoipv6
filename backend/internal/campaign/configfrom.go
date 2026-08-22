package campaign

// ConfigSource is the consumer-side view of the config registry — satisfied
// by *config.Config without importing it.
type ConfigSource interface {
	String(key string) string
	Int(key string) int
	Bool(key string) bool
}

// ConfigFrom binds the campaign.* registry keys (09-ops §2.6), Pull/Push
// included — containerized deployments run the git-less distroless image
// against a mounted checkout and set both false.
func ConfigFrom(src ConfigSource) Config {
	return Config{
		RepoPath:               src.String("campaign.repo_path"),
		GitRemote:              src.String("campaign.git_remote"),
		MaxDomainsPerFile:      src.Int("campaign.max_domains_per_file"),
		MaxSubdomainsPerDomain: src.Int("campaign.max_subdomains_per_domain"),
		Pull:                   src.Bool("campaign.pull"),
		Push:                   src.Bool("campaign.push"),
	}
}
