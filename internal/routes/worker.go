package routes

import (
	"log"
	"net/http"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/wireops/wireops/internal/sync"
	"github.com/wireops/wireops/internal/worker"
)

func RegisterWorkerRoutes(r *router.Router[*core.RequestEvent], app core.App, workerSvc *worker.Service, dispatcher sync.WorkerDispatcher, workerServer *worker.WorkerServer) {

	r.POST("/api/custom/worker/tokens", func(e *core.RequestEvent) error {
		if !e.HasSuperuserAuth() {
			return e.JSON(http.StatusUnauthorized, map[string]string{"error": "Unauthorized. Admin only."})
		}

		createdBy := ""
		if e.Auth != nil {
			createdBy = e.Auth.Id
		}

		token, record, err := workerSvc.IssueToken(createdBy)
		if err != nil {
			log.Printf("[WORKER] Error issuing token: %v", err)
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to issue worker token"})
		}

		return e.JSON(http.StatusOK, map[string]string{
			"token":      token,
			"token_id":   record.Id,
			"status":     record.GetString("status"),
			"expires_at": record.GetDateTime("expires_at").String(),
		})
	})

	r.GET("/api/custom/workers", func(e *core.RequestEvent) error {
		if !e.HasSuperuserAuth() {
			return e.JSON(http.StatusUnauthorized, map[string]string{"error": "Unauthorized. Admin only."})
		}

		records, err := app.FindAllRecords("workers")
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		result := make([]map[string]interface{}, 0, len(records))
		for _, rec := range records {
			var history []worker.HealthEvent
			_ = rec.UnmarshalJSONField("health_history", &history)
			if history == nil {
				history = []worker.HealthEvent{}
			}

			status := rec.GetString("status")
			if status == "ACTIVE" && !dispatcher.IsConnected(rec.Id) {
				status = "OFFLINE"
			}

			tokenRecord, tokenErr := workerSvc.GetTokenForWorker(rec.Id)
			tokenStatus := ""
			expiresAt := ""
			lastUsedAt := ""
			if tokenErr == nil && tokenRecord != nil {
				tokenStatus = tokenRecord.GetString("status")
				expiresAt = tokenRecord.GetDateTime("expires_at").String()
				lastUsedAt = tokenRecord.GetDateTime("last_used_at").String()
			}

			result = append(result, map[string]interface{}{
				"id":            rec.Id,
				"hostname":      rec.GetString("hostname"),
				"status":        status,
				"last_seen":     rec.GetDateTime("last_seen").String(),
				"health_history": history,
				"tags":          workerServer.GetWorkerTags(rec.Id),
				"is_embedded":   rec.GetString("fingerprint") == "embedded",
				"token_status":  tokenStatus,
				"token_expires": expiresAt,
				"token_last_used": lastUsedAt,
			})
		}

		return e.JSON(http.StatusOK, result)
	})

	r.POST("/api/custom/workers/{id}/revoke", func(e *core.RequestEvent) error {
		if !e.HasSuperuserAuth() {
			return e.JSON(http.StatusUnauthorized, map[string]string{"error": "Unauthorized. Admin only."})
		}

		workerID := e.Request.PathValue("id")


		record, err := app.FindRecordById("workers", workerID)
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "Worker not found"})
		}

		if record.GetString("fingerprint") == "embedded" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "Cannot revoke the embedded worker."})
		}

		stacks, err := app.FindAllRecords("stacks", dbx.HashExp{"worker": workerID})
		if err != nil && err.Error() != "sql: no rows in result set" {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to query stacks: " + err.Error()})
		}

		if len(stacks) > 0 {
			return e.JSON(http.StatusConflict, map[string]string{
				"error": "This worker has active stacks registered to it. Reassign or delete the stacks before revoking.",
			})
		}

		if err := workerSvc.RevokeWorker(workerID); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to revoke worker"})
		}

		workerServer.DisconnectWorker(workerID)

		return e.JSON(http.StatusOK, map[string]string{"status": "revoked"})
	})
}
