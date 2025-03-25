package cmd

import (
	"context"
	"errors"
	"fmt"
	"time"

	"whynoipv6/internal/core"
	"whynoipv6/internal/geoip"
	"whynoipv6/internal/resolver"
	"whynoipv6/internal/toolbox"

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
		campaignCrawl()
	},
}

func init() {
	campaignCmd.AddCommand(campaignCrawlCmd)
}

func campaignCrawl() {
	ctx := context.Background()
	logg := logg.With().Str("service", "campaignCrawl").Logger()

	// Initialize the geoip database.
	err := geoip.Initialize(cfg.GeoIPPath)
	if err != nil {
		logg.Error().Err(err).Msg("Could not initialize geoip database")
		return
	}

	const (
		numWorkers = 5               // Number of workers
		jobTimeout = 2 * time.Minute // Timeout for each batch of jobs
	)

	// Run the crawler indefinitely.
	for {
		t := time.Now()
		logg.Info().Msg("Starting Campaign crawl at " + t.Format("2006-01-02 15:04:05"))

		var offset int64 = 0        // Offset for the database query
		const limit int64 = 50      // Limit for the database query
		var totalDomains int        // Total number of domains checked in this crawl
		var totalSuccessfulJobs int // Total number of successful jobs
		var totalFailedJobs int     // Total number of failed jobs

		// Inner loop
		for {
			var domainJobs int                                         // Number of domains updated in this batch
			successfulJobs := 0                                        // Successful jobs in this batch
			failedJobs := 0                                            // Failed jobs in this batch
			loopTime := time.Now()                                     // Start time for this batch
			campaignJobs := make(chan core.CampaignDomainModel, limit) // Channel for jobs
			done := make(
				chan bool,
				limit,
			) // Channel for signaling completion of jobs

			// Start workers for this batch of jobs
			for w := 1; w <= numWorkers; w++ {
				go processCampaignDomain(ctx, campaignJobs, done)
			}

			// Get domains to check
			domains, err := campaignService.CrawlCampaignDomain(ctx, offset, limit)
			if err != nil {
				// Ping the sql server to see if it's up
				if err = db.Ping(ctx); err != nil {
					toolbox.HealthCheckUpdate(cfg.HealthcheckCampaign, toolbox.HealthFail)
					logg.Fatal().Err(err).Msg("Database is down!")
					return
				}
				logg.Error().Err(err).Msg("Could not get domains to check")
				return
			}

			// Break out of loop if there are no domains left
			if len(domains) == 0 {
				break
			}

			// Send jobs to the workers
			for _, domain := range domains {
				campaignJobs <- domain // Send the job to the jobs channel
				domainJobs++           // Increment the number of domains updated in this batch
				totalDomains++         // Increment the total number of domains checked in this crawl
			}

			close(
				campaignJobs,
			) // Close the jobs channel and wait for this batch of workers to finish
			timeout := time.After(jobTimeout) // Timeout for this batch of jobs

			// This loop monitors the completion of domain processing jobs. It iterates up to 'domainJobs' times,
			// checking for job completion or timeout. Each iteration either increments 'successfulJobs' or 'failedJobs'
			// based on the job's success status reported via the 'done' channel. If a 'jobTimeout' occurs,
			// indicated by the 'timeout' channel, it logs a warning and exits the loop early using 'goto BatchTimeout',
			// thus handling potential delays in job processing.
			for a := 1; a <= domainJobs; a++ {
				select {
				case success := <-done:
					if success {
						successfulJobs++
					} else {
						failedJobs++
					}
				case <-timeout:
					logg.Warn().Msgf("Batch timeout after %v", jobTimeout)
					goto BatchTimeout // Break out of the inner loop
				}
			}

			// Batch finished
		BatchTimeout:
			offset += limit                       // Update the offset for the next batch
			totalSuccessfulJobs += successfulJobs // Update the total count of successful jobs
			totalFailedJobs += failedJobs         // Update the total count of failed jobs
			logg.Info().
				Msgf("Checked %v domains, Successful: %v, Failed: %v Duraation: %s", domainJobs, successfulJobs, failedJobs, prettyDuration(time.Since(loopTime)))
		}

		// Outer loop finished
		logg.Info().
			Msgf("Total Domains: %v domains, Successful Jobs: %v, Failed Jobs: %v Duration: %s", totalDomains, totalSuccessfulJobs, totalFailedJobs, prettyDuration(time.Since(t)))

		// Healthcheck reporting
		toolbox.HealthCheckUpdate(cfg.HealthcheckCampaign, toolbox.HealthOK)
		// Notify partyvan
		toolbox.NotifyIrc(
			fmt.Sprintf(
				"[WhyNoIPv6 Campaign] Total Domains: %v, Successful: %v, Failed: %v Duration: %s",
				totalDomains,
				totalSuccessfulJobs,
				totalFailedJobs,
				prettyDuration(time.Since(t)),
			),
		)

		// Store crawler metrics in the database.
		crawlData := map[string]any{
			"duration": time.Since(t).Seconds(),
			"total":    totalDomains,
			"success":  totalSuccessfulJobs,
			"failed":   totalFailedJobs,
		}
		if err := metricService.StoreMetric(ctx, "crawler_campaign", crawlData); err != nil {
			logg.Err(err).Msg("Error storing metric")
		}

		// Sleep for 2 hours
		logg.Info().Msg("Time until next check: 2 hours")
		time.Sleep(2 * time.Hour)
		// time.Sleep(20 * time.Second)
	}
}

