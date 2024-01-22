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

	"github.com/miekg/dns"
	"github.com/spf13/cobra"
)

// crawlCmd represents the crawl command
var crawlCmd = &cobra.Command{
	Use:   "crawl",
	Short: "Crawls the campaign sites in the database",
	Long:  "Crawls the campagin sites in the database",
	Run: func(cmd *cobra.Command, args []string) {
		changelogService = *core.NewChangelogService(db)
		domainService = *core.NewDomainService(db)
		countryService = *core.NewCountryService(db)
		asnService = *core.NewASNService(db)
		metricService = *core.NewMetricService(db)
		domainCrawl()
	},
}

func init() {
	rootCmd.AddCommand(crawlCmd)
}

// domainCrawl crawls the domains in the database
func domainCrawl() {
	ctx := context.Background()
	logg := logg.With().Str("service", "domainCrawl").Logger()

	// Initialize the geoip database.
	err := geoip.Initialize(cfg.GeoIPPath)
	if err != nil {
		logg.Error().Err(err).Msg("Could not initialize geoip database")
		return
	}

	const (
		numWorkers = 10              // Number of workers
		jobTimeout = 2 * time.Minute // Timeout for each batch of jobs
	)

	// Run the crawler indefinitely.
	for {
		t := time.Now()
		logg.Info().Msg("Starting crawl at " + t.Format("2006-01-02 15:04:05"))

		var offset int64 = 0        // Offset for the database query
		const limit int64 = 200     // Limit for the database query
		var totalDomains int        // Total number of domains checked in this crawl
		var totalSuccessfulJobs int // Total number of successful jobs
		var totalFailedJobs int     // Total number of failed jobs

		// Inner loop
		for {
			var domainJobs int                         // Number of domains updated in this batch
			successfulJobs := 0                        // Successful jobs in this batch
			failedJobs := 0                            // Failed jobs in this batch
			loopTime := time.Now()                     // Start time for this batch
			jobs := make(chan core.DomainModel, limit) // Channel for jobs
			done := make(chan bool, limit)             // Channel for signaling completion of jobs

			// Start workers for this batch of jobs
			for w := 1; w <= numWorkers; w++ {
				go processDomain(ctx, jobs, done)
			}

			// Get domains to check
			domains, err := domainService.CrawlDomain(ctx, offset, limit)
			if err != nil {
				logg.Error().Err(err).Msg("Could not get domains to check")
				return
			}

			// Break out of loop if there are no domains left
			if len(domains) == 0 {
				break
			}

			// Send jobs to the workers
			for _, domain := range domains {
				jobs <- domain // Send the job to the jobs channel
				domainJobs++   // Increment the number of domains updated in this batch
				totalDomains++ // Increment the total number of domains checked in this crawl
			}

			close(jobs)                       // Close the jobs channel and wait for this batch of workers to finish
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
			logg.Info().Msgf("Checked %v domains, Successful: %v, Failed: %v Total: %v Duration: %s", domainJobs, successfulJobs, failedJobs, totalDomains, prettyDuration(time.Since(loopTime)))
		}

		// Outer loop finished
		logg.Info().Msgf("Total Domains: %v domains, Successful Jobs: %v, Failed Jobs: %v Duration: %s", totalDomains, totalSuccessfulJobs, totalFailedJobs, prettyDuration(time.Since(t)))

		// Store crawler metrics in the database.
		crawlData := map[string]interface{}{
			"duration": time.Since(t).Seconds(),
			"total":    totalDomains,
			"success":  totalSuccessfulJobs,
			"failed":   totalFailedJobs,
		}
		if err := metricService.StoreMetric(ctx, "crawler", crawlData); err != nil {
			logg.Err(err).Msg("Error storing metric")
		}

		// Collect and store domain statistics.
		stats, err := domainService.CrawlerStats(ctx)
		if err != nil {
			logg.Err(err).Msg("Error getting stats")
		}
		if err := metricService.StoreMetric(ctx, "domains", stats); err != nil {
			logg.Err(err).Msg("Error storing metric")
		}

		// Calculate country stats.
		err = countryService.CalculateCountryStats(ctx)
		if err != nil {
			logg.Err(err).Msg("Error calculating country stats")
		}
		// Calculate ASN stats.
		err = asnService.CalculateASNStats(ctx)
		if err != nil {
			logg.Err(err).Msg("Error calculating ASN stats")
		}

		// Healthcheck reporting
		toolbox.HealthCheckUpdate(cfg.HealthcheckCrawler)
		// Notify partyvan
		toolbox.NotifyIrc(fmt.Sprintf("[WhyNoIPv6] Total Domains: %v, Successful: %v, Failed: %v Duration: %s", totalDomains, totalSuccessfulJobs, totalFailedJobs, prettyDuration(time.Since(t))))

		// Sleep for 2 hours
		logg.Info().Msg("Time until next check: 10 minutes")
		// time.Sleep(2 * time.Hour)
		time.Sleep(10 * time.Minute)
	}

}

