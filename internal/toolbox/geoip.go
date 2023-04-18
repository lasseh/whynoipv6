package toolbox

import (
	"errors"
	"fmt"
	"log"
	"net"
	"regexp"
	"strings"

	"github.com/IncSW/geoip2"
	"github.com/miekg/dns"
)

// Asn represents an Autonomous System Number (ASN) record.
type Asn struct {
	Number int32
	Name   string
}

// ASNInfo retrieves the ASN information for a given domain.
func (s *Service) ASNInfo(domain string) (Asn, error) {
	var err error
	domain, err = IDNADomain(domain)
	if err != nil {
		return Asn{}, err
	}

	// Attempt to retrieve the A record for the domain (this will fail for IPv6-only domains).
	result, err := s.localQuery(domain, dns.TypeA)
	if err != nil {
		log.Printf("[%s] [ASNInfo] No A record for domain. Error: %s", domain, err)
	}

	// If the A record is found, iterate through the records and look up the ASN information.
	if err == nil {
		for _, r := range result.Answer {
			switch r.Header().Rrtype {
			case dns.TypeA:
				ip := r.(*dns.A).A.String()
				// log.Printf("[%s] Checking ASN for IP: %s", domain, ip)
				geo, err := s.IPtoASN(ip)
				if err != nil {
					// fmt.Println(err)
					continue
				}
				if geo != (Asn{}) {
					return geo, nil
				}
			// If A record, lookup the ASN
			case dns.TypeAAAA:
				ip := r.(*dns.AAAA).AAAA.String()
				// log.Printf("[%s] Checking ASN for IP: %s", domain, ip)
				geo, err := s.IPtoASN(ip)
				if err != nil {
					// fmt.Println(err)
					continue
				}
				if geo != (Asn{}) {
					return geo, nil
				}
			case dns.TypeCNAME:
				cname := r.(*dns.CNAME).Target
				// log.Printf("[%s] Checking ASN for CNAME: %s", domain, cname)
				return s.ASNInfo(cname)
			}
		}
	}
	return Asn{}, nil
}

// IPtoASN retrieves the ASN information for a given IP address from the GeoLite2 database.
func (s *Service) IPtoASN(ip string) (Asn, error) {
	reader, err := geoip2.NewASNReaderFromFile(s.GeoDB + "GeoLite2-ASN.mmdb")
	if err != nil {
		return Asn{}, err
	}

	record, err := reader.Lookup(net.ParseIP(ip))
	if err != nil {
		return Asn{}, err
	}

	return Asn{
		Number: int32(record.AutonomousSystemNumber),
		Name:   record.AutonomousSystemOrganization,
	}, nil
}

// CountryCode Section

// GetTLDFromDomain extracts the Top-Level Domain (TLD) from a given domain.
func (s *Service) GetTLDFromDomain(domain string) (string, error) {
	re := regexp.MustCompile(`(?i)([a-z0-9-]+)\.([a-z]{2,})$`)
	match := re.FindStringSubmatch(domain)
	if match == nil {
		return "", errors.New("No match found")
	}
	return strings.ToUpper(match[2]), nil
}

// CountryCode retrieves the country code for a given domain by looking up the A records and their corresponding IP addresses.
func (s *Service) CountryCode(domain string) (string, error) {
	q := QueryResult{}

	var err error
	domain, err = IDNADomain(domain)
	if err != nil {
		return "", err
	}

	result, err := s.localQuery(domain, dns.TypeA)
	if err != nil {
		log.Printf("[%s] [CountryCode] No A record for domain. Error: %s", domain, err)
		return "", err
	}
	// Check for domain error
	if result.Rcode != dns.RcodeSuccess {
		q.Rcode = result.Rcode
		return "", fmt.Errorf("Rcode: %s", dns.RcodeToString[result.Rcode])
	}

	// Loop over result and check country code for each IP address
	for _, r := range result.Answer {
		switch r.Header().Rrtype {
		case dns.TypeA:
			ip := r.(*dns.A).A.String()
			// log.Printf("[%s] Checking Country Code for IP: %s", domain, ip)
			cc, err := s.geoCountryCode(ip)
			if err != nil {
				// fmt.Println(err)
				continue
			}
			if cc != "" {
				return cc, nil
			}
		}
	}

	return "", nil
}

// geoCountryCode retrieves the country code for a given IP address from the GeoLite2 database.
func (s *Service) geoCountryCode(ip string) (string, error) {
	reader, err := geoip2.NewCountryReaderFromFile(s.GeoDB + "GeoLite2-Country.mmdb")
	if err != nil {
		return "", err
	}
	record, err := reader.Lookup(net.ParseIP(ip))
	if err != nil {
		return "", errors.New("No Country Code found for IP: " + ip)
	}

	return record.Country.ISOCode, nil
}
