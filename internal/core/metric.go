package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"whynoipv6/internal/postgres/db"

	"github.com/jackc/pgtype"
)

// MetricService is a service for handling metrics.
type MetricService struct {
	q *db.Queries
}

// NewMetricService creates a new MetricService instance.
func NewMetricService(d db.DBTX) *MetricService {
	return &MetricService{
		q: db.New(d),
	}
}

// Metric represents a metric data point.
type Metric struct {
	Time time.Time
	Data pgtype.JSONB
}

// StoreMetric stores a metric data point with the given measurement name and data.
func (s *MetricService) StoreMetric(ctx context.Context, measurement string, data any) error {
	// Encode the data to a []byte
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return err
	}

	// Create a new pgtype.JSONB struct
	jsonb := &pgtype.JSONB{}

	// Set the data on the pgtype.JSONB struct
	if err := jsonb.Set(dataBytes); err != nil {
		return err
	}

	return s.q.StoreMetric(ctx, db.StoreMetricParams{
		Measurement: measurement,
		Data:        *jsonb,
	})
}

// GetMetrics retrieves all the metrics for a specified measurement.
func (s *MetricService) GetMetrics(ctx context.Context, measurement string) ([]Metric, error) {
	metrics, err := s.q.GetMetric(ctx, measurement)
	if err != nil {
		return nil, err
	}

	var metricList []Metric
	for _, metric := range metrics {
		metricList = append(metricList, Metric{
			Time: metric.Time,
			Data: metric.Data,
		})
	}
	return metricList, nil
}

// AsnList retrieves all BGP ASN records.
func (s *MetricService) AsnList(
	ctx context.Context,
	offset, limit int64,
	order string,
) ([]ASNModel, error) {
	var asnRecords []db.Asn
	var err error

	switch order {
	case "ipv6":
		asnRecords, err = s.q.AsnByIPv6(ctx, db.AsnByIPv6Params{Offset: offset, Limit: limit})
	default: // default to ipv4
		asnRecords, err = s.q.AsnByIPv4(ctx, db.AsnByIPv4Params{Offset: offset, Limit: limit})
	}

	if err != nil {
		return nil, fmt.Errorf("error fetching ASN records: %w", err)
	}

	asns := make([]ASNModel, 0, len(asnRecords))
	for _, asn := range asnRecords {
		asns = append(asns, ASNModel{
			ID:      asn.ID,
			Number:  asn.Number,
			Name:    asn.Name,
			CountV4: asn.CountV4.Int32 - asn.CountV6.Int32, // Since all domains has IPv4, we subtract IPv6 from IPv4 to get the IPv4 only domains
			CountV6: asn.CountV6.Int32,
		})
	}

	return asns, nil
}

// SearchAsn retrieves all BGP ASN records for the given ASN number.
func (s *MetricService) SearchAsn(ctx context.Context, searchQuery string) ([]ASNModel, error) {
	// Normalize search query by trimming "AS" prefix if present
	searchQuery = strings.TrimPrefix(strings.ToUpper(searchQuery), "AS")

	var asnRecords []db.Asn
	asnNumber, err := strconv.Atoi(searchQuery)
	if err == nil {
		// Search query is a valid ASN number
		asnRecords, err = s.q.SearchAsNumber(ctx, int32(asnNumber))
		if err != nil {
			return nil, fmt.Errorf("error fetching ASN record by number: %w", err)
		}
	} else {
		// Search query is an ASN name
		asnRecords, err = s.q.SearchAsName(ctx, NullString(searchQuery))
		if err != nil {
			return nil, fmt.Errorf("error fetching ASN record by name: %w", err)
		}
	}

	asns := make([]ASNModel, 0, len(asnRecords))
	for _, asn := range asnRecords {
		asns = append(asns, ASNModel{
			ID:      asn.ID,
			Number:  asn.Number,
			Name:    asn.Name,
			CountV4: asn.CountV4.Int32 - asn.CountV6.Int32, // Since all domains has IPv4, we subtract IPv6 from IPv4 to get the IPv4 only domains
			CountV6: asn.CountV6.Int32,
		})
	}

	return asns, nil
}

// type DomainStatsModel struct {
// 	TotalSites int64 `json:"total_sites"`
// 	TotalAaaa  int64 `json:"total_aaaa"`
// 	TotalWww   int64 `json:"total_www"`
// 	TotalBoth  int64 `json:"total_both"`
// 	TotalNs    int64 `json:"total_ns"`
// 	Top1k      int64 `json:"top_1k"`
// 	TopNs      int64 `json:"top_ns"`
// }

// DomainStatsModel represents a domain statistic.
type DomainStatsModel struct {
	Time       time.Time
	Totalsites pgtype.Numeric
	Totalns    pgtype.Numeric
	Totalaaaa  pgtype.Numeric
	Totalwww   pgtype.Numeric
	Totalboth  pgtype.Numeric
	Top1k      pgtype.Numeric
	Topns      pgtype.Numeric
}

// DomainStats retrieves the aggregated metrics for all crawled domains.
func (s *MetricService) DomainStats(ctx context.Context) ([]Metric, error) {
	metrics, err := s.q.DomainStats(ctx)
	if err != nil {
		return nil, err
	}

	var metricList []Metric
	for _, metric := range metrics {
		metricList = append(metricList, Metric{
			Time: metric.Time,
			Data: metric.Data,
		})
	}
	return metricList, nil
}
