package cmd

import (
	"context"
	"fmt"
	"whynoipv6/internal/core"
	"whynoipv6/internal/geoip"
	"whynoipv6/internal/resolver"

	"github.com/jackc/pgx/v4"
)

// getNetworkProvider retrieves the network provider for a given domain
func getNetworkProvider(ctx context.Context, domain string) (int64, error) {
	logg := logg.With().Str("service", "getNetworkProvider").Logger()
	// Get the domain's IP addresses.
	ip, err := resolver.IPLookup(domain)
	if err != nil {
		logg.Debug().Msgf("[%s] GeoLookup Error: %s", domain, err)
	}
	// logg.Debug().Msgf("[%s] IP: %s", domain, ip)

	// Get the domain's ASN.
	asn, err := geoip.AsnLookup(ip)
	if err != nil {
		logg.Debug().Msgf("[%s] AsnLookup Error: %s", domain, err)
	}
	logg.Debug().Msgf("[%s] ASN: '%s' (AS%d)", domain, asn.Name, asn.Number)

	// If a valid ASN number is found, check if it exists in the database.
	if asn.Number != 0 {
		dbAsn, err := asnService.GetASByNumber(ctx, asn.Number)
		if err != nil && err != pgx.ErrNoRows {
			logg.Debug().Msgf("[%s] GetASByNumber Error: %s\n", domain, err)
		}

		// If the ASN is not present in the database, create a new entry.
		if err == pgx.ErrNoRows {
			if verbose {
				logg.Debug().Msgf("[%s] AS%d does not exist in database", domain, asn.Number)
			}

			newAsn, err := asnService.CreateAsn(ctx, asn.Number, asn.Name)
			if err != nil {
				logg.Debug().Msgf("[%s] CreateAsn Error (AS: %s): %s\n", domain, asn.Name, err)
				return 1, nil
			}
			logg.Debug().Msgf("[%s] Added %s(AS%d) to database", domain, newAsn.Name, newAsn.Number)
			dbAsn = newAsn
		}

		// Return ASN ID for the found or created ASN.
		return dbAsn.ID, nil
	}

	// If no ASN is found, return a default ASN ID (1 - Unknown) as a fallback.
	return 1, nil
}

func getCountryID(ctx context.Context, domain string) (int64, error) {
	logg := logg.With().Str("service", "getCountryID").Logger()
	// Extract the TLD from the domain.
	tld, err := geoip.ExtractTLDFromDomain(domain)
	if err != nil {
		logg.Debug().Msgf("[%s] ExtractTLDFromDomain Error: %s\n", domain, err)
		return 251, nil // See the fallback explanation below.
	}

	// If the TLD is empty, return a default country ID (251 - Unknown).
	if tld == "" {
		logg.Debug().Msgf("[%s] TLD is empty: %s\n", domain, tld)
		return 251, nil // See the fallback explanation below.
	}

	// Check if the TLD is country-bound in the database.
	// Ignore if no mapping is found.
	dbTld, err := countryService.GetCountryTld(ctx, fmt.Sprintf(".%s", tld))
	if err != nil && err != pgx.ErrNoRows {
		logg.Debug().Msgf("[%s] GetCountryTld Error: %s\n", domain, err)
	}

	// Return the country ID if a mapping is found in the database.
	if dbTld != (core.CountryModel{}) {
		logg.Debug().Msgf("[%s] Domain is TLD-bound to: %s", domain, dbTld.CountryTld)
		return dbTld.ID, nil
	}

	// If no TLD mapping is found, check the Geo Database for the country code.

	// Get the domains IP.
	ip, err := resolver.IPLookup(domain)
	if err != nil {
		logg.Debug().Msgf("[%s] IPLookup Error: %s", domain, err)
	}
	geoCountryCode, err := geoip.CountryLookup(ip)
	if err != nil {
		logg.Debug().Msgf("[%s] CountryLookup Error: %s", domain, err)
	}
	logg.Debug().Msgf("[%s] GEO CountryCode is %s", domain, geoCountryCode)

	// Check if the Geo Country Code exists in the database.
	dbTld, err = countryService.GetCountryTld(ctx, fmt.Sprintf(".%s", geoCountryCode))
	if err != nil && err != pgx.ErrNoRows {
		logg.Debug().Msgf("[%s] GetCountryTld Error: %s\n", domain, err)
	}

	// Return the country ID if a mapping is found in the database.
	if dbTld != (core.CountryModel{}) {
		logg.Debug().Msgf("[%s] Domain is GEO mapped to %s", domain, dbTld.Country)
		return dbTld.ID, nil
	}

	// Fallback: if no country code is found, return a default country ID (251 - Unknown).
	return 251, nil
}
