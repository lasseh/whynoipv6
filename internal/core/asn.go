package core

import (
	"context"
	"whynoipv6/internal/postgres/db"

	"github.com/jackc/pgx/v4"
)

// ASNService provides methods to interact with BGP Autonomous System Numbers (ASNs).
type ASNService struct {
	queries *db.Queries
}

// NewASNService creates a new ASNService instance.
func NewASNService(d db.DBTX) *ASNService {
	return &ASNService{
		queries: db.New(d),
	}
}

// ASNModel represents a BGP Autonomous System Number (ASN) and its associated information.
type ASNModel struct {
	ID     int64  `json:"id"`
	Number int32  `json:"asn"`
	Name   string `json:"name"`
}

// CreateAsn creates a new BGP ASN record with the specified number and name.
func (s *ASNService) CreateAsn(ctx context.Context, number int32, name string) (ASNModel, error) {
	asn, err := s.queries.CreateASN(ctx, db.CreateASNParams{
		Number: number,
		Name:   name,
	})
	if err != nil {
		return ASNModel{}, err
	}
	return ASNModel{
		ID:     asn.ID,
		Number: asn.Number,
		Name:   asn.Name,
	}, nil
}

// GetASByNumber retrieves the BGP ASN record with the specified AS number.
func (s *ASNService) GetASByNumber(ctx context.Context, number int32) (ASNModel, error) {
	asnRecord, err := s.queries.GetASByNumber(ctx, number)
	if err == pgx.ErrNoRows {
		return ASNModel{}, pgx.ErrNoRows
	}
	if err != nil {
		return ASNModel{}, err
	}
	return ASNModel{
		ID:     asnRecord.ID,
		Number: asnRecord.Number,
		Name:   asnRecord.Name,
	}, nil
}
