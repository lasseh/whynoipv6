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

	// Dns Client
	resolver, err = toolboxService.NewResolver()
	if err != nil {
		log.Printf("Could not initalize dns resolver: %s\n", err)
		os.Exit(1)
	}
	log.Println("DNS resolver initialized", resolver)

	// Loop forever
	for {
		t := time.Now()
		log.Println("Starting crawl at", t.Format("2006-01-02 15:04:05"))

		var offset int32 = 0
		var limit int32 = 50
		var updated int32 = 0
		var totalChecked int32 = 0
		var wg sync.WaitGroup
		// Main loop
		for {
			// Loop time
			lt := time.Now()

			domains, err := domainService.CrawlDomain(ctx, offset, limit)
			if err != nil {
				log.Fatal(err.Error())
			}

			// Stop if no more data
			if len(domains) == 0 {
				log.Println("All domains checked!")
				break
			}

			// Loop through domains
			for _, domain := range domains {
				wg.Add(1)
				go func(domain core.DomainModel) {
					defer wg.Done()

					check, err := checkDomain(domain)
					if err != nil {
						log.Printf("[%s] checkDomain error: %s\n", domain.Site, err)
						// Skip next steps
						return
					}

					// Update domain
					err = updateDomain(domain, check)
					if err != nil {
						log.Printf("[%s] updateDomain error: %s\n", domain.Site, err)
					}
					if verbose {
						log.Printf("[%s] Updated\n", domain.Site)
					}

					// Increment updated
					updated++
				}(domain)
			}
			// 🦗
			wg.Wait()

			// Increment offset
			offset += limit
			totalChecked += int32(len(domains))

			log.Printf(red+"Checked %d sites, took %v [Total: %d/%d]%s", len(domains), prettyDuration(time.Since(lt)), updated, totalChecked, reset)
		}

		// Collect stats
		stats, err := statService.Domainstats(ctx)
		if err != nil {
			log.Printf("Error getting stats: %s\n", err)
		}

		log.Printf(red+"Checked/Updated %d/%d sites in %s%s\n", updated, totalChecked, prettyDuration(time.Since(t)), reset)

		// Update healthcheck status
		toolboxService.HealthCheckUpdate(cfg.HealthcheckCrawler)

		// Notify partyvan
		toolboxService.NotifyIrc(fmt.Sprintf("[WhyNoIPv6] Crawler checked %d/%d sites in %s", updated, totalChecked, prettyDuration(time.Since(t))))

		// Metrics
		crawlData := map[string]interface{}{
			"duration":      time.Since(t).Seconds(),
			"total_checked": totalChecked,
			"updated":       updated,
		}
		if err := metricService.StoreMetric(ctx, "crawler", crawlData); err != nil {
			log.Printf("Error storing metric: %s\n", err)
		}
		if err := metricService.StoreMetric(ctx, "domains", stats); err != nil {
			log.Printf("Error storing metric: %s\n", err)
		}

		// Sleep for 2 hours
		log.Println("time until next check: 2 hours")
		time.Sleep(2 * time.Hour)
	}
}

// newCheckDomain runs all the checks on a domain
func checkDomain(domain core.DomainModel) (core.DomainModel, error) {
	result := core.DomainModel{}

	// Check if domain is has any dns records, else just disable it before any checks
	if err := toolboxService.ValidateDomain(domain.Site); err != nil {
		if err.Error() == "NXDOMAIN" {
			// Disable domain
			if err2 := domainService.DisableDomain(context.Background(), domain.Site); err2 != nil {
				log.Printf("[%s] Error disabling domain: %s\n", domain.Site, err)
			}
			return result, fmt.Errorf("Disabling domain: %v", err)
		}
		return result, fmt.Errorf("Validate domain error: %v", err)

	}

	// Check if domain has AAAA records
	checkAAAA, err := toolboxService.CheckTLD(domain.Site)
	if err != nil {
		if verbose {
			log.Printf("[%s] CheckTLD AAAA error: %s\n", domain.Site, err)
		}
	}
	result.CheckAAAA = checkAAAA.IPv6

	// Check if wwww.domain has AAAA record
	checkWWW, err := toolboxService.CheckTLD(fmt.Sprintf("www.%s", domain.Site))
	if err != nil {
		if verbose {
			log.Printf("[%s] CheckTLD WWW error: %s\n", domain.Site, err)
		}
	}
	result.CheckWWW = checkWWW.IPv6

	// Check if domain has AAAA record for nameservers
	checkNS, err := toolboxService.CheckNS(domain.Site)
	if err != nil {
		if verbose {
			log.Printf("[%s] CheckNS error: %s", domain.Site, err)
		}
	}
	result.CheckNS = checkNS.IPv6

	// --------------------------------------------------------------------------------------------------------------------------
	// Check if it is possible to connect to domain using IPv6 only
	//
	// checkCurl, err := toolboxService.CheckCurl(domain.Site)
	// if err != nil {
	// 	if verbose {
	// 		log.Printf("[%s] CheckCurl error: %s\n", domain.Site, err)
	// 	}
	// }
	// result.CheckCurl = checkCurl
	// --------------------------------------------------------------------------------------------------------------------------

	// Map AsnID to ASN Table
	asnid, err := getASNInfo(domain.Site)
	if err != nil {
		log.Printf("[%s] getASNInfo error: %s\n", domain.Site, err)
	}
	result.AsnID = asnid

	// Map CountryID to Country Table
	countryid, err := getCountryInfo(domain.Site)
	if err != nil {
		log.Printf("[%s] getCountryID error: %s\n", domain.Site, err)
	}
	result.CountryID = countryid

	return result, nil
}

