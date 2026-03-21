package routes

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/wireops/wireops/internal/pki"
	"github.com/wireops/wireops/internal/protocol"
	"github.com/wireops/wireops/internal/sync"
	"github.com/wireops/wireops/internal/worker"
)

func RegisterWorkerRoutes(r *router.Router[*core.RequestEvent], app core.App, workerSvc *worker.Service, pkiSvc *pki.Service, dispatcher sync.WorkerDispatcher, mtlsServer *worker.MTLSServer) {

	// POST /worker/seat
	r.POST("/api/custom/worker/seat", func(e *core.RequestEvent) error {
		if !e.HasSuperuserAuth() {
			return e.JSON(http.StatusUnauthorized, map[string]string{"error": "Unauthorized. Admin only."})
		}

		token, err := workerSvc.GenerateSeat()
		if err != nil {
			log.Printf("[WORKER] Error generating seat: %v", err)
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to generate seat"})
		}

		return e.JSON(http.StatusOK, map[string]string{"seat": token})
	})

	// GET /workers
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

			certNotAfter := rec.GetDateTime("cert_not_after").String()
			certStatus := ""
			if certNotAfter != "" {
				certStatus = pki.CertStatus(rec.GetDateTime("cert_not_after").Time())
			}

			result = append(result, map[string]interface{}{
				"id":              rec.Id,
				"hostname":        rec.GetString("hostname"),
				"fingerprint":     rec.GetString("fingerprint"),
				"status":          status,
				"last_seen":       rec.GetDateTime("last_seen").String(),
				"health_history":  history,
				"tags":            mtlsServer.GetWorkerTags(rec.Id),
				"cert_not_after":  certNotAfter,
				"cert_status":     certStatus,
			})
		}

		return e.JSON(http.StatusOK, result)
	})

	// GET /settings/pki
	r.GET("/api/custom/settings/pki", func(e *core.RequestEvent) error {
		if !e.HasSuperuserAuth() {
			return e.JSON(http.StatusUnauthorized, map[string]string{"error": "Unauthorized. Admin only."})
		}

		details, err := pkiSvc.GetPKIDetails()
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		return e.JSON(http.StatusOK, details)
	})

	// POST /worker/bootstrap
	r.POST("/api/custom/worker/bootstrap", func(e *core.RequestEvent) error {
		var req struct {
			BootstrapToken string `json:"bootstrap_token"`
			CSR            string `json:"csr"` // PEM encoded
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&req); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid JSON body"})
		}

		if !workerSvc.ValidateAndConsumeSeat(req.BootstrapToken) {
			log.Printf("[WORKER] Bootstrap failed: invalid or expired seat token")
			return e.JSON(http.StatusUnauthorized, map[string]string{"error": "Invalid or expired bootstrap token"})
		}

		workerRecord, err := workerSvc.RegisterWorker("unknown", "pending")
		if err != nil {
			log.Printf("[WORKER] Failed to register worker placeholder: %v", err)
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to create worker record"})
		}
		workerID := workerRecord.Id

		signed, err := pkiSvc.SignCSR([]byte(req.CSR), workerID)
		if err != nil {
			log.Printf("[WORKER] Failed to sign CSR for worker %s: %v", workerID, err)
			workerSvc.RevokeWorker(workerID) // cleanup
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to sign CSR"})
		}

		workerRecord.Set("cert_not_after", signed.NotAfter)
		workerRecord.Set("cert_serial", signed.Serial)
		if saveErr := app.Save(workerRecord); saveErr != nil {
			log.Printf("[WORKER] Failed to save cert metadata for worker %s: %v", workerID, saveErr)
		}

		caCertPEM, err := pkiSvc.GetCACertPEM()
		if err != nil {
			log.Printf("[WORKER] Failed to get CA cert: %v", err)
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to retrieve CA cert"})
		}

		log.Printf("[WORKER] Successfully exchanged bootstrap token for certificate. Worker ID: %s", workerID)

		return e.JSON(http.StatusOK, map[string]string{
			"worker_id":   workerID,
			"worker_cert": string(signed.CertPEM),
			"ca_cert":     string(caCertPEM),
		})
	})

	// POST /workers/:id/revoke
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

		mtlsServer.DisconnectWorker(workerID)

		return e.JSON(http.StatusOK, map[string]string{"status": "revoked"})
	})

	// POST /pki/renew-server
	r.POST("/api/custom/pki/renew-server", func(e *core.RequestEvent) error {
		if !e.HasSuperuserAuth() {
			return e.JSON(http.StatusUnauthorized, map[string]string{"error": "Unauthorized. Admin only."})
		}

		if err := pkiSvc.RenewServerCert(); err != nil {
			log.Printf("[PKI] Manual server cert renewal failed: %v", err)
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to renew server certificate"})
		}

		return e.JSON(http.StatusOK, map[string]string{"status": "renewed"})
	})

	// POST /workers/:id/renew-cert
	r.POST("/api/custom/workers/{id}/renew-cert", func(e *core.RequestEvent) error {
		if !e.HasSuperuserAuth() {
			return e.JSON(http.StatusUnauthorized, map[string]string{"error": "Unauthorized. Admin only."})
		}

		workerID := e.Request.PathValue("id")
		if !mtlsServer.IsConnected(workerID) {
			return e.JSON(http.StatusConflict, map[string]string{"error": "Worker is not connected"})
		}

		if err := mtlsServer.SendMessage(workerID, protocol.MsgRequestRenewal, nil); err != nil {
			log.Printf("[WORKER] Failed to send renewal request to worker %s: %v", workerID, err)
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to send renewal request"})
		}

		return e.JSON(http.StatusOK, map[string]string{"status": "renewal_requested"})
	})

	// POST /workers/:id/force-rebootstrap
	r.POST("/api/custom/workers/{id}/force-rebootstrap", func(e *core.RequestEvent) error {
		if !e.HasSuperuserAuth() {
			return e.JSON(http.StatusUnauthorized, map[string]string{"error": "Unauthorized. Admin only."})
		}

		workerID := e.Request.PathValue("id")

		record, err := app.FindRecordById("workers", workerID)
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "Worker not found"})
		}
		if record.GetString("fingerprint") == "embedded" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "Cannot force re-bootstrap on the embedded worker."})
		}

		if mtlsServer.IsConnected(workerID) {
			_ = mtlsServer.SendMessage(workerID, protocol.MsgForceRebootstrap, nil)
			mtlsServer.DisconnectWorker(workerID)
		}

		if err := workerSvc.RevokeWorker(workerID); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to revoke worker"})
		}

		return e.JSON(http.StatusOK, map[string]string{"status": "rebootstrap_initiated"})
	})
}
