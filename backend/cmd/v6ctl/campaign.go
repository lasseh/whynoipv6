package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lasseh/whynoipv6/internal/campaign"
	"github.com/lasseh/whynoipv6/internal/lock"
)

func campaignCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "campaign",
		Short: "Campaign repository sync",
	}

	var force bool
	syncCmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync the campaign YAML checkout into the database",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := cfgFromCmd(cmd)
			ccfg := campaign.ConfigFrom(cfg)
			ccfg.Force = force
			if err := ccfg.Validate(); err != nil {
				return err
			}
			pool, err := newPool(cmd)
			if err != nil {
				return err
			}
			defer pool.Close()
			// Serialized against tick step 5 and any concurrent operator run
			// by the JobCampaignSync lock; the wait is normative (04 §10).
			var rep *campaign.Report
			err = lock.Run(cmd.Context(), pool, lock.JobCampaignSync, singletonWait, func(ctx context.Context) error {
				r, err := campaign.Sync(ctx, ccfg, pool)
				rep = r
				return err
			})
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
			if n := len(rep.DisableFrozen); n > 0 {
				fmt.Printf("WARNING: %d campaign(s) were held back from disable/removal because "+
					"their file was rejected, not deleted: %s. Fix the rejections below and re-run.\n",
					n, strings.Join(rep.DisableFrozen, ", "))
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
			return syncOutcome(rep)
		},
	}
	syncCmd.Flags().BoolVar(&force, "force", false,
		"sync an empty checkout, disabling every campaign (the guard exists because "+
			"an empty checkout is usually a broken clone)")
	cmd.AddCommand(syncCmd)
	cmd.AddCommand(campaignValidateCmd())
	return cmd
}

// syncOutcome turns "completed with rejections" into a non-zero exit
// (review issue 42, 06 §3.3 step 7 erratum). The database work has already
// committed by this point, so the exit code changes nothing but
// visibility — and visibility is the whole problem: the verb runs from a
// webhook-triggered CI job and under systemd OnFailure, and a rejected
// campaign file that never imports, or a CuratedFrozen run that suspends
// every curated removal, looked exactly like a clean sync to both.
//
// `tranco import`, the sibling verb, already exits non-zero on its aborted
// outcome.
func syncOutcome(rep *campaign.Report) error {
	var reasons []string
	if n := len(rep.RejectedFiles); n > 0 {
		reasons = append(reasons, fmt.Sprintf("%d rejected file(s)", n))
	}
	if n := len(rep.RejectedHosts); n > 0 {
		reasons = append(reasons, fmt.Sprintf("%d rejected host(s)", n))
	}
	if rep.CuratedFrozen {
		reasons = append(reasons, "curated removals frozen")
	}
	if n := len(rep.DisableFrozen); n > 0 {
		reasons = append(reasons, fmt.Sprintf("%d campaign(s) frozen", n))
	}
	if strings.HasPrefix(rep.WriteBack, "failed:") {
		reasons = append(reasons, "uuid write-back "+rep.WriteBack)
	}
	if len(reasons) == 0 {
		return nil
	}
	return fmt.Errorf("sync completed with %s", strings.Join(reasons, ", "))
}
