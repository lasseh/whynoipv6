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
var nameservers = []string{
	"[2606:4700:4700::1111]:53",
	"[2606:4700:4700::1001]:53",
	"1.1.1.1:53",
	"1.0.0.1:53",
}

// DomainResult represents a scan result.
type DomainResult struct {
	BaseDomain string
	WwwDomain  string
	Nameserver string
	MXRecord   string
	// v6Only     string // TODO: Add v6Only check
}

// DomainStatus checks the domain's IPv6, NS, and MX records.
func DomainStatus(domain string) (DomainResult, error) {
	c := &dns.Client{Timeout: DefaultTimeout}
	log := log.With().Str("service", "DomainStatus").Logger()

	// Convert domain to ASCII for DNS lookup
	domain, err := convertToASCII(domain)
	if err != nil {
		return DomainResult{}, fmt.Errorf("IDNA conversion error: %v", err)
	}

	baseDomainStatus, err := checkDomainStatus(domain, c)
	if err != nil {
		log.Error().Msgf("Error checking base domain [%s]: %v", domain, err)
		log.Debug().Err(err).Msgf("Error checking base domain [%s]", domain)
		// return DomainResult{}, err
	}

	WwwDomainStatus, err := checkDomainStatus("www."+domain, c)
	if err != nil {
		log.Error().Msgf("Error checking www domain [%s]: %v", domain, err)
		// return DomainResult{}, err
	}

	nsStatus, mxStatus, err := checkDNSRecords(domain, c)
	if err != nil {
		log.Err(err).Msgf("Error checking NS/MX records for domain [%s]: %v", domain, err)
		// return DomainResult{}, err
	}

	return DomainResult{
		BaseDomain: baseDomainStatus,
		WwwDomain:  WwwDomainStatus,
		Nameserver: nsStatus,
		MXRecord:   mxStatus,
	}, nil
}

// checkDomainStatus checks the domain's IPv6 availability.
// It returns the string value of the result, or an error if the query fails.
func checkDomainStatus(domain string, c *dns.Client) (string, error) {
	result, err := queryDomainStatus(c, domain, dns.TypeAAAA)
	if err != nil {
		return "", err
	}
	if result != NoRecordsFound {
		return result, nil
	}

	// Check for IPv4 as fallback
	result, err = queryDomainStatus(c, domain, dns.TypeA)
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
	// log.Debug().Msgf("Checking nameservers for [%s]", domain)

	// Check on the top level domain.
	tld := getTopLevelDomain(domain)

	// Get all nameservers for the domain
	nsList, err := getNameservers(c, tld)
	if err != nil {
		log.Warn().Msgf("Error getting nameservers for domain [%s]: %v", domain, err)
		return "", err
	}

	// Check each nameserver for IPv6
	for _, ns := range nsList {
		if checkInetType(c, ns, dns.TypeAAAA) {
			log.Debug().Msgf("[%s] Nameserver [%s] has IPv6", domain, ns)
			return IPv6Available, nil
		}
	}
	// If no nameservers have IPv6, check for IPv4
	for _, ns := range nsList {
		if checkInetType(c, ns, dns.TypeA) {
			log.Debug().Msgf("[%s] Nameserver [%s] has IPv4", domain, ns)
			return IPv4Only, nil
		}
	}

	// If no records found at all, return "no_records_found"
	log.Debug().Msgf("[%s] No nameservers found for domain", domain)
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
	// log.Debug().Msgf("Checking MX records for IPv6 for domain [%s]", domain)

	// Get all MX records for the domain
	mxRecords, err := getMXRecords(c, domain)
	if err != nil {
		log.Warn().Msgf("Error getting mailservers for domain [%s]: %v", domain, err)
		return "", err
	}

	// Check each MX record for IPv6
	for _, mx := range mxRecords {
		if checkInetType(c, mx, dns.TypeAAAA) {
			log.Debug().Msgf("[%s] MX record [%s] has IPv6", domain, mx)
			return IPv6Available, nil
		}
	}

	// If no MX records have IPv6, check for IPv4
	for _, mx := range mxRecords {
		if checkInetType(c, mx, dns.TypeA) {
			log.Debug().Msgf("[%s] MX record [%s] has IPv4", domain, mx)
			return IPv4Only, nil
		}
	}

	// If no records found at all, return "no_records_found"
	log.Debug().Msgf("[%s] No MX records found for domain", domain)
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
			log.Err(err).
				Msgf("Error querying DNS for record type [%d] for domain [%s]", recordType, domain)
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

// queryDomainStatus performs a DNS query for a given query name and type.
// It returns the string value of the result, or an error if the query fails.
func queryDomainStatus(client *dns.Client, domain string, qtype uint16) (string, error) {
	log := log.With().Str("service", "queryDomainStatus").Logger()
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
			log.Printf("[%s] DNS query unsuccessful: %s", domain, dns.RcodeToString[r.Rcode])
			return "", err
		}

		for _, rr := range r.Answer {
			switch rr := rr.(type) {
			case *dns.AAAA:
				if qtype == dns.TypeAAAA {
					log.Debug().Msgf("[%s] IPv6 Answer: %s", domain, rr.AAAA.String())
					return IPv6Available, nil
				}
			case *dns.A:
				if qtype == dns.TypeA {
					log.Debug().Msgf("[%s] IPv4 Answer: %s", domain, rr.A.String())
					return IPv4Only, nil
				}
			case *dns.CNAME:
				cnameHops++
				if cnameHops > maxCNAMEHops {
					log.Warn().Msgf("Exceeded CNAME hop limit for [%s]", domain)
					return "", fmt.Errorf("exceeded CNAME hop limit for domain [%s]", domain)
				}
				log.Debug().Msgf("[%s] Following CNAME: %s", domain, rr.Target)
				domain = rr.Target // Set the domain to the target of the CNAME and check again
				continue
			}
		}

		return NoRecordsFound, nil // No relevant records found
	}
}

