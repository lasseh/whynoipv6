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
	r.Get("/overview", rs.Overview)

	// GET /metrics/asn
	r.Get("/asn", rs.AsnMetrics)

	// GET /metrics/asn/search/{query}
	r.Get("/asn/search/{query}", rs.SearchAsn)

	return r
}

// Totals returns the aggregated metrics for all crawled domains.
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

// Overview returns the aggregated metrics for all crawled domains.
func (rs MetricHandler) Overview(w http.ResponseWriter, r *http.Request) {
	// metrics, err := rs.Repo.DomainStats(r.Context())
	metrics, err := rs.Repo.DomainStats(r.Context())
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
	Number    int32   `json:"number"`
	Name      string  `json:"name"`
	CountV4   int32   `json:"count_v4"`
	CountV6   int32   `json:"count_v6"`
	PercentV4 float64 `json:"percent_v4,omitempty"`
	PercentV6 float64 `json:"percent_v6,omitempty"`
}

// AsnMetrics returns the aggregated metrics for all crawled domains per ASN.
func (rs MetricHandler) AsnMetrics(w http.ResponseWriter, r *http.Request) {
	// Retrieve the order parameter from the URL
	order := r.URL.Query().Get("order")
	if order == "" {
		order = "ipv4"
	}

	// Validate the filter parameter
	if order != "ipv4" && order != "ipv6" {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, render.M{"error": "invalid filter parameter"})
		return
	}

	asn, err := rs.Repo.AsnList(r.Context(), 0, 50, order)
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

// SearchAsn returns the metrics for a given ASN.
func (rs MetricHandler) SearchAsn(w http.ResponseWriter, r *http.Request) {
	// Retrieve the query parameter from the URL
	searchQuery := chi.URLParam(r, "query")

	asn, err := rs.Repo.SearchAsn(r.Context(), searchQuery)
	if err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, render.M{"error": "asn not found"})
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
