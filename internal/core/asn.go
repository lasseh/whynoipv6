package core

import (
	"context"
	"whynoipv6/internal/postgres/db"

	"github.com/jackc/pgx/v4"
)

// ASNService is a service for BGP AS Numbers.
type ASNService struct {
	q *db.Queries
}

// NewASNService creates a new AsnService.
func NewASNService(d db.DBTX) *ASNService {
	return &ASNService{
		q: db.New(d),
	}
}

// ASNModel represents a BGP AS Number.
type ASNModel struct {
	ID     int64  `json:"id"`
	Number int32  `json:"asn"`
	Name   string `json:"name"`
}

// CreateAsn creates a new BGP AS Number.
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

// GetASByNumber gets a BGP Info by AS Number.
func (s *ASNService) GetASByNumber(ctx context.Context, number int32) (ASNModel, error) {
	a, err := s.q.GetASByNumber(ctx, number)
	if err == pgx.ErrNoRows {
		return ASNModel{}, pgx.ErrNoRows
	}
	if err != nil {
		return ASNModel{}, err
	}
	return ASNModel{
		ID:     a.ID,
		Number: a.Number,
		Name:   a.Name,
	}, nil
}
