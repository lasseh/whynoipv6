package resolver

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"whynoipv6/internal/logger"

	"github.com/miekg/dns"
	"golang.org/x/net/idna"
)

// Constants
const (
	IPv6Available  = "supported"
	IPv4Only       = "unsupported"
	NoRecordsFound = "no_record"
	DefaultTimeout = 20 * time.Second
	maxCNAMEHops   = 10
)

var log = logger.GetLogger()

// var nameservers = []string{"1.1.1.1:53", "8.8.8.8:53", "9.9.9.9:53"}
var nameservers = []string{"[2606:4700:4700::1111]:53", "[2606:4700:4700::1001]:53", "1.1.1.1:53", "1.0.0.1:53"}

// DomainResult represents a scan result.
type DomainResult struct {
	BaseDomain string
	WwwDomain  string
	Nameserver string
	MXRecord   string
	v6Only     string
}

// DomainStatus checks the domain's IPv6, NS, and MX records.
func DomainStatus(domain string) (DomainResult, error) {
	c := &dns.Client{Timeout: DefaultTimeout}
	// log := log.With().Str("service", "CheckDomainStatus").Logger()

	// Convert domain to ASCII for DNS lookup
	domain, err := convertToASCII(domain)
	if err != nil {
		return DomainResult{}, fmt.Errorf("IDNA conversion error: %v", err)
	}

	baseDomainStatus, err := checkDomainStatus(domain, c)
	if err != nil {
		return DomainResult{}, err
	}

	WwwDomainStatus, err := checkDomainStatus("www."+domain, c)
	if err != nil {
		return DomainResult{}, err
	}

	nsStatus, mxStatus, err := checkDNSRecords(domain, c)
	if err != nil {
		return DomainResult{}, err
	}

	return DomainResult{
		BaseDomain: baseDomainStatus,
		WwwDomain:  WwwDomainStatus,
		Nameserver: nsStatus,
		MXRecord:   mxStatus,
	}, nil
}

// checkDomainStatus checks the domain's IPv6 availability.
func checkDomainStatus(domain string, c *dns.Client) (string, error) {
	result, err := queryDomainRecord(c, domain, dns.TypeAAAA)
	if err != nil {
		return "", err
	}
	if result != NoRecordsFound {
		return result, nil
	}

	// Check for IPv4 as fallback
	result, err = queryDomainRecord(c, domain, dns.TypeA)
	if err != nil {
		return "", err
	}
	if result != NoRecordsFound {
		return IPv4Only, nil
	}

	return NoRecordsFound, nil
}

// checkDNSRecords checks DNS records (NS, MX) concurrently.
func checkDNSRecords(domain string, c *dns.Client) (string, string, error) {
	var nsStatus, mxStatus string
	var nsErr, mxErr error
	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		defer wg.Done()
		nsStatus, nsErr = checkNameserver(domain, c)
	}()
	go func() {
		defer wg.Done()
		mxStatus, mxErr = checkMX(domain, c)
	}()
	wg.Wait()

	// If there are any errors, return them
	if nsErr != nil {
		return "", "", nsErr
	}
	if mxErr != nil {
		return "", "", mxErr
	}

	return nsStatus, mxStatus, nil
}

// checkNameserver performs a DNS query for NS records
func checkNameserver(domain string, c *dns.Client) (string, error) {
	log := log.With().Str("service", "checkNameserver").Logger()
	log.Debug().Msgf("Checking nameservers for [%s]", domain)

	// Get all nameservers for the domain
	nsList, err := getNameservers(c, domain)
	if err != nil {
		log.Warn().Msgf("Error getting nameservers for domain [%s]: %v", domain, err)
		return "", err
	}

	// Check each nameserver for IPv6
	for _, ns := range nsList {
		if checkInetType(c, ns, dns.TypeAAAA) {
			log.Debug().Msgf("Nameserver [%s] has IPv6", ns)
			return IPv6Available, nil
		}
	}
	// If no nameservers have IPv6, check for IPv4
	for _, ns := range nsList {
		if checkInetType(c, ns, dns.TypeA) {
			log.Debug().Msgf("Nameserver [%s] has IPv4", ns)
			return IPv4Only, nil
		}
	}

	// If no records found at all, return "no_records_found"
	log.Debug().Msgf("No nameservers found for domain [%s]", domain)
	return NoRecordsFound, nil
}

