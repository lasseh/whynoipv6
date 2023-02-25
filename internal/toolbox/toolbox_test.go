package toolbox_test

import (
	"testing"
	"whynoipv6/internal/toolbox"
)

func TestInternationalizeDomains(t *testing.T) {
	domains := []string{
		"xn--mgbkt9eckr.net",
		"نسوانجي.net",
		"bodøposten.no",
	}
	expected := []string{
		"xn--mgbkt9eckr.net",
		"xn--mgbkt9eckr.net",
		"xn--bodposten-n8a.no",
	}

	for i, domain := range domains {
		idna, err := toolbox.IDNADomain(domain)
		if err != nil {
			t.Errorf("Failed to internationalize domain '%s': %v", domain, err)
		}
		if expected[i] != idna {
			t.Errorf("Internationalized domain name[0] does not match expected output[1]: '%s' != '%s'", idna, expected)
		}
	}
}
