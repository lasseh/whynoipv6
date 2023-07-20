package core

import (
	"context"
	"whynoipv6/internal/postgres/db"

	"github.com/jackc/pgx/v4"
)

// ASNService provides methods to interact with BGP Autonomous System Numbers (ASNs).
type ASNService struct {
	q *db.Queries
}

// NewASNService creates a new ASNService instance.
func NewASNService(d db.DBTX) *ASNService {
	return &ASNService{
		q: db.New(d),
	}
}

// ASNModel represents a BGP Autonomous System Number (ASN) and its associated information.
type ASNModel struct {
	ID        int64   `json:"id"`
	Number    int32   `json:"asn"`
	Name      string  `json:"name"`
	CountV4   int32   `json:"count_v4,omitempty"`
	CountV6   int32   `json:"count_v6,omitempty"`
	PercentV4 float64 `json:"percent_v4,omitempty"`
	PercentV6 float64 `json:"percent_v6,omitempty"`
}

// CreateAsn creates a new BGP ASN record with the specified number and name.
func (s *ASNService) CreateAsn(ctx context.Context, number int32, name string) (ASNModel, error) {
	asn, err := s.q.CreateASN(ctx, db.CreateASNParams{
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
	asnRecord, err := s.q.GetASByNumber(ctx, number)
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

// ListASN retrieves all BGP ASN records.
// func (s *ASNService) ListASN(ctx context.Context, offset, limit int32) ([]ASNModel, error) {
// 	asnRecords, err := s.q.ListASN(ctx, db.ListASNParams{
// 		Offset: offset,
// 		Limit:  limit,
// 	})
// 	if err != nil {
// 		return nil, err
// 	}
// 	asns := make([]ASNModel, len(asnRecords))
// 	for i, asnRecord := range asnRecords {
// 		asns[i] = ASNModel{
// 			ID:      asnRecord.ID,
// 			Number:  asnRecord.Number,
// 			Name:    asnRecord.Name,
// 			CountV4: asnRecord.CountV4.Int32,
// 			CountV6: asnRecord.CountV6.Int32,
// 		}
// 	}
// 	return asns, nil
// }

// CalculateASNStats calculates the statistics for an ASN.
func (s *ASNService) CalculateASNStats(ctx context.Context) error {
	return s.q.CalculateASNStats(ctx)
}