// getNameservers retrieves the nameservers for a given domain
func getNameservers(c *dns.Client, domain string) ([]string, error) {
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(domain), dns.TypeNS)
	m.RecursionDesired = true

	r, err := performQuery(c, m)
	if err != nil {
		log.Err(err).Msgf("Error querying DNS for nameservers for domain [%s]", domain)
		return nil, err
	}

	var nsRecords []string
	for _, a := range r.Answer {
		if ns, ok := a.(*dns.NS); ok {
			nsRecords = append(nsRecords, ns.Ns)
		}
	}
	return nsRecords, nil
}

// checkMX performs a DNS query for MX records
func checkMX(domain string, c *dns.Client) (string, error) {
	log := log.With().Str("service", "checkMX").Logger()
	log.Debug().Msgf("Checking MX records for IPv6 for domain [%s]", domain)

	// Get all MX records for the domain
	mxRecords, err := getMXRecords(c, domain)
	if err != nil {
		log.Warn().Msgf("Error getting mailservers for domain [%s]: %v", domain, err)
		return "", err
	}

	// Check each MX record for IPv6
	for _, mx := range mxRecords {
		if checkInetType(c, mx, dns.TypeAAAA) {
			log.Debug().Msgf("MX record [%s] has IPv6", mx)
			return IPv6Available, nil
		}
	}

	// If no MX records have IPv6, check for IPv4
	for _, mx := range mxRecords {
		if checkInetType(c, mx, dns.TypeA) {
			log.Debug().Msgf("MX record [%s] has IPv4", mx)
			return IPv4Only, nil
		}
	}

	// If no records found at all, return "no_records_found"
	log.Debug().Msgf("No MX records found for domain [%s]", domain)
	return NoRecordsFound, nil
}

// getMXRecords retrieves the MX records for a given domain
func getMXRecords(c *dns.Client, domain string) ([]string, error) {
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(domain), dns.TypeMX)
	m.RecursionDesired = true

	r, err := performQuery(c, m)
	if err != nil {
		log.Err(err).Msgf("Error querying DNS for MX records for domain [%s]", domain)
		return nil, err
	}

	var mxRecords []string
	for _, a := range r.Answer {
		if mx, ok := a.(*dns.MX); ok {
			mxRecords = append(mxRecords, mx.Mx)
		}
	}
	return mxRecords, nil
}

// checkInetType checks if a domain has a specified type of DNS record, following CNAME records if necessary.
// It returns true if the domain has a record of the specified type, and false otherwise.
func checkInetType(c *dns.Client, domain string, recordType uint16) bool {
	cnameHops := 0

	for {
		m := new(dns.Msg)
		m.SetQuestion(dns.Fqdn(domain), recordType)
		m.RecursionDesired = true

		r, err := performQuery(c, m)
		if err != nil {
			log.Err(err).Msgf("Error querying DNS for record type [%d] for domain [%s]", recordType, domain)
			return false
		}

		for _, rr := range r.Answer {
			switch rr := rr.(type) {
			case *dns.AAAA:
				if recordType == dns.TypeAAAA {
					return true // IPv6 address found
				}
			case *dns.A:
				if recordType == dns.TypeA {
					return true // IPv4 address found
				}
			case *dns.CNAME:
				cnameHops++
				if cnameHops > maxCNAMEHops {
					log.Warn().Msgf("Exceeded CNAME hop limit for domain [%s]", domain)
					return false // Exceeded CNAME hop limit
				}
				domain = rr.Target // Set the domain to the target of the CNAME and check again
				continue
			}
		}

		return false // No relevant records found
	}
}

