package cmd

import (
	"context"
	"fmt"
	"sync"
	"time"
	"whynoipv6/internal/core"
	"whynoipv6/internal/logger"
	"whynoipv6/internal/resolver"

	"github.com/spf13/cobra"
)

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
		metricService = *core.NewMetricService(db)
		// statService = *core.NewStatService(db)
		// toolboxService = *toolbox.NewToolboxService(cfg.GeoIPPath, cfg.Nameserver)
		getCampaignSites()
	},
}
var logg = logger.GetLogger()

func init() {
	campaignCmd.AddCommand(campaignCrawlCmd)
}

// getCampaignSites crawls the campaign sites in the database.
func getCampaignSites() {
	ctx := context.Background()
	logg := logg.With().Str("service", "campaignCrawl").Logger()

	startTime := time.Now()
	defer func() {
		logg.Info().Msg("Crawl completed in " + time.Since(startTime).Round(time.Second).String())
	}()

	// Workers
	workerCount := 5

	// Run the crawler indefinitely.
	for {
		// Start Time
		t := time.Now()
		logg.Info().Msg("Starting crawl at " + t.Format("2006-01-02 15:04:05"))

		domainChan := make(chan core.CampaignDomainModel, workerCount)
		var wg sync.WaitGroup

		// Start worker goroutines for this crawl
		for i := 0; i < workerCount; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for domain := range domainChan {
					processDomain(ctx, domain)
				}
			}()
		}

		var offset int64 = 0
		const limit int64 = 50
		var totalCheckedDomains int

		for {
			var updatedDomains int
			loopTime := time.Now()

			domains, err := campaignService.CrawlCampaignDomain(ctx, offset, limit)
			if err != nil {
				logg.Error().Err(err).Msg("Could not get domains to check")
				break
			}

			if len(domains) == 0 {
				logg.Info().Msg("All domains checked!")
				break
			}

			for _, domain := range domains {
				domainChan <- domain
				updatedDomains++
				totalCheckedDomains++
			}

			offset += limit

			// Print the current progress.
			logg.Info().Msgf("Checked %v domains in %s", updatedDomains, time.Since(loopTime).Round(time.Second).String())
		}

		close(domainChan) // Close the channel to signal the workers to stop
		wg.Wait()         // Wait for all workers to finish

		logg.Info().Msgf("Total domains checked: %v", totalCheckedDomains)
		logg.Info().Msg("Time for crawl: " + time.Since(t).Round(time.Second).String())

		// Sleep for 2 hours before starting the next crawl.
		logg.Info().Msg("Time until next check: 2 hours")
		time.Sleep(20 * time.Second)
	}
}

func processDomain(ctx context.Context, domain core.CampaignDomainModel) {
	logg := logg.With().Str("service", "processDomain").Logger()

	// Check if the domain has IPv6 support.
	checkResult, err := checkCampaignDomain(domain)
	if err != nil {
		logg.Error().Err(err).Msg("Could not check domain")
		return
	}

	// Update the domain with the result.
	err = updateCampaignDomain(ctx, domain, checkResult)
	if err != nil {
		logg.Error().Err(err).Msg("Could not update domain")
		return
	}
}

func checkCampaignDomain(domain core.CampaignDomainModel) (core.CampaignDomainModel, error) {
	checkResult := core.CampaignDomainModel{}
	logg := logg.With().Str("service", "checkCampaignDomain").Logger()

	// Validate domain
	err := resolver.ValidateDomain(domain.Site)
	if err != nil {
		logg.Error().Err(err).Msg("Invalid domain")
		return domain, err
	}

	// Check if the domain has IPv6 support.
	domainResult, err := resolver.DomainStatus(domain.Site)
	if err != nil {
		logg.Error().Err(err).Msg("Could not check domain")
		return domain, err
	}

	// Map the result to the domain model.
	checkResult.BaseDomain = domainResult.BaseDomain
	checkResult.WwwDomain = domainResult.WwwDomain
	checkResult.Nameserver = domainResult.Nameserver
	checkResult.MXRecord = domainResult.MXRecord

	// Retrieve ASN information for the domain.
	// asnID, err := getASNInfo(domain.Site)
	// if err != nil {
	// 	logg.Error().Err(err).Msg("Could not get ASN info")
	// }
	// checkResult.AsnID = asnID
	checkResult.AsnID = 1

	// Map country code to country table
	// countryCode, err := getCountryInfo(domain.Site)
	// if err != nil {
	// 	logg.Error().Err(err).Msg("Could not get country info")
	// }
	// checkResult.CountryID = countryCode

	// Update the domain with the check result.
	return checkResult, nil
}