// IPLookup performs a DNS lookup for a given domain and returns the first IPv6 or IPv4 address found.
func IPLookup(domain string) (string, error) {
	c := &dns.Client{Timeout: DefaultTimeout}

	// Convert domain to ASCII for DNS lookup
	domain, err := convertToASCII(domain)
	if err != nil {
		return "", fmt.Errorf("IDNA conversion error: %v", err)
	}

	// Get the IPv6 for the domain
	ipv6, err := queryDNSRecord(c, domain, dns.TypeAAAA)
	if err != nil {
		return "", err
	}
	if ipv6 != nil && len(ipv6.Answer) > 0 {
		return ipv6.Answer[0].(*dns.AAAA).AAAA.String(), nil
	}

	// Get the IPv4 for the domain
	ip, err := queryDNSRecord(c, domain, dns.TypeA)
	if err != nil {
		return "", err
	}
	if ip != nil && len(ip.Answer) > 0 {
		return ip.Answer[0].(*dns.A).A.String(), nil
	}

	return "", nil
}

// queryDNSRecord performs a DNS query for a given query name and type.
// It returns the answer in a *dns.Msg (or nil in case of an error, in which case err will be set accordingly.)
func queryDNSRecord(client *dns.Client, domain string, qtype uint16) (*dns.Msg, error) {
	log := log.With().Str("service", "queryDNSRecord").Logger()
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(domain), qtype)
	m.RecursionDesired = true

	r, err := performQuery(client, m)
	if err != nil {
		log.Err(err).Msgf("Error querying DNS [%s]", domain)
		return nil, err
	}

	if r.Rcode != dns.RcodeSuccess {
		log.Warn().Msgf("[%s] DNS query unsuccessful: %s", domain, dns.RcodeToString[r.Rcode])
		return nil, err
	}

	// Check if the result is a CNAME
	for _, rr := range r.Answer {
		switch rr := rr.(type) {
		case *dns.CNAME:
			log.Debug().Msgf("[%s] Following CNAME: %s", domain, rr.Target)
			domain = rr.Target // Set the domain to the target of the CNAME and check again
			return queryDNSRecord(client, domain, qtype)
		}
	}

	return r, nil
}

// performQuery performs a DNS query using multiple nameservers.
// returns the first successful response, or an error if all nameservers fail.
func performQuery(c *dns.Client, m *dns.Msg) (*dns.Msg, error) {
	var errs []string
	for _, nameserver := range nameservers {
		r, _, err := c.Exchange(m, nameserver)
		log := log.With().Str("nameserver", nameserver).Logger()
		if err != nil {
			// errMsg := fmt.Sprintf("Error querying DNS server [%s]: %v", nameserver, err)
			// log.Warn().Msg(errMsg)
			// log.Err(err).Msgf("[%v] Query error on %v", m.Question[0].Name, dns.TypeToString[m.Question[0].Qtype])
			log.Err(err).
				Msgf("Error checking %v on %v", dns.TypeToString[m.Question[0].Qtype], m.Question[0].Name)
			// log.Warn().Msgf("Query: %v", m)
			// errs = append(errs, errMsg)
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
func ValidateDomain(domain string) (int, error) {
	c := &dns.Client{Timeout: DefaultTimeout}

	// Convert domain to ASCII for DNS lookup
	domain, err := convertToASCII(domain)
	if err != nil {
		// Return a non-zero exit code to disable the domain
		return 1, fmt.Errorf("IDNA conversion error: %v", err)
	}

	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(domain), dns.TypeTXT)
	m.RecursionDesired = true

	// Check if domain has any DNS records, else disable it before performing any checks
	result, err := performQuery(c, m)
	if err != nil {
		return 0, err
	}
	if result.Rcode != dns.RcodeSuccess {
		return result.Rcode, fmt.Errorf("[%s] RCODE: %s", domain, dns.RcodeToString[result.Rcode])
	}

	return 0, nil
}

// getTopLevelDomain extracts the top-level domain from a domain.
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
