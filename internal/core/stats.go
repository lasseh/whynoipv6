package core

import (
	"whynoipv6/internal/postgres/db"
)

// StatService is a service for managing domain statistics.
type StatService struct {
	q *db.Queries
}

// NewStatService creates a new StatService instance.
func NewStatService(d db.DBTX) *StatService {
	return &StatService{
		q: db.New(d),
	}
}

// DomainStat represents a domain statistic.
// type DomainStat struct {
// 	TotalSites int64 `json:"total_sites"`
// 	TotalAaaa  int64 `json:"total_aaaa"`
// 	TotalWww   int64 `json:"total_www"`
// 	TotalBoth  int64 `json:"total_both"`
// 	TotalNs    int64 `json:"total_ns"`
// 	Top1k      int64 `json:"top_1k"`
// 	TopNs      int64 `json:"top_ns"`
// }

// DomainStats retrieves the statistics for all crawled domains.
// func (s *StatService) DomainStats(ctx context.Context) (DomainStat, error) {
// 	stats, err := s.q.DomainStats(ctx)
// 	if err != nil {
// 		return DomainStat{}, err
// 	}
// 	return DomainStat{
// 		TotalSites: stats.TotalSites,
// 		TotalAaaa:  stats.TotalAaaa,
// 		TotalWww:   stats.TotalWww,
// 		TotalBoth:  stats.TotalBoth,
// 		TotalNs:    stats.TotalNs,
// 		Top1k:      stats.Top1k,
// 		TopNs:      stats.TopNs,
// 	}, nil
// }

// CalculateCountryStats calculates the statistics for a country.
// func (s *StatService) CalculateCountryStats(ctx context.Context) error {
// 	return s.q.CalculateCountryStats(ctx)
// }

// CalculateASNStats calculates the statistics for an ASN.
// func (s *StatService) CalculateASNStats(ctx context.Context) error {
// 	return s.q.CalculateASNStats(ctx)
// }
