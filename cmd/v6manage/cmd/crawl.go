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
var changelogService core.ChangelogService
var domainService core.DomainService
var countryService core.CountryService
var toolboxService toolbox.Service
var asnService core.ASNService
var statService core.StatService
var resolver *toolbox.Resolver
var metricService core.MetricService

// crawlCmd represents the crawl command
var crawlCmd = &cobra.Command{
	Use:   "crawl",
	Short: "Crawls the sites in the database",
	Long:  "Crawls the sites in the database",
	Run: func(cmd *cobra.Command, args []string) {
		changelogService = *core.NewChangelogService(db)
		domainService = *core.NewDomainService(db)
		countryService = *core.NewCountryService(db)
		asnService = *core.NewASNService(db)
		statService = *core.NewStatService(db)
		metricService = *core.NewMetricService(db)
		toolboxService = *toolbox.NewToolboxService(cfg.GeoIPPath, cfg.Nameserver)
		getSites()
	},
}

func init() {
	rootCmd.AddCommand(crawlCmd)
}

func getSites() {
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
		currentTime := time.Now()
		log.Println("Starting crawl at", currentTime.Format("2006-01-02 15:04:05"))

		var offset int32 = 0
		const limit int32 = 50
		var updatedDomains int32 = 0
		var totalCheckedDomains int32 = 0
		var wg sync.WaitGroup

		// Main loop to crawl and update domains.
		for {
			loopStartTime := time.Now()

			domains, err := domainService.CrawlDomain(ctx, offset, limit)
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
				go func(domain core.DomainModel) {
					defer wg.Done()

					checkResult, err := checkDomain(domain)
					if err != nil {
						log.Printf("[%s] checkDomain error: %s\n", domain.Site, err)
						return
					}

					err = updateDomain(domain, checkResult)
					if err != nil {
						log.Printf("[%s] updateDomain error: %s\n", domain.Site, err)
					}
					if verbose {
						log.Printf("[%s] Updated\n", domain.Site)
					}

					updatedDomains++
				}(domain)
			}
			// 🦗
			wg.Wait()

			// Update the progress.
			offset += limit
			totalCheckedDomains += int32(len(domains))

			log.Printf(red+"Checked %d sites, took %v [Total: %d/%d]%s", len(domains), prettyDuration(time.Since(loopStartTime)), updatedDomains, totalCheckedDomains, reset)
		}

		// Collect and store domain statistics.
		stats, err := statService.DomainStats(ctx)
		if err != nil {
			log.Printf("Error getting stats: %s\n", err)
		}

		log.Printf(red+"Checked/Updated %d/%d sites in %s%s\n", updatedDomains, totalCheckedDomains, prettyDuration(time.Since(currentTime)), reset)

		// Update healthcheck status
		toolboxService.HealthCheckUpdate(cfg.HealthcheckCrawler)

		// Notify partyvan
		toolboxService.NotifyIrc(fmt.Sprintf("[WhyNoIPv6] Crawler checked %d/%d sites in %s", updatedDomains, totalCheckedDomains, prettyDuration(time.Since(currentTime))))

		// Store crawler metrics.
		crawlData := map[string]interface{}{
			"duration":      time.Since(currentTime).Seconds(),
			"total_checked": totalCheckedDomains,
			"updated":       updatedDomains,
		}
		if err := metricService.StoreMetric(ctx, "crawler", crawlData); err != nil {
			log.Printf("Error storing metric: %s\n", err)
		}
		if err := metricService.StoreMetric(ctx, "domains", stats); err != nil {
			log.Printf("Error storing metric: %s\n", err)
		}

		// Sleep for 2 hours before starting the next crawl.
		log.Println("Time until next check: 2 hours")
		time.Sleep(2 * time.Hour)
	}
}

