package cmd

import (
	"fmt"
	"log"
	"os"
	"time"
	"whynoipv6/internal/core"
	"whynoipv6/internal/toolbox"

	"github.com/spf13/cobra"
)

var slow bool

// debugCmd represents the debug command.
var debugCmd = &cobra.Command{
	Use:   "debug",
	Short: "Debug a site",
	Long:  "Debug a site by checking its IPv6 support, nameservers, ASN and country information.",
	Run: func(cmd *cobra.Command, args []string) {
		asnService = *core.NewASNService(db)
		countryService = *core.NewCountryService(db)
		toolboxService = *toolbox.NewToolboxService(cfg.GeoIPPath, cfg.Nameserver)
		metricService = *core.NewMetricService(db)
		debugDomain()
	},
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&slow, "slow", "s", false, "adds a 5 second delay between each domain check")
	rootCmd.AddCommand(debugCmd)
}

// debugDomain checks the IPv6 support, nameservers, ASN and country information for a list of domains.
func debugDomain() {
	// List of domain names to be checked.
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

	// Initialize the DNS client.
	_, err := toolboxService.NewResolver()
	if err != nil {
		log.Printf("Could not initialize DNS resolver: %s\n", err)
		os.Exit(1)
	}

	// Loop through the domains and check their properties.
	for _, domain := range domains {
		log.Println("Checking domain:", domain)

		// Validate the domain name.
		if err := toolboxService.ValidateDomain(domain); err != nil {
			log.Printf("Invalid domain: %s - %s\n", domain, err)
			log.Println("Disable domain!")
			log.Println("")
			continue
		}

		if slow {
			time.Sleep(500 * time.Millisecond)
		}

		// Check if domain has an AAAA record.
		hasAAAA, err := toolboxService.CheckTLD(domain)
		if err != nil && verbose {
			log.Printf("[%s] CheckTLD AAAA error: %s\n", domain, err)
		}
		log.Println("CheckTLD AAAA:", hasAAAA)
		if slow {
			time.Sleep(500 * time.Millisecond)
		}

		// Check if www.domain has an AAAA record.
		hasWWW, err := toolboxService.CheckTLD(fmt.Sprintf("www.%s", domain))
		if err != nil && verbose {
			log.Printf("[%s] CheckTLD WWW error: %s\n", domain, err)
		}
		log.Println("CheckTLD WWW:", hasWWW)
		if slow {
			time.Sleep(500 * time.Millisecond)
		}

		// Check if domain has AAAA records for nameservers.
		hasNS, err := toolboxService.CheckNS(domain)
		if err != nil && verbose {
			log.Printf("[%s] CheckNS error: %s", domain, err)
		}
		log.Println("CheckNS:", hasNS)
		if slow {
			time.Sleep(500 * time.Millisecond)
		}

		// Retrieve ASN information for the domain.
		asnID, err := getASNInfo(domain)
		if err != nil {
			log.Printf("[%s] getASNInfo error: %s\n", domain, err)
		}
		log.Println("ASNID:", asnID)
		if slow {
			time.Sleep(500 * time.Millisecond)
		}

		// Retrieve country information for the domain.
		countryID, err := getCountryInfo(domain)
		if err != nil {
			log.Printf("[%s] getCountryID error: %s\n", domain, err)
		}
		log.Println("CountryID:", countryID)
		if slow {
			time.Sleep(500 * time.Millisecond)
		}

		// Done
		log.Println("")
		if slow {
			time.Sleep(500 * time.Millisecond)
		}
	}
}
