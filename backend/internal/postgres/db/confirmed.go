// Hand-written extension of the sqlc-generated package (not regenerated).
package db

import "github.com/jackc/pgx/v5/pgtype"

// ConfirmedSextet carries the six confirmed (status, since) column pairs in
// canonical dimension order: base, www, ns, mx, conn, resources. Every
// StatusBlock is built from this carrier; each row type lists its
// column→dimension pairing exactly once, in its Confirmed method.
type ConfirmedSextet struct {
	Status [6]*Ipv6Status
	Since  [6]pgtype.Timestamptz
}

// AllNull reports whether no dimension has ever been confirmed.
func (c *ConfirmedSextet) AllNull() bool {
	for _, s := range c.Status {
		if s != nil {
			return false
		}
	}
	return true
}

// Confirmed returns the row's confirmed sextet in canonical dimension order.
func (r *DomainConfirmedRow) Confirmed() ConfirmedSextet {
	return ConfirmedSextet{
		Status: [6]*Ipv6Status{r.BaseStatus, r.WwwStatus, r.NsStatus, r.MxStatus, r.ConnStatus, r.ResourcesStatus},
		Since:  [6]pgtype.Timestamptz{r.BaseSince, r.WwwSince, r.NsSince, r.MxSince, r.ConnSince, r.ResourcesSince},
	}
}

// Confirmed returns the row's confirmed sextet in canonical dimension order.
func (r *DomainDetailByHostRow) Confirmed() ConfirmedSextet {
	return ConfirmedSextet{
		Status: [6]*Ipv6Status{r.BaseStatus, r.WwwStatus, r.NsStatus, r.MxStatus, r.ConnStatus, r.ResourcesStatus},
		Since:  [6]pgtype.Timestamptz{r.BaseSince, r.WwwSince, r.NsSince, r.MxSince, r.ConnSince, r.ResourcesSince},
	}
}
