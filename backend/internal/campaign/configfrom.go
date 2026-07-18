package campaign

// ConfigSource is the consumer-side view of the config registry — satisfied
// by *config.Config without importing it.
type ConfigSource interface {
	String(key string) string
	Int(key string) int
}

// ConfigFrom binds the campaign.* registry keys (09-ops §2.6). Pull/Push
// and AdoptUnknownUUIDs stay with the caller — they are invocation policy,
// not registry state.
func ConfigFrom(src ConfigSource) Config {
	return Config{
		RepoPath:          src.String("campaign.repo_path"),
		GitRemote:         src.String("campaign.git_remote"),
		MaxDomainsPerFile: src.Int("campaign.max_domains_per_file"),
	}
}
