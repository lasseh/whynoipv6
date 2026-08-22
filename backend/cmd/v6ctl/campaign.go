package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lasseh/whynoipv6/internal/campaign"
)

func campaignCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "campaign",
		Short: "Campaign repository sync",
	}

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
			rep, err := campaign.Sync(cmd.Context(), campaign.ConfigFrom(cfg), pool)
			if err != nil {
				return err
			}
			fmt.Printf("created: %d updated: %d renamed: %d re-enabled: %d disabled: %d\n",
				len(rep.Created), len(rep.Updated), len(rep.Renamed), len(rep.ReEnabled), len(rep.Disabled))
			fmt.Printf("membership adds: %d removes: %d\n", rep.MembershipAdds, rep.MembershipRemoves)
			fmt.Printf("curated subdomains: %d lists, +%d/-%d memberships\n",
				rep.CuratedFiles, rep.CuratedAdds, rep.CuratedRemoves)
			if rep.CuratedFrozen {
				fmt.Println("WARNING: curated removals were suspended — a rejected list would " +
					"otherwise have unlisted its hosts. Fix the rejections above and re-run.")
			}
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
	cmd.AddCommand(syncCmd)
	cmd.AddCommand(campaignValidateCmd())
	return cmd
}
