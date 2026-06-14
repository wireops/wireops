package routes

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
	"github.com/wireops/wireops/internal/metrics"
	"github.com/wireops/wireops/internal/rbac"
	"github.com/wireops/wireops/internal/sync"
)

// RegisterMetricsRoutes exposes Prometheus metrics on the UI/API port (PORT).
func RegisterMetricsRoutes(r *router.Router[*core.RequestEvent], app core.App, dispatcher sync.WorkerDispatcher) {
	auth := rbac.Require(rbac.CapViewMetrics)

	writePrometheus := func(e *core.RequestEvent, body string) error {
		e.Response.Header().Set("Content-Type", metrics.PrometheusContentType)
		_, err := e.Response.Write([]byte(body))
		return err
	}

	aggregateHandler := func(e *core.RequestEvent) error {
		out, err := metrics.CollectAll(e.Request.Context(), app, dispatcher)
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return writePrometheus(e, out)
	}

	// Canonical Prometheus path on the operational port (same as UI).
	r.GET("/metrics", aggregateHandler).BindFunc(auth)

	// Backward-compatible alias.
	r.GET("/api/custom/metrics", aggregateHandler).BindFunc(auth)

	// Per-worker metrics for advanced scrape targets.
	r.GET("/api/custom/workers/{id}/metrics", func(e *core.RequestEvent) error {
		workerID := e.Request.PathValue("id")
		worker, err := app.FindRecordById("workers", workerID)
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "Worker not found"})
		}

		ctx, cancel := context.WithTimeout(e.Request.Context(), 10*time.Second)
		defer cancel()

		out, collectErr := metrics.CollectWorker(ctx, dispatcher, workerID)
		if collectErr != nil {
			return e.JSON(http.StatusServiceUnavailable, map[string]string{
				"error": fmt.Sprintf("worker '%s' is offline or unreachable: %s", worker.GetString("hostname"), collectErr.Error()),
			})
		}

		return writePrometheus(e, out)
	}).BindFunc(auth)
}
