package toolbox

// Toolbox is a collection of tools used by v6manage
import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"regexp"
	"time"

	"github.com/miekg/dns"
	"golang.org/x/net/idna"
)

const wwwPrefix = "www."

// Service is a service for managing v6 tools
type Service struct {
	GeoDB      string
	Nameserver string
}

// NewToolboxService represents a toolbox entry.
func NewToolboxService(gdb, ns string) *Service {
	return &Service{
		GeoDB:      gdb,
		Nameserver: ns,
	}
}

// CheckTLD checks if a domain has an AAAA record (IPv6) and returns a QueryResult.
// The QueryResult.IPv6 field is set to true on the first IPv6 record found.
func (s *Service) CheckTLD(domain string) (QueryResult, error) {
	queryResult := QueryResult{}

	// Convert the domain to its IDNA form.
	var err error
	domain, err = IDNADomain(domain)
	if err != nil {
		return queryResult, err
	}

	// Perform a local DNS query for AAAA records.
	result, err := s.localQuery(domain, dns.TypeAAAA)
	if err != nil {
		return QueryResult{}, err
	}

	// Check for domain error.
	if result.Rcode != dns.RcodeSuccess {
		queryResult.Rcode = result.Rcode
		return queryResult, fmt.Errorf("Rcode: %s", dns.RcodeToString[result.Rcode])
	}

	// Check if the result contains an IPv6 address.
	for _, record := range result.Answer {
		switch r := record.(type) {
		case *dns.AAAA:
			queryResult.IPv6 = true
			return queryResult, nil
		case *dns.CNAME:
			// Perform a recursive check for the CNAME target.
			cnameResult, _ := s.CheckTLD(r.Target)
			if cnameResult.IPv6 {
				queryResult.IPv6 = true
				return queryResult, nil
			}
		}
	}

	// No IPv6 record found.
	return queryResult, nil
}

// CheckNS checks if a domain has an AAAA (IPv6) record in the NS records and returns a QueryResult.
// The QueryResult.IPv6 field is set to true on the first IPv6 record found.
func (s *Service) CheckNS(domain string) (QueryResult, error) {
	queryResult := QueryResult{}

	// Convert the domain to its IDNA form.
	var err error
	domain, err = IDNADomain(domain)
	if err != nil {
		return queryResult, err
	}

	// Perform a local DNS query for NS records.
	result, err := s.localQuery(domain, dns.TypeNS)
	if err != nil {
		return QueryResult{}, err
	}

	// Check for domain error.
	if result.Rcode != dns.RcodeSuccess {
		queryResult.Rcode = result.Rcode
		return queryResult, fmt.Errorf("Rcode: %s", dns.RcodeToString[result.Rcode])
	}

	// Loop over NameServers and check if any of them has IPv6.
	for _, nsRecord := range result.Answer {
		switch ns := nsRecord.(type) {
		case *dns.CNAME:
			// If the response is a CNAME, follow it and check.
			cnameResult, _ := s.CheckTLD(ns.Target)
			if cnameResult.IPv6 {
				queryResult.IPv6 = true
			}
		case *dns.NS:
			nsResult, err := s.CheckTLD(ns.Ns)
			if err != nil {
				// Ignore errors caused by creative DNS server setups.
				// We only care if at least one DNS server has IPv6 enabled!
			}
			// Return true on the first IPv6-enabled nameserver.
			if nsResult.IPv6 {
				queryResult.IPv6 = true
				return queryResult, nil
			}
		}
	}

	// At this point we have processed all NS records.
	// If queryResult.IPv6 is true, then we have found at least one IPv6 record.
	return queryResult, nil
}

