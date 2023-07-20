package rest

import (
	"net/http"
	"time"
	"whynoipv6/internal/core"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/jackc/pgtype"
)

// MetricHandler is a handler for managing all metric-related operations.
type MetricHandler struct {
	Repo *core.MetricService
}

// MetricResponse is the response structure for a metric.
type MetricResponse struct {
	Time time.Time    `json:"time"`
	Data pgtype.JSONB `json:"data"`
}

// Routes returns a router with all metric endpoints mounted.
func (rs MetricHandler) Routes() chi.Router {
	r := chi.NewRouter()

	// GET /metrics/total
	r.Get("/total", rs.Totals)

	// GET /metrics/asn
	r.Get("/asn", rs.AsnMetrics)

	return r
}

// Totals returns the aggregated metrics for all crawled domains.
// TODO: Add pagination and limits
func (rs MetricHandler) Totals(w http.ResponseWriter, r *http.Request) {
	metrics, err := rs.Repo.GetMetrics(r.Context(), "domains")
	if err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, render.M{"error": "metric not found"})
		return
	}

	var metricList []MetricResponse
	for _, metric := range metrics {
		metricList = append(metricList, MetricResponse{
			Time: metric.Time,
			Data: metric.Data,
		})
	}

	render.JSON(w, r, metricList)
}

// ASNResponse represents a BGP Autonomous System Number (ASN) and its associated information.
type ASNResponse struct {
	ID        int64   `json:"id"`
	Number    int32   `json:"asn"`
	Name      string  `json:"name"`
	CountV4   int32   `json:"count_v4,omitempty"`
	CountV6   int32   `json:"count_v6,omitempty"`
	PercentV4 float64 `json:"percent_v4,omitempty"`
	PercentV6 float64 `json:"percent_v6,omitempty"`
}

// AsnMetrics returns the aggregated metrics for all crawled domains per ASN.
func (rs MetricHandler) AsnMetrics(w http.ResponseWriter, r *http.Request) {
	asn, err := rs.Repo.ListASN(r.Context(), 0, 50)
	if err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, render.M{"error": "metric not found"})
		return
	}

	var asnList []ASNResponse
	for _, a := range asn {
		asnList = append(asnList, ASNResponse{
			ID:        a.ID,
			Number:    a.Number,
			Name:      a.Name,
			CountV4:   a.CountV4,
			CountV6:   a.CountV6,
			PercentV4: a.PercentV4,
			PercentV6: a.PercentV6,
		})
	}

	render.JSON(w, r, asnList)
}