// processCampaignDomain processes a domain and updates it in the database.
// Returns true if the job was successful, false if it failed
func processCampaignDomain(
	ctx context.Context,
	jobs <-chan core.CampaignDomainModel,
	done chan<- bool,
) {
	logg := logg.With().Str("service", "processCampaignDomain").Logger()
	for job := range jobs {
		// Process the job
		checkResult, err := checkCampaignDomain(ctx, job)
		if err != nil {
			logg.Error().Err(err).Msgf("[%s] Could not check domain", job.Site)
			done <- false // Signal completion indicating failure
			return        // Stop processing this job
		}

		// Update domain
		err = updateCampaignDomain(ctx, job, checkResult)
		if err != nil {
			logg.Error().Err(err).Msg("Could not update domain")
			done <- false // Signal completion indicating failure
			return        // Stop processing this job
		}

		done <- true // Signal completion, indicating if it was successful
	}
}

// checkCampaignDomain runs all the checks on the domain.
func checkCampaignDomain(
	ctx context.Context,
	domain core.CampaignDomainModel,
) (core.CampaignDomainModel, error) {
	checkResult := core.CampaignDomainModel{}
	logg := logg.With().Str("service", "checkCampaignDomain").Logger()

	// Validate domain
	// Ignore the rcode here, since we want to manually disable domains.
	_, err := resolver.ValidateDomain(domain.Site)
	if err != nil {
		// logg.Error().Err(err).Msg("Invalid domain")
		return domain, err
	}

	// Run all the checks on the domain.
	domainResult, err := resolver.DomainStatus(domain.Site)
	if err != nil {
		logg.Error().Err(err).Msgf("[%s] Could not check domain", domain.Site)
		return domain, err
	}

	// Map the result to the domain model.
	checkResult.ID = domain.ID
	checkResult.Site = domain.Site
	checkResult.CampaignID = domain.CampaignID
	checkResult.BaseDomain = domainResult.BaseDomain
	checkResult.WwwDomain = domainResult.WwwDomain
	checkResult.Nameserver = domainResult.Nameserver
	checkResult.MXRecord = domainResult.MXRecord

	// Check if the results are empty and set them to "no_record" if they are.
	if checkResult.BaseDomain == "" {
		logg.Error().Msgf("[%s] Empty BaseDomain", domain.Site)
		checkResult.BaseDomain = "no_record"
	}
	if checkResult.WwwDomain == "" {
		logg.Error().Msgf("[%s] Empty WwwDomain", domain.Site)
		checkResult.WwwDomain = "no_record"
	}
	if checkResult.Nameserver == "" {
		logg.Error().Msgf("[%s] Empty Nameserver", domain.Site)
		checkResult.Nameserver = "no_record"
	}
	if checkResult.MXRecord == "" {
		logg.Error().Msgf("[%s] Empty MXRecord", domain.Site)
		checkResult.MXRecord = "no_record"
	}

	// Retrieve ASN information for the domain if it has basic dns records.
	// If the domain has no records, set the ASN ID to 1 (Unknown).
	if checkResult.BaseDomain != "no_record" || checkResult.WwwDomain != "no_record" {
		asnID, err := getNetworkProvider(ctx, domain.Site)
		if err != nil {
			logg.Error().Err(err).Msg("Could not get ASN info")
		}
		// Check if the ASN is empty
		if asnID == 0 {
			logg.Error().Msgf("[%s] Empty ASN", domain.Site)
			checkResult.AsnID = 1
		}
		checkResult.AsnID = asnID
	} else {
		checkResult.AsnID = 1
	}

	// Map country code to country table
	// If the domain has no records, set the Country ID to 251 (Unknown).
	if checkResult.BaseDomain != "no_record" || checkResult.WwwDomain != "no_record" {
		countryID, err := getCountryID(ctx, domain.Site)
		if err != nil {
			logg.Error().Err(err).Msg("Could not get country info")
		}
		// Check if the country is empty
		if countryID == 0 {
			logg.Error().Msgf("[%s] Empty country", domain.Site)
			checkResult.CountryID = 251
		}
		checkResult.CountryID = countryID
	} else {
		checkResult.CountryID = 251
	}

	// Update the domain with the check result.
	return checkResult, nil
}

