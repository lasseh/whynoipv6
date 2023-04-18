package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"
	"whynoipv6/internal/core"
	"whynoipv6/internal/toolbox"

	"github.com/spf13/cobra"
)

// Global services
var campaignService core.CampaignService

// crawlCmd represents the crawl command
var campaignCrawlCmd = &cobra.Command{
	Use:   "crawl",
	Short: "Crawls the campaign sites in the database",
	Long:  "Crawls the campagin sites in the database",
	Run: func(cmd *cobra.Command, args []string) {
		changelogService = *core.NewChangelogService(db)
		campaignService = *core.NewCampaignService(db)
		countryService = *core.NewCountryService(db)
		asnService = *core.NewASNService(db)
		statService = *core.NewStatService(db)
		metricService = *core.NewMetricService(db)
		toolboxService = *toolbox.NewToolboxService(cfg.GeoIPPath, cfg.Nameserver)
		getCampaignSites()
	},
}

func init() {
	campaignCmd.AddCommand(campaignCrawlCmd)
}

func getCampaignSites() {
	ctx := context.Background()

	red := "\033[31m"
	reset := "\033[0m"

	// Initialize DNS client.
	_, err := toolboxService.NewResolver()
	if err != nil {
		log.Printf("Could not initialize DNS resolver: %s\n", err)
		os.Exit(1)
	}

	// Run the crawler indefinitely.
	for {
		// start time
		t := time.Now()
		log.Println("Starting crawl at", t.Format("2006-01-02 15:04:05"))

		var offset int32 = 0
		const limit int32 = 5
		var updatedDomains int32 = 0
		var totalCheckedDomains int32 = 0
		var wg sync.WaitGroup

		// Main loop to crawl and update domains.
		for {
			loopStartTime := time.Now()

			domains, err := campaignService.CrawlCampaignDomain(ctx, offset, limit)
			if err != nil {
				log.Fatal(err.Error())
			}

			// Stop if no more data
			if len(domains) == 0 {
				log.Println("All domains checked!")
				break
			}

			// Loop through domains and check IPv6 support concurrently.
			for _, domain := range domains {
				wg.Add(1)
				go func(domain core.CampaignDomainModel) {
					defer wg.Done()

					checkResult, err := checkCampaignDomain(domain)
					if err != nil {
						log.Printf("[%s] checkDomain error: %s\n", domain.Site, err)
						return
					}

					// Update domain
					err = updateCampaignDomain(domain, checkResult)
					if err != nil {
						log.Printf("[%s] updateDomain error: %s\n", domain.Site, err)
					}
					if verbose {
						log.Printf("[%s] Updated\n", domain.Site)
					}

					// Increment total
					updatedDomains++
				}(domain)
			}
			// 🦗
			wg.Wait()

			// Increment offset
			// Update the progress.
			offset += limit
			totalCheckedDomains += int32(len(domains))

			log.Printf(red+"Checked %d sites, took %v [Total: %d/%d]%s", len(domains), prettyDuration(time.Since(loopStartTime)), updatedDomains, totalCheckedDomains, reset)
		}

		log.Printf(red+"Checked %d sites in %s%s\n", updatedDomains, prettyDuration(time.Since(t)), reset)

		// Update healthcheck status
		toolboxService.HealthCheckUpdate(cfg.HealthcheckCampaign)

		// Notify partyvan
		toolboxService.NotifyIrc(fmt.Sprintf("[WhyNoIPv6] Campaign Crawler checked %d/%d sites in %s", updatedDomains, totalCheckedDomains, prettyDuration(time.Since(t))))

		// Store crawler metrics.
		crawlData := map[string]interface{}{
			"duration":      time.Since(t).Seconds(),
			"total_checked": totalCheckedDomains,
			"updated":       updatedDomains,
		}
		if err := metricService.StoreMetric(ctx, "crawler_campaign", crawlData); err != nil {
			log.Printf("Error storing metric: %s\n", err)
		}

		// Sleep for 2 hours before starting the next crawl.
		log.Println("Time until next check: 2 hours")
		time.Sleep(2 * time.Hour)
	}
}

