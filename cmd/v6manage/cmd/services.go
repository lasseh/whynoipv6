package cmd

import (
	"whynoipv6/internal/core"
	"whynoipv6/internal/toolbox"
)

var (
	// Global services
	campaignService  core.CampaignService
	changelogService core.ChangelogService
	domainService    core.DomainService
	countryService   core.CountryService
	toolboxService   toolbox.Service
	asnService       core.ASNService
	statService      core.StatService
	// resolver         *toolbox.Resolver
	metricService core.MetricService
)
