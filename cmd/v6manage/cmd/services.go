package cmd

import (
	"fmt"
	"time"

	"whynoipv6/internal/core"
	"whynoipv6/internal/logger"
)

var (
	// Global services
	campaignService  core.CampaignService
	changelogService core.ChangelogService
	domainService    core.DomainService
	countryService   core.CountryService
	asnService       core.ASNService
	metricService    core.MetricService
	logg             = logger.GetLogger() // Global logger
	// toolboxService   toolbox.Service
	// statService      core.StatService
	// resolver         *toolbox.Resolver
)

// prettyDuration converts a time.Duration value into a human-readable format
// by rounding it to the nearest second and formatting it as "HH:mm:ss".
// Sorry i dont know where to put this :(
func prettyDuration(d time.Duration) string {
	// Round the duration to the nearest second to avoid fractional seconds.
	d = d.Round(time.Second)

	// Extract the number of hours, and subtract them from the total duration.
	hours := int(d.Hours())
	d -= time.Duration(hours) * time.Hour

	// Extract the number of minutes, and subtract them from the remaining duration.
	minutes := int(d.Minutes())
	d -= time.Duration(minutes) * time.Minute

	// Extract the number of seconds from the remaining duration.
	seconds := int(d.Seconds())

	// Format the hours, minutes, and seconds as a string in the "HH:mm:ss" format.
	return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
}