// checkCampaignDomain runs all the checks on a domain
func checkCampaignDomain(domain core.CampaignDomainModel) (core.CampaignDomainModel, error) {
	checkResult := core.CampaignDomainModel{}

	// Check if domain has any DNS records, else just disable it before any checks
	err := toolboxService.ValidateDomain(domain.Site)
	if err != nil {
		if err.Error() == "NXDOMAIN" {
			// Disable domain
			// if disableErr := campaignService.DisableCampaignDomain(context.Background(), domain.Site); disableErr != nil {
			// 	log.Printf("[%s] Error disabling domain: %s\n", domain.Site, err)
			// }
			// return checkResult, fmt.Errorf("Disabling domain: %v", err)
			if verbose {
				log.Printf("[%s] Domain does not exist, should be disabled: %v\n", domain.Site, err)
			}
		}
		return checkResult, fmt.Errorf("Validate domain error: %v", err)
	}

	// Check if domain has AAAA records
	tldCheck, err := toolboxService.CheckTLD(domain.Site)
	if err != nil && verbose {
		log.Printf("[%s] CheckTLD AAAA error: %s\n", domain.Site, err)
	}
	checkResult.CheckAAAA = tldCheck.IPv6

	// Check if www.domain has AAAA record
	wwwCheck, err := toolboxService.CheckTLD(fmt.Sprintf("www.%s", domain.Site))
	if err != nil && verbose {
		log.Printf("[%s] CheckTLD WWW error: %s\n", domain.Site, err)
	}
	checkResult.CheckWWW = wwwCheck.IPv6

	// Check if domain has AAAA record for nameservers
	nsCheck, err := toolboxService.CheckNS(domain.Site)
	if err != nil && verbose {
		log.Printf("[%s] CheckNS error: %s", domain.Site, err)
	}
	checkResult.CheckNS = nsCheck.IPv6

	// --------------------------------------------------------------------------------------------------------------------------
	// Check if it is possible to connect to domain using IPv6 only
	//
	// curlCheck, err := toolboxService.CheckCurl(domain.Site)
	// if err != nil && verbose {
	// 	log.Printf("[%s] CheckCurl error: %s\n", domain.Site, err)
	// }
	// checkResult.CheckCurl = curlCheck
	// --------------------------------------------------------------------------------------------------------------------------

	// Map AsnID to ASN Table
	asnID, err := getASNInfo(domain.Site)
	if err != nil {
		log.Printf("[%s] getASNInfo error: %s\n", domain.Site, err)
	}
	checkResult.AsnID = asnID

	// Map CountryID to Country Table
	countryID, err := getCountryInfo(domain.Site)
	if err != nil {
		log.Printf("[%s] getCountryID error: %s\n", domain.Site, err)
	}
	checkResult.CountryID = countryID

	return checkResult, nil
}

func updateCampaignDomain(currentDomain, newDomain core.CampaignDomainModel) error {
	ctx := context.Background()

	// Helper function to log and create changelog entry.
	createChangelog := func(domain core.CampaignDomainModel, message string) {
		_, err := changelogService.CampaignCreate(ctx, core.ChangelogModel{
			DomainID:   int32(domain.ID),
			CampaignID: domain.CampaignID,
			Message:    message,
		})
		if err != nil {
			log.Printf("[%s] Error writing changelog: %s\n", domain.Site, err)
		}
	}

	// Check and update AAAA record status.
	if !currentDomain.CheckAAAA && newDomain.CheckAAAA {
		currentDomain.CheckAAAA = true
		currentDomain.TsAAAA = time.Now()
		currentDomain.TsUpdated = time.Now()
		createChangelog(currentDomain, fmt.Sprintf("Got AAAA record for %s", currentDomain.Site))
	} else if currentDomain.CheckAAAA && !newDomain.CheckAAAA {
		currentDomain.CheckAAAA = false
		currentDomain.TsUpdated = time.Now()
		createChangelog(currentDomain, fmt.Sprintf("Lost AAAA record for %s", currentDomain.Site))
	}

	// Check and update WWW AAAA record status.
	if !currentDomain.CheckWWW && newDomain.CheckWWW {
		currentDomain.CheckWWW = true
		currentDomain.TsWWW = time.Now()
		currentDomain.TsUpdated = time.Now()
		createChangelog(currentDomain, fmt.Sprintf("Got AAAA record for www.%s", currentDomain.Site))
	} else if currentDomain.CheckWWW && !newDomain.CheckWWW {
		currentDomain.CheckWWW = false
		currentDomain.TsUpdated = time.Now()
		createChangelog(currentDomain, fmt.Sprintf("Lost AAAA record for www.%s", currentDomain.Site))
	}

	// Check and update nameserver AAAA record status.
	if !currentDomain.CheckNS && newDomain.CheckNS {
		currentDomain.CheckNS = true
		currentDomain.TsNS = time.Now()
		currentDomain.TsUpdated = time.Now()
		createChangelog(currentDomain, fmt.Sprintf("Nameserver got AAAA record for %s", currentDomain.Site))
	} else if currentDomain.CheckNS && !newDomain.CheckNS {
		currentDomain.CheckNS = false
		currentDomain.TsUpdated = time.Now()
		createChangelog(currentDomain, fmt.Sprintf("Nameserver lost AAAA record for %s", currentDomain.Site))
	}

	// Update ASN ID and Country ID.
	currentDomain.AsnID = newDomain.AsnID
	currentDomain.CountryID = newDomain.CountryID

	// Update the check timestamp.
	currentDomain.TsCheck = time.Now()

	// Update the domain in the database.
	err := campaignService.UpdateCampaignDomain(ctx, currentDomain)
	if err != nil {
		log.Printf("[%s] Error writing to database: %s\n", currentDomain.Site, err)
		log.Fatalf("FATAL: %+v\n", currentDomain)
	}

	return nil
}
