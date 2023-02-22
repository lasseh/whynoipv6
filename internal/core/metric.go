package core

import (
	"context"
	"encoding/json"
	"time"
	"whynoipv6/internal/postgres/db"

	"github.com/jackc/pgtype"
)

// MetricService is a service for metrics.
type MetricService struct {
	q *db.Queries
}

// NewMetricService creates a new MetricService.
func NewMetricService(d db.DBTX) *MetricService {
	return &MetricService{
		q: db.New(d),
	}
}

// MetricModel represents a metric.
type MetricModel struct {
	Time time.Time
	Data pgtype.JSONB
}

// StoreMetric stores a metric.
func (s *MetricService) StoreMetric(ctx context.Context, measurement string, data any) error {
	// Encode the data to a []byte
	dataBytes, _ := json.Marshal(data)

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

// GetMetrics gets all the metrics for a measurement.
func (s *MetricService) GetMetrics(ctx context.Context, measurement string) ([]MetricModel, error) {
	metrics, err := s.q.GetMetric(ctx, measurement)
	if err != nil {
		return []MetricModel{}, err
	}

	var m []MetricModel
	for _, d := range metrics {
		m = append(m, MetricModel{
			Time: d.Time,
			Data: d.Data,
		})
	}
	return m, nil

}