// updateDomain updates the domain in the database with the check result.
func updateCampaignDomain(
	ctx context.Context,
	currentDomain, newDomain core.CampaignDomainModel,
) error {
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
		changelog, err := generateCampaignChangelog(currentDomain, newDomain)
		if err != nil {
			logg.Error().Err(err).Msgf("[%s] Could not generate changelog", currentDomain.Site)
			return err
		}
		createChangelog(currentDomain, changelog, newDomain.BaseDomain)
		currentDomain.BaseDomain = newDomain.BaseDomain
		currentDomain.TsBaseDomain = time.Now()
		currentDomain.TsUpdated = time.Now()
	}

	if currentDomain.WwwDomain != newDomain.WwwDomain {
		changelog, err := generateCampaignChangelog(currentDomain, newDomain)
		if err != nil {
			logg.Error().Err(err).Msgf("[%s] Could not generate changelog", currentDomain.Site)
			return err
		}
		createChangelog(currentDomain, changelog, newDomain.WwwDomain)
		currentDomain.WwwDomain = newDomain.WwwDomain
		currentDomain.TsWwwDomain = time.Now()
		currentDomain.TsUpdated = time.Now()
	}

	if currentDomain.Nameserver != newDomain.Nameserver {
		changelog, err := generateCampaignChangelog(currentDomain, newDomain)
		if err != nil {
			logg.Error().Err(err).Msgf("[%s] Could not generate changelog", currentDomain.Site)
			return err
		}
		createChangelog(currentDomain, changelog, newDomain.Nameserver)
		currentDomain.Nameserver = newDomain.Nameserver
		currentDomain.TsNameserver = time.Now()
		currentDomain.TsUpdated = time.Now()
	}

	if currentDomain.MXRecord != newDomain.MXRecord {
		changelog, err := generateCampaignChangelog(currentDomain, newDomain)
		if err != nil {
			logg.Error().Err(err).Msgf("[%s] Could not generate changelog", currentDomain.Site)
			return err
		}
		createChangelog(currentDomain, changelog, newDomain.MXRecord)
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
		logg.Error().Err(err).Msgf("[%s] Could not update domain", currentDomain.Site)
		return err
	}

	// CampaignDomainLog stores a log of the latest crawl.
	type CampaignDomainLog struct {
		BaseDomain string `json:"base_domain"`
		WwwDomain  string `json:"www_domain"`
		Nameserver string `json:"nameserver"`
		MxRecord   string `json:"mx_record"`
	}

	// Write a log of the check.
	if err := campaignService.StoreCampaignDomainLog(ctx, currentDomain.ID, CampaignDomainLog{
		BaseDomain: newDomain.BaseDomain,
		WwwDomain:  newDomain.WwwDomain,
		Nameserver: newDomain.Nameserver,
		MxRecord:   newDomain.MXRecord,
	}); err != nil {
		logg.Error().Err(err).Msgf("[%s] Could not store campaign domain log", currentDomain.Site)
	}

	return nil
}

