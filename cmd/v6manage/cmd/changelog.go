package cmd

import (
	"context"
	"fmt"
	"log"

	"whynoipv6/internal/core"

	"github.com/spf13/cobra"
)

// changelogCmd represents the command for displaying the last changelogs
var changelogCmd = &cobra.Command{
	Use:   "changelog",
	Short: "Displays the last changelogs",
	Long:  "Displays the last changelogs",
	Run: func(cmd *cobra.Command, args []string) {
		displayChangelogs()
	},
}

func init() {
	rootCmd.AddCommand(changelogCmd)
}

// displayChangelogs retrieves and prints the last 50 changelog entries
func displayChangelogs() {
	ctx := context.Background()

	// Instantiate the changelog service
	changelogService := core.NewChangelogService(db)

	// Retrieve the last 50 changelog entries
	changelogEntries, err := changelogService.List(ctx, 50, 0)
	if err != nil {
		log.Fatal(err.Error())
	}

	// Print the retrieved changelog entries
	for _, entry := range changelogEntries {
		fmt.Printf(
			"[%s] %s - %s\n",
			entry.Ts.Format("2006-01-02 15:04:05"),
			entry.Site,
			entry.Message,
		)
	}
}
