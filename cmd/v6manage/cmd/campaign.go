package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

// campaignCmd represents the campaign command
var campaignCmd = &cobra.Command{
	Use:   "campaign",
	Short: "Manage campaigns",
	Long:  "Manage campaigns",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			if err := cmd.Help(); err != nil {
				os.Exit(1)
			}
			os.Exit(0)
		}
	},
}

func init() {
	rootCmd.AddCommand(campaignCmd)
}