// checkDomain runs all the checks on a domain
func checkDomain(domainToCheck core.DomainModel) (core.DomainModel, error) {
	checkedDomain := core.DomainModel{}

	// Check if domain has any DNS records, else disable it before performing any checks
	if err := toolboxService.ValidateDomain(domainToCheck.Site); err != nil {
		if err.Error() == "NXDOMAIN" {
			// Disable domain
			if disableErr := domainService.DisableDomain(context.Background(), domainToCheck.Site); disableErr != nil {
				log.Printf("[%s] Error disabling domain: %s\n", domainToCheck.Site, err)
			}
			return checkedDomain, fmt.Errorf("Disabling domain: %v", err)
		}
		return checkedDomain, fmt.Errorf("Validate domain error: %v", err)
	}

	// Check if domain has AAAA records
	aaaaCheck, err := toolboxService.CheckTLD(domainToCheck.Site)
	if err != nil && verbose {
		log.Printf("[%s] CheckTLD AAAA error: %s\n", domainToCheck.Site, err)
	}
	checkedDomain.CheckAAAA = aaaaCheck.IPv6

	// Check if www.domain has AAAA record
	wwwCheck, err := toolboxService.CheckTLD(fmt.Sprintf("www.%s", domainToCheck.Site))
	if err != nil && verbose {
		log.Printf("[%s] CheckTLD WWW error: %s\n", domainToCheck.Site, err)
	}
	checkedDomain.CheckWWW = wwwCheck.IPv6

	// Check if domain has AAAA record for nameservers
	nsCheck, err := toolboxService.CheckNS(domainToCheck.Site)
	if err != nil && verbose {
		log.Printf("[%s] CheckNS error: %s", domainToCheck.Site, err)
	}
	checkedDomain.CheckNS = nsCheck.IPv6

	// --------------------------------------------------------------------------------------------------------------------------
	// Check if it is possible to connect to domain using IPv6 only
	//
	// curlCheck, err := toolboxService.CheckCurl(domainToCheck.Site)
	// if err != nil && verbose {
	// 	log.Printf("[%s] CheckCurl error: %s\n", domainToCheck.Site, err)
	// }
	// checkedDomain.CheckCurl = curlCheck
	// --------------------------------------------------------------------------------------------------------------------------

	// Map AsnID to ASN Table
	asnID, err := getASNInfo(domainToCheck.Site)
	if err != nil {
		log.Printf("[%s] getASNInfo error: %s\n", domainToCheck.Site, err)
	}
	checkedDomain.AsnID = asnID

	// Map CountryID to Country Table
	countryID, err := getCountryInfo(domainToCheck.Site)
	if err != nil {
		log.Printf("[%s] getCountryID error: %s\n", domainToCheck.Site, err)
	}
	checkedDomain.CountryID = countryID

	return checkedDomain, nil
}

func updateDomain(existingDomain, newDomain core.DomainModel) error {
	ctx := context.Background()

	// Helper function to create changelog entries
	createChangelog := func(message string) {
		_, err := changelogService.Create(ctx, core.ChangelogModel{
			DomainID: int32(existingDomain.ID),
			Message:  message,
		})
		if err != nil {
			log.Printf("[%s] Error writing changelog: %s\n", existingDomain.Site, err)
		}
	}

	// Compare AAAA record result
	// Domain changes from no AAAA to AAAA
	if !existingDomain.CheckAAAA && newDomain.CheckAAAA {
		existingDomain.CheckAAAA = true
		existingDomain.TsAAAA = time.Now()
		existingDomain.TsUpdated = time.Now()

		createChangelog(fmt.Sprintf("Got AAAA record for %s", existingDomain.Site))
	}
	// Domain changes from AAAA to no AAAA
	if existingDomain.CheckAAAA && !newDomain.CheckAAAA {
		existingDomain.CheckAAAA = false
		existingDomain.TsUpdated = time.Now()

		createChangelog(fmt.Sprintf("Lost AAAA record for %s", existingDomain.Site))
	}

	// Compare WWW record result
	// Domain changes from no WWW to WWW
	if !existingDomain.CheckWWW && newDomain.CheckWWW {
		existingDomain.CheckWWW = true
		existingDomain.TsWWW = time.Now()
		existingDomain.TsUpdated = time.Now()

		createChangelog(fmt.Sprintf("Got AAAA record for www.%s", existingDomain.Site))
	}
	// Domain changes from WWW to no WWW
	if existingDomain.CheckWWW && !newDomain.CheckWWW {
		existingDomain.CheckWWW = false
		existingDomain.TsUpdated = time.Now()

		createChangelog(fmt.Sprintf("Lost AAAA record for www.%s", existingDomain.Site))
	}

	// Compare NS record result
	// Nameserver changes from no NS to NS
	if !existingDomain.CheckNS && newDomain.CheckNS {
		existingDomain.CheckNS = true
		existingDomain.TsNS = time.Now()
		existingDomain.TsUpdated = time.Now()

		createChangelog(fmt.Sprintf("Nameserver got AAAA record for %s", existingDomain.Site))
	}
	// Nameserver changes from NS to no NS
	if existingDomain.CheckNS && !newDomain.CheckNS {
		existingDomain.CheckNS = false
		existingDomain.TsUpdated = time.Now()

		createChangelog(fmt.Sprintf("Nameserver lost AAAA record for %s", existingDomain.Site))
	}

	// Set AsnID and CountryID
	existingDomain.AsnID = newDomain.AsnID
	existingDomain.CountryID = newDomain.CountryID

	// Set check time
	existingDomain.TsCheck = time.Now()

	// Write to database
	err := domainService.UpdateDomain(ctx, existingDomain)
	if err != nil {
		log.Printf("[%s] Error writing to database: %s\n", existingDomain.Site, err)
		log.Fatalf("FATAL: %+v\n", existingDomain)
	}

	return nil
}
