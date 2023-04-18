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

	return r
}

// Totals returns the aggregated metrics for all crawled domains.
func (rs MetricHandler) Totals(w http.ResponseWriter, r *http.Request) {
	metrics, err := rs.Repo.GetMetrics(r.Context(), "domains")
	if err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, render.M{"error": "domain not found"})
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