func updateDomain(domain, new core.DomainModel) error {
	ctx := context.Background()

	// Compare result for AAAA record
	// Domain go from no AAAA to AAAA
	if !domain.CheckAAAA && new.CheckAAAA {
		domain.CheckAAAA = true
		domain.TsAAAA = time.Now()
		domain.TsUpdated = time.Now()

		_, err := changelogService.Create(context.Background(), core.ChangelogModel{
			DomainID: int32(domain.ID),
			Message:  fmt.Sprintf("Got AAAA record for %s", domain.Site),
		})
		if err != nil {
			log.Printf("[%s] Error writing changelog: %s\n", domain.Site, err)
		}
	}
	// Domain go from AAAA to no AAAA
	if domain.CheckAAAA && !new.CheckAAAA {
		domain.CheckAAAA = false
		domain.TsUpdated = time.Now()

		_, err := changelogService.Create(context.Background(), core.ChangelogModel{
			DomainID: int32(domain.ID),
			Message:  fmt.Sprintf("Lost AAAA record for %s", domain.Site),
		})
		if err != nil {
			log.Printf("[%s] Error writing changelog: %s", domain.Site, err)
		}
	}

	// WWW Record
	// Domain go from no WWW to WWW
	if !domain.CheckWWW && new.CheckWWW {
		domain.CheckWWW = true
		domain.TsWWW = time.Now()
		domain.TsUpdated = time.Now()

		_, err := changelogService.Create(context.Background(), core.ChangelogModel{
			DomainID: int32(domain.ID),
			Message:  fmt.Sprintf("Got AAAA record for www.%s", domain.Site),
		})
		if err != nil {
			log.Printf("[%s] Error writing changelog: %s\n", domain.Site, err)
		}
	}
	// Domain go from WWW to no WWW
	if domain.CheckWWW && !new.CheckWWW {
		domain.CheckWWW = false
		domain.TsUpdated = time.Now()

		_, err := changelogService.Create(context.Background(), core.ChangelogModel{
			DomainID: int32(domain.ID),
			Message:  fmt.Sprintf("Lost AAAA record for www.%s", domain.Site),
		})
		if err != nil {
			log.Printf("[%s] Error writing changelog: %s\n", domain.Site, err)
		}
	}

	// NS record
	// Domain go from no NS to NS
	if !domain.CheckNS && new.CheckNS {
		domain.CheckNS = true
		domain.TsNS = time.Now()
		domain.TsUpdated = time.Now()

		_, err := changelogService.Create(context.Background(), core.ChangelogModel{
			DomainID: int32(domain.ID),
			Message:  fmt.Sprintf("Nameserver got AAAA record for %s", domain.Site),
		})
		if err != nil {
			log.Printf("[%s] Error writing changelog: %s\n", domain.Site, err)
		}
	}
	// Domain go from NS to no NS
	if domain.CheckNS && !new.CheckNS {
		domain.CheckNS = false
		domain.TsUpdated = time.Now()

		_, err := changelogService.Create(context.Background(), core.ChangelogModel{
			DomainID: int32(domain.ID),
			Message:  fmt.Sprintf("Nameserver lost AAAA record for %s", domain.Site),
		})
		if err != nil {
			log.Printf("[%s] Error writing changelog: %s\n", domain.Site, err)
		}
	}

	// Curl
	// Compare curl result

	// Set AsnID
	domain.AsnID = new.AsnID

	// Set CountryID
	domain.CountryID = new.CountryID

	// Set check time
	domain.TsCheck = time.Now()

	// Write to database
	err = domainService.UpdateDomain(ctx, domain)
	if err != nil {
		log.Printf("[%s] Error writing to database: %s\n", domain.Site, err)
		log.Fatalf("FATAL: %+v\n", domain)
	}

	return nil
}
