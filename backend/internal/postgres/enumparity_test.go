package postgres

import (
	"testing"

	"github.com/lasseh/whynoipv6/internal/domain"
	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// TestEnumParity pins the domain enums to their sqlc-generated db twins:
// the crawler converts between them by value cast (bindDim, obsDB), which
// is only sound while the declared value sets are identical.
func TestEnumParity(t *testing.T) {
	statusTwins := map[domain.IPv6Status]db.Ipv6Status{
		domain.StatusSupported:     db.Ipv6StatusSupported,
		domain.StatusUnsupported:   db.Ipv6StatusUnsupported,
		domain.StatusNoRecord:      db.Ipv6StatusNoRecord,
		domain.StatusNotApplicable: db.Ipv6StatusNotApplicable,
	}
	for d, g := range statusTwins {
		if string(d) != string(g) {
			t.Errorf("IPv6Status %q != db %q", d, g)
		}
	}

	obsTwins := map[domain.Observation]db.Observation{
		domain.ObsSupported:     db.ObservationSupported,
		domain.ObsPartial:       db.ObservationPartial,
		domain.ObsUnsupported:   db.ObservationUnsupported,
		domain.ObsNoRecord:      db.ObservationNoRecord,
		domain.ObsNotApplicable: db.ObservationNotApplicable,
		domain.ObsError:         db.ObservationError,
		domain.ObsInconsistent:  db.ObservationInconsistent,
	}
	if len(obsTwins) != len(domain.ObservationValues) {
		t.Errorf("twin table covers %d observations, declared %d",
			len(obsTwins), len(domain.ObservationValues))
	}
	for d, g := range obsTwins {
		if string(d) != string(g) {
			t.Errorf("Observation %q != db %q", d, g)
		}
	}
}
