package cmd

import (
	"context"
	"fmt"
	"log"
	"time"
	"whynoipv6/internal/core"
	"whynoipv6/internal/toolbox"

	"github.com/jackc/pgx/v4"
)

func prettyDuration(d time.Duration) string {
	d = d.Round(time.Second)

	hours := int(d.Hours())
	d -= time.Duration(hours) * time.Hour

	minutes := int(d.Minutes())
	d -= time.Duration(minutes) * time.Minute

	seconds := int(d.Seconds())

	return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
}

// getASNInfo gets ASN info from geoip
func getASNInfo(domain string) (int64, error) {
	ctx := context.Background()

	domain, err = toolbox.IDNADomain(domain)
	if err != nil {
		return 0, err
	}

	// Get ASN for domain
	asn, err := toolboxService.ASNInfo(domain)
	if err != nil {
		log.Printf("[%s] GeoASNInfo Error: %s", domain, err)
	}
	if verbose {
		log.Printf("[%s] ASN: '%s' (AS%d)", domain, asn.Name, asn.Number)
	}

	// Check if ASN exists in database
	if asn.Number != 0 {
		dbasn, err := asnService.GetASByNumber(ctx, asn.Number)
		if err != nil && err != pgx.ErrNoRows {
			log.Printf("[%s] GetASByNumber Error: %s\n", domain, err)
		}
		if err == pgx.ErrNoRows {
			if verbose {
				log.Printf("[%s] AS%d does not exist in database", domain, asn.Number)
			}

			// Create ASN in db
			newAsn, err := asnService.CreateAsn(ctx, asn.Number, asn.Name)
			if err != nil {
				log.Printf("[%s] CreateAsn Error: %s\n", domain, err)
			}
			if verbose {
				log.Printf("[%s] Added %s(AS%d) to database\n", domain, newAsn.Name, newAsn.Number)
			}
			dbasn = newAsn
		}

		// Compare ASN and changelog it if needed
		// This is super spammy because of anycast, so disabled for now
		if asn.Name != dbasn.Name {
			if verbose {
				log.Printf("[%s] AS changed from '%s' TO '%s'", domain, asn.Name, dbasn.Name)
			}
			// _, err := changelogService.Create(context.Background(), core.ChangelogModel{
			// 	DomainID: int32(domain.ID),
			// 	Message:  fmt.Sprintf("AS changed from: '%s' To: '%s'", domain.AsName, dbasn.Name),
			// })
			// if err != nil {
			// 	log.Printf("[%s] Error writing changelog: %s\n", domain.Site, err)
			// }
		}
		// Return ASN ID
		return dbasn.ID, nil
	}

	// Ugly hack, i'm sorry!
	// If we dont find asn, static map it to AsnID 1 - Unknown
	return 1, nil
}

func getCountryInfo(domain string) (int64, error) {
	ctx := context.Background()

	// Regex out TLD from Domain
	tld, err := toolboxService.GetTLDFromDomain(domain)
	if err != nil {
		log.Printf("[%s] GetTLDFromDomain Error: %s\n", domain, err)
		return 251, nil // See uglu hack below
	}

	// Check if tld is empty
	if tld == "" {
		log.Printf("[%s] TLD is empty: %s\n", domain, tld)
		return 251, nil // See uglu hack below
	}

	// Check if TLD is a Country bound TLD
	// Ignore if we dont find a mapping
	dbtld, _ := countryService.GetCountryTld(ctx, fmt.Sprintf(".%s", tld))

	// Return Country ID if we found a mapping in database
	if dbtld != (core.CountryModel{}) {
		if verbose {
			log.Printf("[%s] TLD is bound to country: %s", domain, dbtld.CountryTld)
		}
		return dbtld.ID, nil
	}

	// TODO: Check if domain is hosted on a CDN, if so, map it to a new catagory, CDN

	// Since we did not find a mapping to country, check Geo Database
	geocc, err := toolboxService.CountryCode(domain)
	if err != nil {
		log.Printf("[%s] CountryCode Error: %s", domain, err)
	}
	if len(geocc) == 0 {
		if verbose {
			log.Printf("[%s] GEO CountryCode is empty, setting to Unknown", domain)
		}
		return 251, nil // See uglu hack below
	}
	if verbose {
		log.Printf("[%s] GEO CountryCode is %s", domain, geocc)
	}

	// Check if Geo Country Code exists in DB
	dbtld, _ = countryService.GetCountryTld(ctx, fmt.Sprintf(".%s", geocc))

	// Return Country ID if we found a mapping in database
	if dbtld != (core.CountryModel{}) {
		if verbose {
			log.Printf("[%s] Domain is GEO mapped to %s", domain, dbtld.Country)
		}
		return dbtld.ID, nil
	}

	// Ugly hack, i'm sorry!
	// If we dont find country code, static map it to CountryID 251 - Unknown
	return 251, nil
}
