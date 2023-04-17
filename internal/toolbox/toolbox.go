package toolbox

// Toolbox is a collection of tools used by v6manage
import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/miekg/dns"
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

// CheckTLD checks if a domain has an AAAA record and returns true on first hit
func (s *Service) CheckTLD(domain string) (QueryResult, error) {
	q := QueryResult{}

	var err error
	domain, err = IDNADomain(domain)
	if err != nil {
		return q, err
	}

	result, err := s.localQuery(domain, dns.TypeAAAA)
	if err != nil {
		return QueryResult{}, err
	}
	// Check for domain error
	if result.Rcode != dns.RcodeSuccess {
		q.Rcode = result.Rcode
		return q, fmt.Errorf("Rcode: %s", dns.RcodeToString[result.Rcode])
	}

	// Check if result has IPv6
	for _, r := range result.Answer {
		if r.Header().Rrtype == dns.TypeAAAA {
			q.IPv6 = true
			return q, nil
		}
		if r.Header().Rrtype == dns.TypeCNAME {
			c, _ := s.CheckTLD(r.(*dns.CNAME).Target)
			if c.IPv6 {
				q.IPv6 = true
				return q, nil
			}
		}
	}
	// No IPv6 found
	return q, nil
}

// CheckNS checks if a domain has an AAAA record in the NS records and returns true on first hit
func (s *Service) CheckNS(domain string) (QueryResult, error) {
	q := QueryResult{}

	var err error
	domain, err = IDNADomain(domain)
	if err != nil {
		return q, err
	}

	result, err := s.localQuery(domain, dns.TypeNS)
	if err != nil {
		return QueryResult{}, nil
	}
	// Check for domain error
	if result.Rcode != dns.RcodeSuccess {
		q.Rcode = result.Rcode
		return q, fmt.Errorf("Rcode: %s", dns.RcodeToString[result.Rcode])
	}

	// Loop over NameServers and check if any of them has IPv6
	for _, ns := range result.Answer {
		// If response is CNAME, follow it and check
		if ns.Header().Rrtype == dns.TypeCNAME {
			// log.Printf("[%s] Found CNAME in Nameserver Response\n", domain)
			nscname, _ := s.CheckTLD(ns.(*dns.CNAME).Target)
			if nscname.IPv6 {
				q.IPv6 = true
				return q, nil
			}
			return q, nil
		}

		nsq, err := s.CheckTLD(ns.(*dns.NS).Ns)
		if err != nil {
			// We should not care about ppl's creative dns server setups, so ignore this
			// We only care about if one of the dns server has IPv6 enabled!
			// log.Printf("[%s] Failed to lookup Nameserver: %s [%v]", domain, ns.(*dns.NS).Ns, err)
			return q, err
		}
		// Return true on first IPv6 enabled nameserver
		if nsq.IPv6 {
			q.IPv6 = true
			return q, nil
		}
	}
	// No IPv6 found
	return q, nil
}

// ValidateDomain checks if the domain has enough dns info to procede with the checks
// returns error of we dont find any record for domain.com or www.domain.com
func (s *Service) ValidateDomain(domain string) error {
	// Check for lookup errors
	var aError, wwwError bool

	var err error
	domain, err = IDNADomain(domain)
	if err != nil {
		return err
	}

	// Check nameserver
	resultns, err := s.localQuery(domain, dns.TypeTXT)
	if err != nil {
		// no name server to answer the question
		// disable return err here
		//
		// log.Printf("[%s] // localQuery Error: %v", domain, err)
		return err
	}

	if resultns.Rcode != dns.RcodeSuccess {
		// If we get a error here, disable the domain
		// log.Printf("[%s] --------------------------- NS Rcode: %s", domain, dns.RcodeToString[resultns.Rcode])
		return fmt.Errorf("%s", dns.RcodeToString[resultns.Rcode])
	}

	// check for A record (// TODO: fails on a ipv6 only domain)
	result, err := s.localQuery(domain, dns.TypeA)
	if err != nil {
		// log.Println("No A record for domain")
		aError = true
	}
	if err == nil {
		if result.Rcode != dns.RcodeSuccess {
			aError = true
		}
	}

	// check for www A record (fails on a ipv6 only domain)
	resultwww, err := s.localQuery(domain, dns.TypeA)
	if err != nil {
		// log.Println("No A record for www.domain")
		wwwError = true
	}
	if err == nil {
		if resultwww.Rcode != dns.RcodeSuccess {
			wwwError = true
		}
	}

	if aError && wwwError {
		return fmt.Errorf("No dns record for %s", domain)
	}

	return nil
}

// CheckCurl checks if a domain is available over IPv6 only
// TODO: implement this!
func (s *Service) CheckCurl(domain string) (bool, error) {
	webClient := http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
				dialer := net.Dialer{}
				return dialer.DialContext(ctx, "tcp6", addr)
			},
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
	// http get a website with webClient client
	resp, err := webClient.Get("https://" + domain)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 200 {
		return true, nil
	}

	return false, nil
}

// PercentOf calculate [number1] is what percent of [number2]
func (s *Service) PercentOf(current int, all int) float64 {
	percent := (float64(current) * float64(100)) / float64(all)
	return percent
}
