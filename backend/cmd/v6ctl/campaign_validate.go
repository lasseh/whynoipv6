package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/lasseh/whynoipv6/internal/campaign"
)

// campaignValidateCmd is the PR-validation entry point (06 §4.3): all §4.2
// checks, no DB, no network — the GitHub Action builds v6ctl and runs this.
func campaignValidateCmd() *cobra.Command {
	var repo, base, commentFile string
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate campaign YAML files (CI with --base, local otherwise)",
		Args:  cobra.NoArgs,
		// Overrides the root PersistentPreRunE: CI has no DATABASE_URL and
		// this verb never opens the DB (06 §4.3).
		PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
		RunE: func(cmd *cobra.Command, _ []string) error {
			maxDomains := envInt("CAMPAIGN_MAX_DOMAINS_PER_FILE", 5000)       // campaign.max_domains_per_file
			maxSubdomains := envInt("CAMPAIGN_MAX_SUBDOMAINS_PER_DOMAIN", 20) // campaign.max_subdomains_per_domain
			res, err := campaign.Validate(cmd.Context(), repo, base, maxDomains, maxSubdomains)
			if err != nil {
				return err
			}
			if commentFile == "" {
				fmt.Println(res.Comment)
			} else if err := os.WriteFile(commentFile, []byte(res.Comment), 0o644); err != nil {
				return err
			}
			for _, f := range res.Failures {
				fmt.Fprintln(os.Stderr, f)
			}
			if !res.OK() {
				cmd.SilenceUsage = true
				cmd.SilenceErrors = true
				return errors.New("validation failed")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&repo, "repo", ".", "campaign checkout to validate")
	cmd.Flags().StringVar(&base, "base", "", "git ref of the merge base (CI mode); omit for local mode")
	cmd.Flags().StringVar(&commentFile, "comment-file", "", "write the bot-comment Markdown here (default stdout)")
	return cmd
}

// envInt reads a positive integer override for a registry default; CI has no
// config file, so the caps arrive as environment variables.
func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}
