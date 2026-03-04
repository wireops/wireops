package routes

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/wireops/wireops/internal/agent"
	"github.com/wireops/wireops/internal/pki"
	"github.com/wireops/wireops/internal/sync"
)

func RegisterAgentRoutes(r *router.Router[*core.RequestEvent], app core.App, agentSvc *agent.Service, pkiSvc *pki.Service, dispatcher sync.AgentDispatcher, mtlsServer *agent.MTLSServer) {

	// POST /agent/seat
	r.POST("/api/custom/agent/seat", func(e *core.RequestEvent) error {
		// Require admin auth for generating seats
		if !e.HasSuperuserAuth() {
			return e.JSON(http.StatusUnauthorized, map[string]string{"error": "Unauthorized. Admin only."})
		}

		token, err := agentSvc.GenerateSeat()
		if err != nil {
			log.Printf("[AGENT] Error generating seat: %v", err)
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to generate seat"})
		}

		return e.JSON(http.StatusOK, map[string]string{"seat": token})
	})

	// GET /agents
	r.GET("/api/custom/agents", func(e *core.RequestEvent) error {
		// Require admin auth
		if !e.HasSuperuserAuth() {
			return e.JSON(http.StatusUnauthorized, map[string]string{"error": "Unauthorized. Admin only."})
		}

		records, err := app.FindAllRecords("agents")
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		result := make([]map[string]interface{}, 0, len(records))
		for _, rec := range records {
			var history []agent.HealthEvent
			_ = rec.UnmarshalJSONField("health_history", &history)
			if history == nil {
				history = []agent.HealthEvent{}
			}

			status := rec.GetString("status")
			// Dynamic override based on live websocket connection
			if status == "ACTIVE" && !dispatcher.IsConnected(rec.Id) {
				status = "OFFLINE"
			}

			result = append(result, map[string]interface{}{
				"id":             rec.Id,
				"hostname":       rec.GetString("hostname"),
				"fingerprint":    rec.GetString("fingerprint"),
				"status":         status,
				"last_seen":      rec.GetDateTime("last_seen").String(),
				"health_history": history,
				"tags":           mtlsServer.GetAgentTags(rec.Id),
			})
		}

		return e.JSON(http.StatusOK, result)
	})

	// GET /settings/pki
	r.GET("/api/custom/settings/pki", func(e *core.RequestEvent) error {
		// Require admin auth
		if !e.HasSuperuserAuth() {
			return e.JSON(http.StatusUnauthorized, map[string]string{"error": "Unauthorized. Admin only."})
		}

		details, err := pkiSvc.GetPKIDetails()
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		return e.JSON(http.StatusOK, details)
	})

	// POST /bootstrap
	r.POST("/api/custom/agent/bootstrap", func(e *core.RequestEvent) error {
		var req struct {
			BootstrapToken string `json:"bootstrap_token"`
			CSR            string `json:"csr"` // PEM encoded
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&req); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid JSON body"})
		}

		// Validate Seat
		if !agentSvc.ValidateAndConsumeSeat(req.BootstrapToken) {
			log.Printf("[AGENT] Bootstrap failed: invalid or expired seat token")
			return e.JSON(http.StatusUnauthorized, map[string]string{"error": "Invalid or expired bootstrap token"})
		}

		// Since we don't know the hostname yet in bootstrap, we just create the identity
		// Pocketbase uses 15 chars for ID, we can create a record here to reserve the ID
		// or generate a UUID. Let's create an empty record to get an ID.
		agentRecord, err := agentSvc.RegisterAgent("unknown", "pending")
		if err != nil {
			log.Printf("[AGENT] Failed to register agent placeholder: %v", err)
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to create agent record"})
		}
		agentID := agentRecord.Id

		// Sign CSR
		certPEM, err := pkiSvc.SignCSR([]byte(req.CSR), agentID)
		if err != nil {
			log.Printf("[AGENT] Failed to sign CSR for agent %s: %v", agentID, err)
			agentSvc.RevokeAgent(agentID) // cleanup
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to sign CSR"})
		}

		// Also get the CA cert
		caCertPEM, err := pkiSvc.GetCACertPEM()
		if err != nil {
			log.Printf("[AGENT] Failed to get CA cert: %v", err)
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to retrieve CA cert"})
		}

		log.Printf("[AGENT] Successfully exchanged bootstrap token for certificate. Agent ID: %s", agentID)

		return e.JSON(http.StatusOK, map[string]string{
			"agent_id":   agentID,
			"agent_cert": string(certPEM),
			"ca_cert":    string(caCertPEM),
		})
	})

	// POST /agents/:id/revoke
	r.POST("/api/custom/agents/{id}/revoke", func(e *core.RequestEvent) error {
		if !e.HasSuperuserAuth() {
			return e.JSON(http.StatusUnauthorized, map[string]string{"error": "Unauthorized. Admin only."})
		}

		agentID := e.Request.PathValue("id")

		record, err := app.FindRecordById("agents", agentID)
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "Agent not found"})
		}

		if record.GetString("fingerprint") == "embedded" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "Cannot revoke the embedded agent."})
		}

		stacks, err := app.FindAllRecords("stacks", dbx.HashExp{"agent": agentID})
		if err != nil && err.Error() != "sql: no rows in result set" {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to query stacks: " + err.Error()})
		}

		if len(stacks) > 0 {
			return e.JSON(http.StatusConflict, map[string]string{
				"error": "This agent has active stacks registered to it. Reassign or delete the stacks before revoking.",
			})
		}

		if err := agentSvc.RevokeAgent(agentID); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to revoke agent"})
		}

		// Immediately drop the active WebSocket connection, if any.
		mtlsServer.DisconnectAgent(agentID)

		return e.JSON(http.StatusOK, map[string]string{"status": "revoked"})
	})

}
