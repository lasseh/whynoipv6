package cmd

import (
	"context"
	"log"
	"time"
	"whynoipv6/internal/core"

	"github.com/spf13/cobra"
)

// crawlCmd represents the crawl command
var importCmd = &cobra.Command{
	Use:   "import",
	Short: "Imports data from sites to scan table",
	Long:  "Imports data from sites to scan table",
	Run: func(cmd *cobra.Command, args []string) {
		importData()
	},
}

func init() {
	rootCmd.AddCommand(importCmd)
}

func importData() {
	ctx := context.Background()

	// Spawn core service
	siteService := core.NewSiteService(db)
	domainService := core.NewDomainService(db)

	// start time
	t := time.Now()
	log.Println("Importing data...")

	var offset int32 = 0
	var limit int32 = 10000
	// Main loop
	for {
		sites, err := siteService.ListSite(ctx, offset, limit)
		if err != nil {
			log.Fatal(err.Error())
		}

		// Stop if no more data
		if len(sites) == 0 {
			log.Println("No sites left to import")
			break
		}

		// Loop through sites
		for _, s := range sites {
			err := domainService.InsertDomain(ctx, s.Site)
			if err != nil {
				log.Println(err)
			}
		}
		// Increment offset
		offset += limit
	}

	// Print time it took to import
	log.Println("Imported sites in", time.Since(t))

}
