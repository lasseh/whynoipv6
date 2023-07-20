package core

import (
	"context"
	"encoding/json"
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
func (s *MetricService) StoreMetric(ctx context.Context, measurement string, data interface{}) error {
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

// ListASN retrieves all BGP ASN records.
func (s *MetricService) ListASN(ctx context.Context, offset, limit int32) ([]ASNModel, error) {
	asnRecords, err := s.q.ListASN(ctx, db.ListASNParams{
		Offset: offset,
		Limit:  limit,
	})
	if err != nil {
		return nil, err
	}
	var asns []ASNModel
	for _, asn := range asnRecords {
		asns = append(asns, ASNModel{
			ID:      asn.ID,
			Number:  asn.Number,
			Name:    asn.Name,
			CountV4: asn.CountV4.Int32,
			CountV6: asn.CountV6.Int32,
		})
	}
	return asns, nil
}
