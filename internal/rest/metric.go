package rest

import (
	"net/http"
	"time"
	"whynoipv6/internal/core"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/jackc/pgtype"
)

// MetricHandler is a handler for all metrics.
type MetricHandler struct {
	Repo *core.MetricService
}

// MetricResponse is the response for a domain.
type MetricResponse struct {
	Time time.Time    `json:"time"`
	Data pgtype.JSONB `json:"data"`
}

// Routes returns a router with all domain endpoints mounted.
func (rs MetricHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/total", rs.Totals) // GET /metrics/total
	return r
}

// Totals is the status for all domains crawled.
func (rs MetricHandler) Totals(w http.ResponseWriter, r *http.Request) {
	metrics, err := rs.Repo.GetMetrics(r.Context(), "domains")
	if err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, render.M{"error": "domain not found"})
		return
	}

	var metriclist []MetricResponse
	for _, metric := range metrics {
		metriclist = append(metriclist, MetricResponse{
			Time: metric.Time,
			Data: metric.Data,
		})
	}

	render.JSON(w, r, metriclist)
}