// queryDomainRecord performs a DNS query for a given query name and type.
func queryDomainRecord(client *dns.Client, domain string, qtype uint16) (string, error) {
	cnameHops := 0

	for {
		m := new(dns.Msg)
		m.SetQuestion(dns.Fqdn(domain), qtype)
		m.RecursionDesired = true

		r, err := performQuery(client, m)
		if err != nil {
			log.Err(err).Msgf("Error querying DNS [%s]", domain)
			return "", err
		}

		if r.Rcode != dns.RcodeSuccess {
			if r.Rcode == dns.RcodeNameError { // NXDOMAIN
				return NoRecordsFound, nil
			}
			log.Printf("DNS query unsuccessful for [%s]: %s", domain, dns.RcodeToString[r.Rcode])
			return "", err
		}

		for _, rr := range r.Answer {
			switch rr := rr.(type) {
			case *dns.AAAA:
				if qtype == dns.TypeAAAA {
					log.Debug().Msgf("IPv6 Answer for [%s]: %s", domain, rr.AAAA.String())
					return IPv6Available, nil
				}
			case *dns.A:
				if qtype == dns.TypeA {
					log.Debug().Msgf("IPv4 Answer for [%s]: %s", domain, rr.A.String())
					return IPv4Only, nil
				}
			case *dns.CNAME:
				cnameHops++
				if cnameHops > maxCNAMEHops {
					log.Warn().Msgf("Exceeded CNAME hop limit for [%s]", domain)
					return "", fmt.Errorf("exceeded CNAME hop limit for domain [%s]", domain)
				}
				log.Debug().Msgf("Following CNAME for [%s]: %s", domain, rr.Target)
				domain = rr.Target // Set the domain to the target of the CNAME and check again
				continue
			}
		}

		return NoRecordsFound, nil // No relevant records found
	}
}

// performQuery performs a DNS query using multiple nameservers.
func performQuery(c *dns.Client, m *dns.Msg) (*dns.Msg, error) {
	var errs []string

	for _, nameserver := range nameservers {
		r, _, err := c.Exchange(m, nameserver)
		log := log.With().Str("nameserver", nameserver).Logger()
		if err != nil {
			errMsg := fmt.Sprintf("Error querying DNS server [%s]: %v", nameserver, err)
			log.Warn().Msg(errMsg)
			errs = append(errs, errMsg)
			continue // Try next nameserver on error
		}
		return r, nil // Successful response
	}

	// Join all errors into a single string
	return nil, fmt.Errorf("all nameservers failed: %s", strings.Join(errs, "; "))
}

// convertToASCII converts a domain to ASCII (Punycode) using IDNA2008 rules.
func convertToASCII(domain string) (string, error) {
	asciiDomain, err := idna.Lookup.ToASCII(domain)
	if err != nil {
		return "", err
	}
	return asciiDomain, nil
}

// ValidateDomain checks if the domain has enough DNS information to proceed with the checks.
func ValidateDomain(domain string) error {
	c := &dns.Client{Timeout: DefaultTimeout}

	// Convert domain to ASCII for DNS lookup
	domain, err := convertToASCII(domain)
	if err != nil {
		return fmt.Errorf("IDNA conversion error: %v", err)
	}

	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(domain), dns.TypeTXT)
	m.RecursionDesired = true

	// Check if domain has any DNS records, else disable it before performing any checks
	result, err := performQuery(c, m)
	if err != nil {
		return err
	}
	if result.Rcode != dns.RcodeSuccess {
		return fmt.Errorf("[%s] RCODE: %s", domain, dns.RcodeToString[result.Rcode])
		// return nil
	}

	return nil
}

func getTopLevelDomain(domain string) string {
	// Split the domain into parts
	parts := strings.Split(domain, ".")
	// Check if domain has at least two parts to consider it a valid domain
	if len(parts) >= 2 {
		// Return the last two parts as the top-level domain
		return parts[len(parts)-2] + "." + parts[len(parts)-1]
	}
	// If it's not a valid domain or a TLD itself, return the original domain
	return domain
}