func updateCampaignDomain(ctx context.Context, currentDomain, newDomain core.CampaignDomainModel) error {
	// Helper function to log and create a changelog entry.
	createChangelog := func(domain core.CampaignDomainModel, message string, status string) {
		_, err := changelogService.CampaignCreate(ctx, core.ChangelogModel{
			DomainID:   domain.ID,
			CampaignID: domain.CampaignID,
			Message:    message,
			IPv6Status: status,
		})
		if err != nil {
			logg.Error().Err(err).Msg("Could not write changelog")
		}
	}

	// Check if there is any changes to the domain.
	if currentDomain.BaseDomain != newDomain.BaseDomain {
		createChangelog(currentDomain, generateChangelog(currentDomain, newDomain), newDomain.BaseDomain)
		currentDomain.BaseDomain = newDomain.BaseDomain
		currentDomain.TsBaseDomain = time.Now()
		currentDomain.TsUpdated = time.Now()
	}

	if currentDomain.WwwDomain != newDomain.WwwDomain {
		createChangelog(currentDomain, generateChangelog(currentDomain, newDomain), newDomain.WwwDomain)
		currentDomain.WwwDomain = newDomain.WwwDomain
		currentDomain.TsWwwDomain = time.Now()
		currentDomain.TsUpdated = time.Now()
	}

	if currentDomain.Nameserver != newDomain.Nameserver {
		createChangelog(currentDomain, generateChangelog(currentDomain, newDomain), newDomain.Nameserver)
		currentDomain.Nameserver = newDomain.Nameserver
		currentDomain.TsNameserver = time.Now()
		currentDomain.TsUpdated = time.Now()
	}

	if currentDomain.MXRecord != newDomain.MXRecord {
		createChangelog(currentDomain, generateChangelog(currentDomain, newDomain), newDomain.MXRecord)
		currentDomain.MXRecord = newDomain.MXRecord
		currentDomain.TsMXRecord = time.Now()
		currentDomain.TsUpdated = time.Now()
	}

	// Update ASN ID and Country ID.
	currentDomain.AsnID = newDomain.AsnID
	currentDomain.CountryID = newDomain.CountryID

	// Update the check timestamp.
	currentDomain.TsCheck = time.Now()

	// Update the domain in the database.
	err := campaignService.UpdateCampaignDomain(ctx, currentDomain)
	if err != nil {
		logg.Error().Err(err).Msg("Could not update domain")
		return err
	}

	return nil
}

// generateChangelog checks the result of the change and generates a changelog entry.
func generateChangelog(currentDomain, newDomain core.CampaignDomainModel) string {
	// Base Domain
	if currentDomain.BaseDomain != newDomain.BaseDomain {
		if currentDomain.BaseDomain == "unsupported" && newDomain.BaseDomain == "supported" {
			return fmt.Sprintf("Got IPv6 record for %s", currentDomain.Site)
		}
		if currentDomain.BaseDomain == "supported" && newDomain.BaseDomain == "unsupported" {
			return fmt.Sprintf("Lost IPv6 record for %s", currentDomain.Site)
		}
		if currentDomain.BaseDomain == "no_record" && newDomain.BaseDomain == "supported" {
			return fmt.Sprintf("Got IPv6 record for %s", currentDomain.Site)
		}
		if currentDomain.BaseDomain == "no_record" && newDomain.BaseDomain == "unsupported" {
			return fmt.Sprintf("Got IPv4 Only record for %s", currentDomain.Site)
		}
		if newDomain.BaseDomain == "no_record" {
			return fmt.Sprintf("%s has no record", currentDomain.Site)
		}
	}
	// WWW Domain
	if currentDomain.WwwDomain != newDomain.WwwDomain {
		if currentDomain.WwwDomain == "unsupported" && newDomain.WwwDomain == "supported" {
			return fmt.Sprintf("Got IPv6 record for www.%s", currentDomain.Site)
		}
		if currentDomain.WwwDomain == "supported" && newDomain.WwwDomain == "unsupported" {
			return fmt.Sprintf("Lost IPv6 record for www.%s", currentDomain.Site)
		}
		if currentDomain.WwwDomain == "no_record" && newDomain.WwwDomain == "supported" {
			return fmt.Sprintf("Got IPv6 record for www.%s", currentDomain.Site)
		}
		if currentDomain.WwwDomain == "no_record" && newDomain.WwwDomain == "unsupported" {
			return fmt.Sprintf("Got IPv4 Only record for www.%s", currentDomain.Site)
		}
		if newDomain.WwwDomain == "no_record" {
			return fmt.Sprintf("www.%s has no record", currentDomain.Site)
		}
	}

	// Nameserver
	if currentDomain.Nameserver != newDomain.Nameserver {
		if currentDomain.Nameserver == "unsupported" && newDomain.Nameserver == "supported" {
			return fmt.Sprintf("Nameserver got IPv6 record for %s", currentDomain.Site)
		}
		if currentDomain.Nameserver == "supported" && newDomain.Nameserver == "unsupported" {
			return fmt.Sprintf("Nameserver lost IPv6 record for %s", currentDomain.Site)
		}
		if currentDomain.Nameserver == "no_record" && newDomain.Nameserver == "supported" {
			return fmt.Sprintf("Nameserver got IPv6 record for %s", currentDomain.Site)
		}
		if currentDomain.Nameserver == "no_record" && newDomain.Nameserver == "unsupported" {
			return fmt.Sprintf("Nameserver got IPv4 Only record for %s", currentDomain.Site)
		}
		if newDomain.Nameserver == "no_record" {
			return fmt.Sprintf("No NS records for %s", currentDomain.Site)
		}
	}

	// MX Record
	if currentDomain.MXRecord != newDomain.MXRecord {
		if currentDomain.MXRecord == "unsupported" && newDomain.MXRecord == "supported" {
			return fmt.Sprintf("MX record got IPv6 record for %s", currentDomain.Site)
		}
		if currentDomain.MXRecord == "supported" && newDomain.MXRecord == "unsupported" {
			return fmt.Sprintf("MX record lost IPv6 record for %s", currentDomain.Site)
		}
		if currentDomain.MXRecord == "no_record" && newDomain.MXRecord == "supported" {
			return fmt.Sprintf("MX record got IPv6 record for %s", currentDomain.Site)
		}
		if currentDomain.MXRecord == "no_record" && newDomain.MXRecord == "unsupported" {
			return fmt.Sprintf("MX record got IPv4 Only record for %s", currentDomain.Site)
		}
		if newDomain.MXRecord == "no_record" {
			return fmt.Sprintf("No MX records for %s", currentDomain.Site)
		}
	}

	return fmt.Sprintf("Unknown change for %s: %v %v", currentDomain.Site, currentDomain, newDomain)
}
