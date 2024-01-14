package geoip

import (
	"errors"
	"net"
	"regexp"
	"strings"
	"sync"

	"github.com/IncSW/geoip2"
)

var (
	// Global database readers.
	asnDB     *geoip2.ASNReader
	countryDB *geoip2.CountryReader

	// Mutex for thread-safe initialization of the database readers.
	dbInitOnce sync.Once

	// Predefined error messages.
	errInvalidIP   = errors.New("invalid IP address")
	errNoInfoFound = errors.New("no information found")
	// errDBInitFailed = errors.New("database initialization failed")

	// Compiled regular expression for TLD extraction.
	tldRegex = regexp.MustCompile(`(?i)([a-z0-9-]+)\.([a-z]{2,})$`)
)

// Initialize initializes the database readers for ASN and country lookup.
// asnDBPath and countryDBPath are file paths to the respective database files.
// It should be called before using the package for bulk operations.
func Initialize(dbPath string) error {
	var initErr error // Variable to hold initialization error

	dbInitOnce.Do(func() {
		asnDB, initErr = geoip2.NewASNReaderFromFile(dbPath + "GeoLite2-ASN.mmdb")
		if initErr != nil {
			return
		}

		countryDB, initErr = geoip2.NewCountryReaderFromFile(dbPath + "GeoLite2-Country.mmdb")
		if initErr != nil {
			return
		}
	})

	return initErr // Return the error encountered during initialization, if any
}

// CloseDBs closes the database readers.
// It should be called after the bulk operations are done to free up resources.
func CloseDBs() {
	if asnDB != nil {
		// The geoip2 library does not provide a Close() method for the readers.
		// asnDB.Close()
		asnDB = nil
	}
	if countryDB != nil {
		// The geoip2 library does not provide a Close() method for the readers.
		// countryDB.Close()
		countryDB = nil
	}
}

// validateIP checks if the given IP address is valid.
func validateIP(ip string) error {
	if net.ParseIP(ip) == nil {
		return errInvalidIP
	}
	return nil
}

// Asn represents an Autonomous System Number (ASN) record.
type Asn struct {
	Number int32
	Name   string
}

// AsnLookup retrieves the ASN information for a given IP address.
// It returns an Asn struct and an error, if any.
func AsnLookup(ip string) (Asn, error) {
	if err := validateIP(ip); err != nil {
		return Asn{}, err
	}

	record, err := asnDB.Lookup(net.ParseIP(ip))
	if err != nil || record == nil {
		return Asn{}, errNoInfoFound
	}

	return Asn{
		Number: int32(record.AutonomousSystemNumber),
		Name:   record.AutonomousSystemOrganization,
	}, nil
}

// CountryLookup retrieves the country information for a given IP address.
// It returns the country ISO code and an error, if any.
func CountryLookup(ip string) (string, error) {
	if err := validateIP(ip); err != nil {
		return "", err
	}

	record, err := countryDB.Lookup(net.ParseIP(ip))
	if err != nil || record == nil {
		return "", errNoInfoFound
	}

	return record.Country.ISOCode, nil
}

// ExtractTLDFromDomain extracts the Top-Level Domain (TLD) from a given domain.
// It uses a regular expression to match the TLD pattern.
// If a match is found, it returns the TLD in uppercase.
// If no match is found, it returns an error.
func ExtractTLDFromDomain(domain string) (string, error) {
	match := tldRegex.FindStringSubmatch(domain)
	if len(match) < 3 {
		return "", errors.New("no match found")
	}
	return strings.ToUpper(match[2]), nil
}
