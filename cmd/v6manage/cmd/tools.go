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

// prettyDuration converts a time.Duration value into a human-readable format
// by rounding it to the nearest second and formatting it as "HH:mm:ss".
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

// getASNInfo retrieves ASN information for a given domain using the toolboxService.
// If the ASN is not present in the database, it creates a new entry and returns the ASN ID.
func getASNInfo(domain string) (int64, error) {
	ctx := context.Background()

	domain, err = toolbox.IDNADomain(domain)
	if err != nil {
		return 0, err
	}

	// Retrieve ASN information for the domain using the toolboxService.
	asn, err := toolboxService.ASNInfo(domain)
	if err != nil {
		log.Printf("[%s] GeoASNInfo Error: %s", domain, err)
	}
	if verbose {
		log.Printf("[%s] ASN: '%s' (AS%d)", domain, asn.Name, asn.Number)
	}

	// If a valid ASN number is found, check if it exists in the database.
	if asn.Number != 0 {
		dbAsn, err := asnService.GetASByNumber(ctx, asn.Number)
		if err != nil && err != pgx.ErrNoRows {
			log.Printf("[%s] GetASByNumber Error: %s\n", domain, err)
		}

		// If the ASN is not present in the database, create a new entry.
		if err == pgx.ErrNoRows {
			if verbose {
				log.Printf("[%s] AS%d does not exist in database", domain, asn.Number)
			}

			newAsn, err := asnService.CreateAsn(ctx, asn.Number, asn.Name)
			if err != nil {
				log.Printf("[%s] CreateAsn Error: %s\n", domain, err)
			}
			if verbose {
				log.Printf("[%s] Added %s(AS%d) to database\n", domain, newAsn.Name, newAsn.Number)
			}
			dbAsn = newAsn
		}

		// Return ASN ID for the found or created ASN.
		return dbAsn.ID, nil
	}

	// If no ASN is found, return a default ASN ID (1 - Unknown) as a fallback.
	return 1, nil
}

// getCountryInfo retrieves country information for a given domain using the toolboxService.
// It returns the country ID associated with the domain's TLD or Geo Country Code.
func getCountryInfo(domain string) (int64, error) {
	ctx := context.Background()

	// Extract the TLD from the domain.
	tld, err := toolboxService.GetTLDFromDomain(domain)
	if err != nil {
		log.Printf("[%s] GetTLDFromDomain Error: %s\n", domain, err)
		return 251, nil // See the fallback explanation below.
	}

	// If the TLD is empty, return a default country ID (251 - Unknown).
	if tld == "" {
		log.Printf("[%s] TLD is empty: %s\n", domain, tld)
		return 251, nil // See the fallback explanation below.
	}

	// Check if the TLD is country-bound in the database.
	// Ignore if no mapping is found.
	dbTld, _ := countryService.GetCountryTld(ctx, fmt.Sprintf(".%s", tld))

	// Return the country ID if a mapping is found in the database.
	if dbTld != (core.CountryModel{}) {
		if verbose {
			log.Printf("[%s] TLD is bound to country: %s", domain, dbTld.CountryTld)
		}
		return dbTld.ID, nil
	}

	// TODO: Check if the domain is hosted on a CDN, if so, map it to a new category, CDN.

	// If no TLD mapping is found, check the Geo Database for the country code.
	geoCc, err := toolboxService.CountryCode(domain)
	if err != nil {
		log.Printf("[%s] CountryCode Error: %s", domain, err)
	}
	if len(geoCc) == 0 {
		if verbose {
			log.Printf("[%s] GEO CountryCode is empty, setting to Unknown", domain)
		}
		return 251, nil // See the fallback explanation below.
	}
	if verbose {
		log.Printf("[%s] GEO CountryCode is %s", domain, geoCc)
	}

	// Check if the Geo Country Code exists in the database.
	dbTld, _ = countryService.GetCountryTld(ctx, fmt.Sprintf(".%s", geoCc))

	// Return the country ID if a mapping is found in the database.
	if dbTld != (core.CountryModel{}) {
		if verbose {
			log.Printf("[%s] Domain is GEO mapped to %s", domain, dbTld.Country)
		}
		return dbTld.ID, nil
	}

	// Fallback: if no country code is found, return a default country ID (251 - Unknown).
	return 251, nil
}