// ValidateDomain checks if the domain has enough DNS information to proceed with the checks.
// Returns an error if no records are found for either domain.com or www.domain.com.
func (s *Service) ValidateDomain(domain string) error {
	// Convert the domain to its IDNA form.
	var err error
	domain, err = IDNADomain(domain)
	if err != nil {
		return err
	}

	// Check if domain has any DNS records, else disable it before performing any checks
	resultNS, err := s.localQuery(domain, dns.TypeTXT)
	if err != nil || resultNS.Rcode != dns.RcodeSuccess {
		// return fmt.Errorf("%s", dns.RcodeToString[resultNS.Rcode])
		// return fmt.Errorf("Failed to get nameserver for %s: %v", domain, err)
		return fmt.Errorf("NXDOMAIN")
	}

	// Check for A and AAAA record (IPv4 and IPv6).
	isDomainError := s.queryDNSRecord(domain, dns.TypeA) && s.queryDNSRecord(domain, dns.TypeAAAA)

	// Check for www A and www AAAA record (IPv4 and IPv6).
	isWwwDomainError := s.queryDNSRecord(wwwPrefix+domain, dns.TypeA) && s.queryDNSRecord(wwwPrefix+domain, dns.TypeAAAA)

	// Return an error if both A and www A records are not found.
	if isDomainError && isWwwDomainError {
		// return fmt.Errorf("No A or AAAA record found for %s or %s", domain, wwwPrefix + domain)
		return fmt.Errorf("No DNS record for %s: %v", domain, err)
	}

	return nil
}

// queryDNSRecord checks for the DNS record type of the given domain.
// Returns true if there is an error or the DNS record is not found.
func (s *Service) queryDNSRecord(domain string, recordType uint16) bool {
	result, err := s.localQuery(domain, recordType)
	return err != nil || result.Rcode != dns.RcodeSuccess
}

// PercentOf calculates the percentage of 'current' relative to 'all'.
func (s *Service) PercentOf(current int, all int) float64 {
	percentage := (float64(current) * float64(100)) / float64(all)
	return percentage
}

// IDNADomain returns the domain name in its internationalized (Punycode) format.
// If it is already in the internationalized format, it returns the same value.
func IDNADomain(domain string) (string, error) {
	asciiDomain, err := idna.ToASCII(domain)
	if err != nil {
		return "", err
	}
	return asciiDomain, nil
}

// IsDomainAccessibleOverIPv6 checks if a domain is accessible over IPv6 only.
func (s *Service) IsDomainAccessibleOverIPv6(domain string) (bool, error) {
	// Validate the domain before attempting to make a connection.
	if !isValidDomain(domain) {
		return false, fmt.Errorf("invalid domain: %s", domain)
	}

	// Create a HTTP client that supports IPv6 connections.
	ipv6HttpClient := http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
				dialer := net.Dialer{}
				// Split the host and port, then force an IPv6 connection by enclosing the host in square brackets.
				host, port, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, err
				}
				return dialer.DialContext(ctx, "tcp6", fmt.Sprintf("[%s]:%s", host, port))
			},
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}

	// Perform an HTTP GET request using the IPv6-only client.
	response, err := ipv6HttpClient.Get("https://" + domain)
	if err != nil {
		return false, err
	}

	// Ensure response body is closed after function ends.
	defer func() {
		if err := response.Body.Close(); err != nil {
			log.Printf("Error closing response body: %v", err)
		}
	}()

	// Check if the domain is accessible over IPv6.
	if response.StatusCode != http.StatusOK {
		return false, fmt.Errorf("%s is not accessible over IPv6, HTTP status code: %d", domain, response.StatusCode)
	}

	return true, nil
}

// isValidDomain checks if the input string is a valid domain name according to RFC 1035.
func isValidDomain(domain string) bool {
	// RFC 1035
	// <domain> ::= <part> | <part> "." <domain>
	// <part> ::= <letter> [ [ <ldh-str> ] <let-dig> ]
	// <ldh-str> ::= <let-dig-hyp> | <let-dig-hyp> <ldh-str>
	// <let-dig-hyp> ::= <let-dig> | "-"
	// <let-dig> ::= <letter> | <digit>
	// <letter> ::= any one of the 52 alphabetic characters A through Z in upper case and a through z in lower case
	// <digit> ::= any one of the ten digits 0 through 9
	// Note: the above is simplified and does not account for a leading digit or a trailing hyphen.

	// Define the regular expression
	var domainRegexp = regexp.MustCompile(`^(?i)[a-z0-9]+([-.]{1}[a-z0-9]+)*\.[a-z]{2,6}$`)

	// Check if the domain matches the regex
	return domainRegexp.MatchString(domain)
}
