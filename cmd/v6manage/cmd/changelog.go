package cmd

import (
	"context"
	"fmt"
	"log"
	"whynoipv6/internal/core"

	"github.com/spf13/cobra"
)

// crawlCmd represents the crawl command
var changelogCmd = &cobra.Command{
	Use:   "changelog",
	Short: "Displays the last changelogs",
	Long:  "Displays the last changelogs",
	Run: func(cmd *cobra.Command, args []string) {
		listChangelogs()
	},
}

func init() {
	rootCmd.AddCommand(changelogCmd)
}

func listChangelogs() {
	ctx := context.Background()
	// Get the last 50 changelogs

	// Spawn changelog service
	changelogService := core.NewChangelogService(db)

	// List all changelog entries
	changelogs, err := changelogService.List(ctx, 50, 0)
	if err != nil {
		log.Fatal(err.Error())
	}
	for _, c := range changelogs {
		fmt.Printf("[%s] %s - %s\n", c.Ts.Format("2006-01-02 15:04:05"), c.Site, c.Message)
	}

}
