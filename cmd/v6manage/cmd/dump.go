package cmd

import (
	"context"
	"fmt"
	"log"
	"time"

	"whynoipv6/internal/core"

	"github.com/spf13/cobra"
)

// crawlCmd represents the crawl command
var dumpCmd = &cobra.Command{
	Use:   "dump",
	Short: "Dumps all domains per country to a file",
	Long:  "Dump all domains per country to a file",
	Run: func(cmd *cobra.Command, args []string) {
		dumpData()
	},
}

func init() {
	rootCmd.AddCommand(dumpCmd)
}

func dumpData() {
	ctx := context.Background()

	// Spawn core service
	// domainService := core.NewDomainService(db)
	countryService := core.NewCountryService(db)

	// start time
	t := time.Now()
	log.Println("Dumping country data...")

	// Get all countries
	country, err := countryService.List(ctx)
	if err != nil {
		log.Fatal(err.Error())
	}

	// Loop through countries
	for _, c := range country {
		fmt.Println("Country: ", c.Country)

		// Get all domains for country
		// domains, err := countryService.GetCountryCode()(ctx, c.Country)
		// if err != nil {
		// 	log.Fatal(err.Error())
		// }

		// // Loop through domains
		// for _, d := range domains {
		// 	fmt.Println(d.Domain)
		// }
	}
	// Print time it took to import
	fmt.Printf("Dumped country data in: %s", time.Since(t))
}
