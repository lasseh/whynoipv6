package cmd

import (
	"fmt"
	"log"
	"os"
	"whynoipv6/internal/core"
	"whynoipv6/internal/toolbox"

	"github.com/spf13/cobra"
)

// statsCmd represents the stats command
var debugCmd = &cobra.Command{
	Use:   "debug",
	Short: "Debug a site",
	Long:  "Debug",
	Run: func(cmd *cobra.Command, args []string) {
		asnService = *core.NewASNService(db)
		countryService = *core.NewCountryService(db)
		toolboxService = *toolbox.NewToolboxService(cfg.GeoIPPath, cfg.Nameserver)
		metricService = *core.NewMetricService(db)
		debugDomain()
	},
}

func init() {
	rootCmd.AddCommand(debugCmd)
}

func debugDomain() {
	// Create array of domain names
	domains := []string{
		"example.co.uk",
		"serialssolutions.com",
		"uio.no",
		"thisshouldfail.com",
		"vg.no",
		"wikipedia.org",
		"db.no",
		"salesforce.com",
		"nsfc.gov.cn",
		"xn--mgbkt9eckr.net",
		"xn--gmq92kd2rm1kx34a.com",
		"bodøposten.no",
		"finn.no",
	}

	// Dns Client
	resolver, err = toolboxService.NewResolver()
	if err != nil {
		log.Printf("Could not initalize dns resolver: %s\n", err)
		os.Exit(1)
	}

	// Loop through domains
	for _, domain := range domains {
		log.Println("Checking domain:", domain)

		// Validate domain
		if err := toolboxService.ValidateDomain(domain); err != nil {
			log.Printf("Invalid domain: %s - %s\n", domain, err)
			log.Println("Disable domain!")
			log.Println("")
			continue
		}

		// Check if domain has AAAA record
		checkAAAA, err := toolboxService.CheckTLD(domain)
		if err != nil {
			if verbose {
				log.Printf("[%s] CheckTLD AAAA error: %s\n", domain, err)
			}
		}
		log.Println("CheckTLD AAAA:", checkAAAA)

		// Check if wwww.domain has AAAA record
		checkWWW, err := toolboxService.CheckTLD(fmt.Sprintf("www.%s", domain))
		if err != nil {
			if verbose {
				log.Printf("[%s] CheckTLD WWW error: %s\n", domain, err)
			}
		}
		log.Println("CheckTLD WWW:", checkWWW)

		// Check if domain has AAAA record for nameservers
		checkNS, err := toolboxService.CheckNS(domain)
		if err != nil {
			if verbose {
				log.Printf("[%s] CheckNS error: %s", domain, err)
			}
		}
		log.Println("CheckNS:", checkNS)

		// Map AsnID to ASN Table
		asnid, err := getASNInfo(domain)
		if err != nil {
			log.Printf("[%s] getASNInfo error: %s\n", domain, err)
		}
		log.Println("ASNID:", asnid)

		// Map CountryID to Country Table
		countryid, err := getCountryInfo(domain)
		if err != nil {
			log.Printf("[%s] getCountryID error: %s\n", domain, err)
		}
		log.Println("CountryID:", countryid)

		// Done
		log.Println("")
	}

}