// generateCampaignChangelog checks the result of the change and generates a changelog entry.
func generateCampaignChangelog(currentDomain, newDomain core.CampaignDomainModel) (string, error) {
	// Base Domain
	if currentDomain.BaseDomain != newDomain.BaseDomain {
		if currentDomain.BaseDomain == "unsupported" && newDomain.BaseDomain == "supported" {
			return fmt.Sprintf("IPv6 enabled for %s", currentDomain.Site), nil
		}
		if currentDomain.BaseDomain == "supported" && newDomain.BaseDomain == "unsupported" {
			return fmt.Sprintf("IPv6 lost for %s", currentDomain.Site), nil
		}
		if currentDomain.BaseDomain == "no_record" && newDomain.BaseDomain == "supported" {
			return fmt.Sprintf("IPv6 enabled for %s", currentDomain.Site), nil
		}
		if currentDomain.BaseDomain == "no_record" && newDomain.BaseDomain == "unsupported" {
			return fmt.Sprintf("IPv4-only for %s", currentDomain.Site), nil
		}
		if newDomain.BaseDomain == "no_record" {
			return fmt.Sprintf("No DNS records found for %s", currentDomain.Site), nil
		}
	}
	// WWW Domain
	if currentDomain.WwwDomain != newDomain.WwwDomain {
		if currentDomain.WwwDomain == "unsupported" && newDomain.WwwDomain == "supported" {
			return fmt.Sprintf("IPv6 enabled for www.%s", currentDomain.Site), nil
		}
		if currentDomain.WwwDomain == "supported" && newDomain.WwwDomain == "unsupported" {
			return fmt.Sprintf("IPv6 lost for www.%s", currentDomain.Site), nil
		}
		if currentDomain.WwwDomain == "no_record" && newDomain.WwwDomain == "supported" {
			return fmt.Sprintf("IPv6 enabled for www.%s", currentDomain.Site), nil
		}
		if currentDomain.WwwDomain == "no_record" && newDomain.WwwDomain == "unsupported" {
			return fmt.Sprintf("IPv4-only for www.%s", currentDomain.Site), nil
		}
		if newDomain.WwwDomain == "no_record" {
			return fmt.Sprintf("No DNS records found for www.%s", currentDomain.Site), nil
		}
	}

	// Nameserver
	if currentDomain.Nameserver != newDomain.Nameserver {
		if currentDomain.Nameserver == "unsupported" && newDomain.Nameserver == "supported" {
			return fmt.Sprintf("IPv6 enabled nameserver for %s", currentDomain.Site), nil
		}
		if currentDomain.Nameserver == "supported" && newDomain.Nameserver == "unsupported" {
			return fmt.Sprintf("Nameservers degraded to IPv4-only for %s", currentDomain.Site), nil
		}
		if currentDomain.Nameserver == "no_record" && newDomain.Nameserver == "supported" {
			return fmt.Sprintf("IPv6 enabled nameserver for %s", currentDomain.Site), nil
		}
		if currentDomain.Nameserver == "no_record" && newDomain.Nameserver == "unsupported" {
			return fmt.Sprintf("IPv4-only nameservers for %s", currentDomain.Site), nil
		}
		if newDomain.Nameserver == "no_record" {
			return fmt.Sprintf("No NS records found for %s", currentDomain.Site), nil
		}
	}

	// MX Record
	if currentDomain.MXRecord != newDomain.MXRecord {
		if currentDomain.MXRecord == "unsupported" && newDomain.MXRecord == "supported" {
			return fmt.Sprintf("IPv6 enabled MX records for %s", currentDomain.Site), nil
		}
		if currentDomain.MXRecord == "supported" && newDomain.MXRecord == "unsupported" {
			return fmt.Sprintf("MX records degraded to IPv4-only for %s", currentDomain.Site), nil
		}
		if currentDomain.MXRecord == "no_record" && newDomain.MXRecord == "supported" {
			return fmt.Sprintf("IPv6 enabled MX records for %s", currentDomain.Site), nil
		}
		if currentDomain.MXRecord == "no_record" && newDomain.MXRecord == "unsupported" {
			return fmt.Sprintf("IPv4-only MX records for %s", currentDomain.Site), nil
		}
		if newDomain.MXRecord == "no_record" {
			return fmt.Sprintf("No Mail records found for %s", currentDomain.Site), nil
		}
	}

	// No change
	return "", errors.New(
		"Unknown change for " + currentDomain.Site + ": BaseDomain: [" + currentDomain.BaseDomain + " - " + newDomain.BaseDomain + "] WwwDomain: [" + currentDomain.WwwDomain + " - " + newDomain.WwwDomain + "] Nameserver: [" + currentDomain.Nameserver + " - " + newDomain.Nameserver + "] MXRecord: [" + currentDomain.MXRecord + " - " + newDomain.MXRecord + "]",
	)
}
