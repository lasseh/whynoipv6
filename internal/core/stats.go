package core

import (
	"context"
	"whynoipv6/internal/postgres/db"
)

// StatService is a service for managing stats.
type StatService struct {
	q *db.Queries
}

// NewStatService creates a new StatService.
func NewStatService(d db.DBTX) *StatService {
	return &StatService{
		q: db.New(d),
	}
}

// StatModel represents a stat.
type StatModel struct {
	TotalSites int64 `json:"total_sites"`
	TotalAaaa  int64 `json:"total_aaaa"`
	TotalWww   int64 `json:"total_www"`
	TotalBoth  int64 `json:"total_both"`
	TotalNs    int64 `json:"total_ns"`
	Top1k      int64 `json:"top_1k"`
	TopNs      int64 `json:"top_ns"`
}

// Domainstats gets the stats for all domains we have crawled.
func (s *StatService) Domainstats(ctx context.Context) (StatModel, error) {
	stats, err := s.q.DomainStats(ctx)
	if err != nil {
		return StatModel{}, err
	}
	return StatModel{
		TotalSites: stats.TotalSites,
		TotalAaaa:  stats.TotalAaaa,
		TotalWww:   stats.TotalWww,
		TotalBoth:  stats.TotalBoth,
		TotalNs:    stats.TotalNs,
		Top1k:      stats.Top1k,
		TopNs:      stats.TopNs,
	}, nil
}
