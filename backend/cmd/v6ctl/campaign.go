package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lasseh/whynoipv6/internal/campaign"
	"github.com/lasseh/whynoipv6/internal/config"
)

func campaignCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "campaign",
		Short: "Campaign repository sync",
	}

	var adopt bool
	syncCmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync the campaign YAML checkout into the database",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := cfgFromCmd(cmd)
			pool, err := newPool(cmd)
			if err != nil {
				return err
			}
			defer pool.Close()
			rep, err := campaign.Sync(cmd.Context(), campaignConfig(cfg, adopt), pool)
			if err != nil {
				return err
			}
			fmt.Printf("created: %d updated: %d renamed: %d re-enabled: %d disabled: %d\n",
				len(rep.Created), len(rep.Updated), len(rep.Renamed), len(rep.ReEnabled), len(rep.Disabled))
			fmt.Printf("membership adds: %d removes: %d\n", rep.MembershipAdds, rep.MembershipRemoves)
			for f, reason := range rep.RejectedFiles {
				fmt.Printf("rejected file %s: %s\n", f, reason)
			}
			if n := len(rep.RejectedHosts); n > 0 {
				fmt.Printf("rejected host entries: %d\n", n)
				for h, reason := range rep.RejectedHosts {
					fmt.Printf("  %s: %s\n", h, strings.Split(reason, "\n")[0])
				}
			}
			fmt.Printf("write-back: %s\n", rep.WriteBack)
			return nil
		},
	}
	syncCmd.Flags().BoolVar(&adopt, "adopt-unknown-uuids", false,
		"insert campaigns whose files carry a uuid unknown to the DB (one-time bootstrap; never cron)")
	cmd.AddCommand(syncCmd)
	cmd.AddCommand(campaignValidateCmd())
	return cmd
}

// campaignConfig binds the registry keys and applies v6ctl's invocation
// policy: interactive syncs always pull and push.
func campaignConfig(cfg *config.Config, adopt bool) campaign.Config {
	c := campaign.ConfigFrom(cfg)
	c.AdoptUnknownUUIDs = adopt
	c.Pull, c.Push = true, true
	return c
}
