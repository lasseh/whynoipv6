package campaign

import (
	"fmt"
	"os"
	"os/exec"
)

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

// Validate is the startup gate for the bindings above. Both Pull and Push
// default to true, and the runtime image is distroless with no git, so a
// deployment that forgets CAMPAIGN_PULL=false looks healthy all day and
// reports `git pull: exec: "git": executable file not found` once a night
// at the 03:30 tick. Checked at startup instead.
func (c Config) Validate() error {
	if fi, err := os.Stat(c.RepoPath); err != nil {
		return fmt.Errorf("config: campaign.repo_path %q is not readable: %w", c.RepoPath, err)
	} else if !fi.IsDir() {
		return fmt.Errorf("config: campaign.repo_path %q is not a directory", c.RepoPath)
	}
	if c.Pull || c.Push {
		if _, err := exec.LookPath("git"); err != nil {
			return fmt.Errorf("config: campaign.pull/push need git on PATH: %w", err)
		}
	}
	return nil
}