// processDomain processes a domain and updates the database
// Returns true if the job was successful, false if it failed
func processDomain(ctx context.Context, jobs <-chan core.DomainModel, done chan<- bool) {
	logg := logg.With().Str("service", "processDomain").Logger()
	for job := range jobs {
		// Process the job
		checkResult, err := checkDomain(ctx, job)
		if err != nil {
			logg.Error().Err(err).Msgf("[%s] Could not check domain", job.Site)
			done <- false // Signal completion indicating failure
			return        // Stop processing this job
		}

		// Update domain
		err = updateDomain(ctx, job, checkResult)
		if err != nil {
			logg.Error().Err(err).Msg("Could not update domain")
			done <- false // Signal completion indicating failure
			return        // Stop processing this job
		}

		done <- true // Signal completion, indicating if it was successful
	}
}

// checkDomain runns all the checks on a domain
func checkDomain(ctx context.Context, domain core.DomainModel) (core.DomainModel, error) {
	checkResult := core.DomainModel{}
	logg := logg.With().Str("service", "checkDomain").Logger()

	// Validate domain
	rcode, err := resolver.ValidateDomain(domain.Site)
	// The return code 1 is a custom code for IDNA error
	if rcode == dns.RcodeNameError || rcode == dns.RcodeServerFailure || rcode == 1 {
		logg.Error().Err(err).Msgf("[%s] Disabling domain", domain.Site)
		// Disable domain
		if disableErr := domainService.DisableDomain(ctx, domain.Site); disableErr != nil {
			logg.Error().Err(disableErr).Msg("Could not disable domain")
		}
		return domain, err
	}
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

func updateDomain(ctx context.Context, currentDomain, newDomain core.DomainModel) error {
	// Helper function to log and create a changelog entry.
	createChangelog := func(domain core.DomainModel, message string, status string) {
		_, err := changelogService.Create(ctx, core.ChangelogModel{
			DomainID:   domain.ID,
			Message:    message,
			IPv6Status: status,
		})
		if err != nil {
			logg.Error().Err(err).Msg("Could not write changelog")
		}
	}

	// Check if there is any changes to the domain.
	if currentDomain.BaseDomain != newDomain.BaseDomain {
		changelog, err := generateChangelog(currentDomain, newDomain)
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
		changelog, err := generateChangelog(currentDomain, newDomain)
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
		changelog, err := generateChangelog(currentDomain, newDomain)
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
		changelog, err := generateChangelog(currentDomain, newDomain)
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
	err := domainService.UpdateDomain(ctx, currentDomain)
	if err != nil {
		logg.Error().Err(err).Msgf("[%s] Could not update domain", currentDomain.Site)
		return err
	}

	return nil
}

// generateChangelog checks the result of the change and generates a changelog entry.
func generateChangelog(currentDomain, newDomain core.DomainModel) (string, error) {
	// Base Domain
	if currentDomain.BaseDomain != newDomain.BaseDomain {
		if currentDomain.BaseDomain == "unsupported" && newDomain.BaseDomain == "supported" {
			return fmt.Sprintf("Got IPv6 record for %s", currentDomain.Site), nil
		}
		if currentDomain.BaseDomain == "supported" && newDomain.BaseDomain == "unsupported" {
			return fmt.Sprintf("Lost IPv6 record for %s", currentDomain.Site), nil
		}
		if currentDomain.BaseDomain == "no_record" && newDomain.BaseDomain == "supported" {
			return fmt.Sprintf("Got IPv6 record for %s", currentDomain.Site), nil
		}
		if currentDomain.BaseDomain == "no_record" && newDomain.BaseDomain == "unsupported" {
			return fmt.Sprintf("Got IPv4 Only record for %s", currentDomain.Site), nil
		}
		if newDomain.BaseDomain == "no_record" {
			return fmt.Sprintf("%s has no record", currentDomain.Site), nil
		}
	}
	// WWW Domain
	if currentDomain.WwwDomain != newDomain.WwwDomain {
		if currentDomain.WwwDomain == "unsupported" && newDomain.WwwDomain == "supported" {
			return fmt.Sprintf("Got IPv6 record for www.%s", currentDomain.Site), nil
		}
		if currentDomain.WwwDomain == "supported" && newDomain.WwwDomain == "unsupported" {
			return fmt.Sprintf("Lost IPv6 record for www.%s", currentDomain.Site), nil
		}
		if currentDomain.WwwDomain == "no_record" && newDomain.WwwDomain == "supported" {
			return fmt.Sprintf("Got IPv6 record for www.%s", currentDomain.Site), nil
		}
		if currentDomain.WwwDomain == "no_record" && newDomain.WwwDomain == "unsupported" {
			return fmt.Sprintf("Got IPv4 Only record for www.%s", currentDomain.Site), nil
		}
		if newDomain.WwwDomain == "no_record" {
			return fmt.Sprintf("www.%s has no record", currentDomain.Site), nil
		}
	}

	// Nameserver
	if currentDomain.Nameserver != newDomain.Nameserver {
		if currentDomain.Nameserver == "unsupported" && newDomain.Nameserver == "supported" {
			return fmt.Sprintf("Nameserver got IPv6 record for %s", currentDomain.Site), nil
		}
		if currentDomain.Nameserver == "supported" && newDomain.Nameserver == "unsupported" {
			return fmt.Sprintf("Nameserver lost IPv6 record for %s", currentDomain.Site), nil
		}
		if currentDomain.Nameserver == "no_record" && newDomain.Nameserver == "supported" {
			return fmt.Sprintf("Nameserver got IPv6 record for %s", currentDomain.Site), nil
		}
		if currentDomain.Nameserver == "no_record" && newDomain.Nameserver == "unsupported" {
			return fmt.Sprintf("Nameserver got IPv4 Only record for %s", currentDomain.Site), nil
		}
		if newDomain.Nameserver == "no_record" {
			return fmt.Sprintf("No NS records for %s", currentDomain.Site), nil
		}
	}

	// MX Record
	if currentDomain.MXRecord != newDomain.MXRecord {
		if currentDomain.MXRecord == "unsupported" && newDomain.MXRecord == "supported" {
			return fmt.Sprintf("MX record got IPv6 record for %s", currentDomain.Site), nil
		}
		if currentDomain.MXRecord == "supported" && newDomain.MXRecord == "unsupported" {
			return fmt.Sprintf("MX record lost IPv6 record for %s", currentDomain.Site), nil
		}
		if currentDomain.MXRecord == "no_record" && newDomain.MXRecord == "supported" {
			return fmt.Sprintf("MX record got IPv6 record for %s", currentDomain.Site), nil
		}
		if currentDomain.MXRecord == "no_record" && newDomain.MXRecord == "unsupported" {
			return fmt.Sprintf("MX record got IPv4 Only record for %s", currentDomain.Site), nil
		}
		if newDomain.MXRecord == "no_record" {
			return fmt.Sprintf("No MX records for %s", currentDomain.Site), nil
		}
	}

	return "", errors.New("Unknown change for " + currentDomain.Site + ": BaseDomain: [" + currentDomain.BaseDomain + " - " + newDomain.BaseDomain + "] WwwDomain: [" + currentDomain.WwwDomain + " - " + newDomain.WwwDomain + "] Nameserver: [" + currentDomain.Nameserver + " - " + newDomain.Nameserver + "] MXRecord: [" + currentDomain.MXRecord + " - " + newDomain.MXRecord + "]")
}

// runHealthcheckReportingJob runs a job that periodically reports the healthcheck status
// func runHealthcheckReportingJob() {
// 	ticker := time.NewTicker(2 * time.Minute)
// 	defer ticker.Stop()

// 	for range ticker.C {
// 		// Notify healthcheck status
// 		logg.Info().Msg("Performing healthcheck reporting...")
// 		toolboxService.HealthCheckUpdate(cfg.HealthcheckCampaign)
// 	}
// }
