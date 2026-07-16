package routes

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/wireops/wireops/internal/audit"
	"github.com/wireops/wireops/internal/config"
	"github.com/wireops/wireops/internal/job"
	"github.com/wireops/wireops/internal/policy"
	"github.com/wireops/wireops/internal/rbac"
	"github.com/wireops/wireops/internal/sync"
	"github.com/wireops/wireops/internal/worker"
)

type workerJobSummary struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	CommonTags []string `json:"common_tags"`

	tags            []string
	definitionError string
}

func RegisterWorkerRoutes(r *router.Router[*core.RequestEvent], app core.App, workerSvc *worker.Service, dispatcher sync.WorkerDispatcher, workerServer *worker.WorkerServer) {

	r.POST("/api/custom/worker/tokens", func(e *core.RequestEvent) error {
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
	}).BindFunc(rbac.Require(rbac.CapManageSecurity))

	r.GET("/api/custom/workers", func(e *core.RequestEvent) error {
		records, err := app.FindAllRecords("workers")
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		jobCatalog, err := buildWorkerJobCatalog(app)
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		workerJobRunMap := make(map[string]map[string]bool)
		if len(jobCatalog) > 0 {
			jobIDs := make([]any, len(jobCatalog))
			for i, item := range jobCatalog {
				jobIDs[i] = item.ID
			}

			type runPair struct {
				Worker string `db:"worker"`
				Job    string `db:"job"`
			}
			var pairs []runPair

			query := app.DB().Select("worker", "job").
				From("job_runs").
				Where(dbx.In("job", jobIDs...)).
				Distinct(true)

			if err := query.All(&pairs); err == nil {
				for _, p := range pairs {
					if workerJobRunMap[p.Worker] == nil {
						workerJobRunMap[p.Worker] = make(map[string]bool)
					}
					workerJobRunMap[p.Worker][p.Job] = true
				}
			}
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

			tags := workerServer.GetWorkerTags(rec.Id)
			jobs := workerJobsFor(jobCatalog, tags, workerJobRunMap[rec.Id])

			result = append(result, map[string]interface{}{
				"id":              rec.Id,
				"hostname":        rec.GetString("hostname"),
				"status":          status,
				"last_seen":       rec.GetDateTime("last_seen").String(),
				"health_history":  history,
				"tags":            tags,
				"token_status":    tokenStatus,
				"token_expires":   expiresAt,
				"token_last_used": lastUsedAt,
				"job_count":       len(jobs),
				"jobs":            jobs,
				"version":         rec.GetString("version"),
				"docker_version":  rec.GetString("docker_version"),
				"compose_version": rec.GetString("compose_version"),
				"os":              rec.GetString("os"),
				"arch":            rec.GetString("arch"),
				"cpu_usage":       rec.Get("cpu_usage"),
				"memory_usage":    rec.Get("memory_usage"),
				"disk_usage":      rec.Get("disk_usage"),
				"docker_online":   rec.GetBool("docker_online"),
			})
		}

		return e.JSON(http.StatusOK, result)
	}).BindFunc(rbac.Require(rbac.CapViewWorkers))

	r.POST("/api/custom/workers/{id}/revoke", func(e *core.RequestEvent) error {
		workerID := e.Request.PathValue("id")

		if _, err := app.FindRecordById("workers", workerID); err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "Worker not found"})
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
	}).BindFunc(rbac.Require(rbac.CapManageWorkers))

	// --- Per-worker policy ---

	// GET /api/custom/workers/{id}/policy
	// Returns the effective resolved policy for this worker plus its local override fields.
	r.GET("/api/custom/workers/{id}/policy", func(e *core.RequestEvent) error {
		workerID := e.Request.PathValue("id")
		rec, err := app.FindRecordById("workers", workerID)
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "Worker not found"})
		}

		inherit := true
		inheritRaw := rec.Get("policy_inherit")
		if inheritRaw != nil {
			inherit = rec.GetBool("policy_inherit")
		}

		var localVolumes, localNetworks, localImages, localCapAdd, localDevices, localSecurityOpt *[]string
		_ = rec.UnmarshalJSONField("policy_volumes", &localVolumes)
		_ = rec.UnmarshalJSONField("policy_networks", &localNetworks)
		_ = rec.UnmarshalJSONField("policy_images", &localImages)
		_ = rec.UnmarshalJSONField("policy_cap_add", &localCapAdd)
		_ = rec.UnmarshalJSONField("policy_devices", &localDevices)
		_ = rec.UnmarshalJSONField("policy_security_opt", &localSecurityOpt)

		// Read nullable boolean overrides from policy_flags.
		type flagsType struct {
			PreventLatestImages *bool `json:"prevent_latest_images"`
			BlockHostVolumes    *bool `json:"block_host_volumes"`
			BlockPrivileged     *bool `json:"block_privileged"`
			BlockHostNetwork    *bool `json:"block_host_network"`
			BlockHostPID        *bool `json:"block_host_pid"`
			BlockHostIPC        *bool `json:"block_host_ipc"`
			BlockDockerSocket   *bool `json:"block_docker_socket"`
		}
		var flagsOut flagsType
		if raw := rec.GetString("policy_flags"); raw != "" {
			_ = json.Unmarshal([]byte(raw), &flagsOut)
		}

		effective, err := policy.Load(app, workerID)
		if err != nil || effective == nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to load effective policy"})
		}

		return e.JSON(http.StatusOK, map[string]interface{}{
			"inherit":               inherit,
			"allowed_volumes":       localVolumes,
			"allowed_networks":      localNetworks,
			"allowed_images":        localImages,
			"allowed_cap_add":       localCapAdd,
			"allowed_devices":       localDevices,
			"allowed_security_opt":  localSecurityOpt,
			"prevent_latest_images": flagsOut.PreventLatestImages,
			"block_host_volumes":    flagsOut.BlockHostVolumes,
			"block_privileged":      flagsOut.BlockPrivileged,
			"block_host_network":    flagsOut.BlockHostNetwork,
			"block_host_pid":        flagsOut.BlockHostPID,
			"block_host_ipc":        flagsOut.BlockHostIPC,
			"block_docker_socket":   flagsOut.BlockDockerSocket,
			"effective":             effective.ToJSON(),
		})
	}).BindFunc(rbac.Require(rbac.CapManageSettings))

	// PUT /api/custom/workers/{id}/policy
	// Saves or updates the per-worker policy override.
	r.PUT("/api/custom/workers/{id}/policy", func(e *core.RequestEvent) error {
		workerID := e.Request.PathValue("id")
		rec, err := app.FindRecordById("workers", workerID)
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "Worker not found"})
		}

		var body policy.WorkerPolicyOverrideJSON
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid JSON body"})
		}

		rec.Set("policy_inherit", body.Inherit)
		rec.Set("policy_volumes", body.AllowedVolumes)
		rec.Set("policy_networks", body.AllowedNetworks)
		rec.Set("policy_images", body.AllowedImages)
		rec.Set("policy_cap_add", body.AllowedCapAdd)
		rec.Set("policy_devices", body.AllowedDevices)
		rec.Set("policy_security_opt", body.AllowedSecurityOpt)

		// Persist nullable boolean flags as a JSON object.
		type flagsPayload struct {
			PreventLatestImages *bool `json:"prevent_latest_images"`
			BlockHostVolumes    *bool `json:"block_host_volumes"`
			BlockPrivileged     *bool `json:"block_privileged"`
			BlockHostNetwork    *bool `json:"block_host_network"`
			BlockHostPID        *bool `json:"block_host_pid"`
			BlockHostIPC        *bool `json:"block_host_ipc"`
			BlockDockerSocket   *bool `json:"block_docker_socket"`
		}
		flags := flagsPayload{
			PreventLatestImages: body.PreventLatestImages,
			BlockHostVolumes:    body.BlockHostVolumes,
			BlockPrivileged:     body.BlockPrivileged,
			BlockHostNetwork:    body.BlockHostNetwork,
			BlockHostPID:        body.BlockHostPID,
			BlockHostIPC:        body.BlockHostIPC,
			BlockDockerSocket:   body.BlockDockerSocket,
		}
		if flagsJSON, err := json.Marshal(flags); err == nil {
			rec.Set("policy_flags", string(flagsJSON))
		}

		if err := app.Save(rec); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to save worker policy: " + err.Error()})
		}
		return e.JSON(http.StatusOK, map[string]string{"status": "saved"})
	}).BindFunc(rbac.Require(rbac.CapManageSettings))

	// DELETE /api/custom/workers/{id}/policy
	// Resets the per-worker policy to inherit-from-global (clears local overrides).
	r.DELETE("/api/custom/workers/{id}/policy", func(e *core.RequestEvent) error {
		workerID := e.Request.PathValue("id")
		rec, err := app.FindRecordById("workers", workerID)
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "Worker not found"})
		}

		rec.Set("policy_inherit", true)
		rec.Set("policy_volumes", []string{})
		rec.Set("policy_networks", []string{})
		rec.Set("policy_images", []string{})
		rec.Set("policy_cap_add", []string{})
		rec.Set("policy_devices", []string{})
		rec.Set("policy_security_opt", []string{})
		rec.Set("policy_flags", "")

		if err := app.Save(rec); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to reset worker policy: " + err.Error()})
		}
		return e.JSON(http.StatusOK, map[string]string{"status": "reset"})
	}).BindFunc(rbac.Require(rbac.CapManageSettings))

	// --- Global worker policy (Settings) ---

	// GET /api/custom/settings/worker-policy
	r.GET("/api/custom/settings/worker-policy", func(e *core.RequestEvent) error {
		p, err := policy.LoadGlobal(app)
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return e.JSON(http.StatusOK, p.ToJSON())
	}).BindFunc(rbac.Require(rbac.CapManageSettings))

	// PUT /api/custom/settings/worker-policy
	r.PUT("/api/custom/settings/worker-policy", func(e *core.RequestEvent) error {
		var body policy.PolicyJSON
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid JSON body"})
		}
		if body.AllowedVolumes == nil {
			body.AllowedVolumes = []string{}
		}
		if body.AllowedNetworks == nil {
			body.AllowedNetworks = []string{}
		}
		if body.AllowedImages == nil {
			body.AllowedImages = []string{}
		}
		if body.AllowedCapAdd == nil {
			body.AllowedCapAdd = []string{}
		}
		if body.AllowedDevices == nil {
			body.AllowedDevices = []string{}
		}
		if body.AllowedSecurityOpt == nil {
			body.AllowedSecurityOpt = []string{}
		}

		records, err := app.FindAllRecords("worker_policies")
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		var rec *core.Record
		if len(records) > 0 {
			rec = records[0]
		} else {
			col, err := app.FindCollectionByNameOrId("worker_policies")
			if err != nil {
				return e.JSON(http.StatusInternalServerError, map[string]string{"error": "worker_policies collection not found"})
			}
			rec = core.NewRecord(col)
		}

		rec.Set("enabled", body.Enabled)
		rec.Set("allowed_volumes", body.AllowedVolumes)
		rec.Set("allowed_networks", body.AllowedNetworks)
		rec.Set("allowed_images", body.AllowedImages)
		rec.Set("allowed_cap_add", body.AllowedCapAdd)
		rec.Set("allowed_devices", body.AllowedDevices)
		rec.Set("allowed_security_opt", body.AllowedSecurityOpt)
		rec.Set("prevent_latest_images", body.PreventLatestImages)
		rec.Set("block_host_volumes", body.BlockHostVolumes)
		rec.Set("block_privileged", body.BlockPrivileged)
		rec.Set("block_host_network", body.BlockHostNetwork)
		rec.Set("block_host_pid", body.BlockHostPID)
		rec.Set("block_host_ipc", body.BlockHostIPC)
		rec.Set("block_docker_socket", body.BlockDockerSocket)

		if err := app.Save(rec); err != nil {
			log.Printf("[POLICY] Failed to save global worker policy: %v", err)
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to save global policy: " + err.Error()})
		}
		return e.JSON(http.StatusOK, map[string]string{"status": "saved"})
	}).BindFunc(rbac.Require(rbac.CapManageSettings))

	// --- App Settings ---

	// GET /api/custom/settings/app-settings
	r.GET("/api/custom/settings/app-settings", func(e *core.RequestEvent) error {
		records, err := app.FindAllRecords("app_settings")
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		if len(records) > 0 {
			rec := records[0]
			return e.JSON(http.StatusOK, map[string]interface{}{
				"id":                     rec.Id,
				"timezone":               rec.GetString("timezone"),
				"audit_retention_days":   audit.AuditRetentionDays(app),
				"job_run_retention_days": audit.JobRunRetentionDays(app),
				"sso_groups_claim":       rec.GetString("sso_groups_claim"),
			})
		}
		return e.JSON(http.StatusOK, map[string]interface{}{
			"id":                     "",
			"timezone":               "",
			"audit_retention_days":   audit.DefaultAuditRetentionDays,
			"job_run_retention_days": audit.DefaultJobRunRetentionDays,
			"sso_groups_claim":       "groups",
		})
	}).BindFunc(rbac.Require(rbac.CapManageSettings))

	// PUT /api/custom/settings/app-settings
	r.PUT("/api/custom/settings/app-settings", func(e *core.RequestEvent) error {
		var body struct {
			Timezone            *string `json:"timezone"`
			AuditRetentionDays  *int    `json:"audit_retention_days"`
			JobRunRetentionDays *int    `json:"job_run_retention_days"`
			SSOGroupsClaim      *string `json:"sso_groups_claim"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid JSON body"})
		}

		if body.Timezone != nil && *body.Timezone != "" {
			if _, err := time.LoadLocation(*body.Timezone); err != nil {
				return e.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid timezone"})
			}
		}
		if err := validateRetentionDays(body.AuditRetentionDays, body.JobRunRetentionDays); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}

		records, err := app.FindAllRecords("app_settings")
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		var rec *core.Record
		if len(records) > 0 {
			rec = records[0]
		} else {
			col, err := app.FindCollectionByNameOrId("app_settings")
			if err != nil {
				return e.JSON(http.StatusInternalServerError, map[string]string{"error": "collection not found"})
			}
			rec = core.NewRecord(col)
		}

		if body.Timezone != nil {
			rec.Set("timezone", *body.Timezone)
		}
		if body.AuditRetentionDays != nil {
			rec.Set("audit_retention_days", *body.AuditRetentionDays)
		} else if rec.GetInt("audit_retention_days") <= 0 {
			rec.Set("audit_retention_days", audit.DefaultAuditRetentionDays)
		}
		if body.JobRunRetentionDays != nil {
			rec.Set("job_run_retention_days", *body.JobRunRetentionDays)
		} else if rec.GetInt("job_run_retention_days") <= 0 {
			rec.Set("job_run_retention_days", audit.DefaultJobRunRetentionDays)
		}
		if body.SSOGroupsClaim != nil {
			if *body.SSOGroupsClaim != rec.GetString("sso_groups_claim") && !rbac.Can(e, rbac.CapManageSecurity) {
				return e.JSON(http.StatusForbidden, map[string]string{"error": "CapManageSecurity is required to modify sso_groups_claim"})
			}
			rec.Set("sso_groups_claim", *body.SSOGroupsClaim)
		} else if rec.GetString("sso_groups_claim") == "" {
			rec.Set("sso_groups_claim", "groups")
		}

		if err := app.Save(rec); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to save app settings: " + err.Error()})
		}
		return e.JSON(http.StatusOK, map[string]interface{}{
			"id":                     rec.Id,
			"timezone":               rec.GetString("timezone"),
			"audit_retention_days":   audit.AuditRetentionDays(app),
			"job_run_retention_days": audit.JobRunRetentionDays(app),
			"sso_groups_claim":       rec.GetString("sso_groups_claim"),
		})
	}).BindFunc(rbac.Require(rbac.CapManageSettings))
}

func validateRetentionDays(auditDays, jobRunDays *int) error {
	if auditDays != nil && *auditDays < 1 {
		return errors.New("Retention days must be at least 1")
	}
	if jobRunDays != nil && *jobRunDays < 1 {
		return errors.New("Retention days must be at least 1")
	}
	return nil
}

func buildWorkerJobCatalog(app core.App) ([]workerJobSummary, error) {
	records, err := app.FindAllRecords("scheduled_jobs")
	if err != nil {
		return nil, err
	}

	repoWorkspace := config.GetReposWorkspace()
	items := make([]workerJobSummary, 0, len(records))

	for _, rec := range records {
		repoID := rec.GetString("repository")

		item := workerJobSummary{
			ID:   rec.Id,
			Name: rec.GetString("name"),
		}

		jobFile := rec.GetString("job_file")
		def, err := job.ParseJobFile(repoWorkspace, repoID, jobFile)
		if err != nil {
			item.definitionError = err.Error()
		} else {
			item.Name = def.Name
			item.tags = def.Tags
		}

		if item.Name == "" {
			item.Name = jobFile
		}

		items = append(items, item)
	}

	return items, nil
}

func workerJobsFor(catalog []workerJobSummary, workerTags []string, workerRunHistory map[string]bool) []workerJobSummary {
	jobs := make([]workerJobSummary, 0)
	for _, item := range catalog {
		hasRunHistory := workerRunHistory != nil && workerRunHistory[item.ID]
		matchedByTags := item.definitionError == "" && workerMatchesJobTags(workerTags, item.tags)
		if !matchedByTags && !hasRunHistory {
			continue
		}

		item.CommonTags = workerCommonJobTags(workerTags, item.tags)
		jobs = append(jobs, item)
	}
	return jobs
}

func workerMatchesJobTags(workerTags, requiredTags []string) bool {
	if len(requiredTags) == 0 {
		return true
	}

	tagSet := make(map[string]struct{}, len(workerTags))
	for _, tag := range workerTags {
		tagSet[tag] = struct{}{}
	}

	for _, required := range requiredTags {
		if _, ok := tagSet[required]; !ok {
			return false
		}
	}
	return true
}

func workerCommonJobTags(workerTags, jobTags []string) []string {
	if len(workerTags) == 0 || len(jobTags) == 0 {
		return []string{}
	}

	workerTagSet := make(map[string]struct{}, len(workerTags))
	for _, tag := range workerTags {
		workerTagSet[tag] = struct{}{}
	}

	common := make([]string, 0)
	seen := map[string]struct{}{}
	for _, tag := range jobTags {
		if _, ok := workerTagSet[tag]; !ok {
			continue
		}
		if _, duplicate := seen[tag]; duplicate {
			continue
		}
		common = append(common, tag)
		seen[tag] = struct{}{}
	}
	return common
}
