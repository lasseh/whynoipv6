package toolbox

// Toolbox is a collection of tools used by v6manage
import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/miekg/dns"
	"golang.org/x/net/idna"
)

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
				return queryResult, nil
			}
			return queryResult, nil
		case *dns.NS:
			nsResult, err := s.CheckTLD(ns.Ns)
			if err != nil {
				// Ignore errors caused by creative DNS server setups.
				// We only care if at least one DNS server has IPv6 enabled!
				// TODO: We should care about this. The only correct way is that all NS servers for a domain have IPv6 enabled.
				return queryResult, err
			}
			// Return true on the first IPv6-enabled nameserver.
			if nsResult.IPv6 {
				queryResult.IPv6 = true
				return queryResult, nil
			}
		}
	}

	// No IPv6 record found.
	return queryResult, nil
}

// ValidateDomain checks if the domain has enough DNS information to proceed with the checks.
// Returns an error if no records are found for either domain.com or www.domain.com.
func (s *Service) ValidateDomain(domain string) error {
	// Check for lookup errors.
	var aError, wwwError bool

	// Convert the domain to its IDNA form.
	var err error
	domain, err = IDNADomain(domain)
	if err != nil {
		return err
	}

	// Check nameserver.
	resultNS, err := s.localQuery(domain, dns.TypeTXT)
	if err != nil {
		// No name server to answer the question.
		return err
	}

	if resultNS.Rcode != dns.RcodeSuccess {
		// If we get an error here, disable the domain.
		return fmt.Errorf("%s", dns.RcodeToString[resultNS.Rcode])
	}

	// Check for A record (note: this will fail on an IPv6-only domain).
	result, err := s.localQuery(domain, dns.TypeA)
	aError = err != nil || result.Rcode != dns.RcodeSuccess

	// Check for www A record (note: this will fail on an IPv6-only domain).
	resultWWW, err := s.localQuery("www."+domain, dns.TypeA)
	wwwError = err != nil || resultWWW.Rcode != dns.RcodeSuccess

	// Return an error if both A and www A records are not found.
	if aError && wwwError {
		return fmt.Errorf("No DNS record for %s", domain)
	}

	return nil
}

// CheckCurl checks if a domain is accessible over IPv6 only.
// TODO: Check if this works, it's not used yet.
func (s *Service) CheckCurl(domain string) (bool, error) {
	ipv6Client := http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
				dialer := net.Dialer{}
				// Split the host and port and force an IPv6 connection by enclosing the host in square brackets.
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
	response, err := ipv6Client.Get("https://" + domain)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()

	// Check if the domain is accessible over IPv6.
	if response.StatusCode == http.StatusOK {
		return true, nil
	}

	return false, nil
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
