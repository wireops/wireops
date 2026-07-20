package routes

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/wireops/wireops/internal/compose"
	"github.com/wireops/wireops/internal/config"
	"github.com/wireops/wireops/internal/crypto"
	"github.com/wireops/wireops/internal/envvars"
	"github.com/wireops/wireops/internal/manifest"
	"github.com/wireops/wireops/internal/policy"
	"github.com/wireops/wireops/internal/protocol"
	"github.com/wireops/wireops/internal/rbac"
	"github.com/wireops/wireops/internal/safepath"
	"github.com/wireops/wireops/internal/secrets"
	"github.com/wireops/wireops/internal/sync"
	"github.com/wireops/wireops/internal/webhook"
)

func (rr routeRegistrar) registerStackTriggerRoutes() {
	rr.r.POST("/api/custom/stacks/{id}/sync", func(e *core.RequestEvent) error {
		id := e.Request.PathValue("id")
		if id == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "missing id"})
		}
		if !rr.workerOnline(e, id) {
			return nil
		}
		var userID string
		if e.Auth != nil {
			userID = e.Auth.Id
		}
		rr.scheduler.TriggerSync(id, "manual", 0, userID)
		return e.JSON(http.StatusOK, map[string]string{"status": "triggered"})
	}).BindFunc(rbac.Require(rbac.CapOperateStacks))

	rr.r.POST("/api/custom/stacks/{id}/rollback", func(e *core.RequestEvent) error {
		id := e.Request.PathValue("id")
		var body struct {
			CommitSHA string `json:"commit_sha"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil || body.CommitSHA == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "commit_sha required"})
		}
		if !rr.workerOnline(e, id) {
			return nil
		}
		var userID string
		if e.Auth != nil {
			userID = e.Auth.Id
		}
		rr.scheduler.TriggerRollback(id, body.CommitSHA, userID)
		return e.JSON(http.StatusOK, map[string]string{"status": "triggered"})
	}).BindFunc(rbac.Require(rbac.CapOperateStacks))

	rr.r.GET("/api/custom/stacks/{id}/webhook-url", func(e *core.RequestEvent) error {
		id := e.Request.PathValue("id")
		if id == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "missing id"})
		}
		return e.JSON(http.StatusOK, map[string]string{"webhook_url": config.GetWebhookURL(id)})
	}).BindFunc(rbac.Require(rbac.CapOperateStacks))

	rr.r.POST("/api/custom/webhook/{id}", func(e *core.RequestEvent) error {
		id := e.Request.PathValue("id")
		if id == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "missing id"})
		}

		stack, err := rr.app.FindRecordById("stacks", id)
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "stack not found"})
		}

		encryptedSecret := stack.GetString("webhook_secret")
		if encryptedSecret == "" {
			return e.JSON(http.StatusForbidden, map[string]string{"error": "webhook not configured"})
		}

		body, err := io.ReadAll(e.Request.Body)
		if err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
		}

		secretKey := crypto.NormalizeSecretKey(os.Getenv("SECRET_KEY"))
		plainSecret, err := crypto.Decrypt(encryptedSecret, secretKey)
		if err != nil {
			log.Printf("[webhook] failed to decrypt secret for stack %s: %v", id, err)
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "webhook misconfigured"})
		}

		if !webhook.VerifySignature(string(plainSecret), body, e.Request.Header.Get(webhook.GitHubSignatureHeader)) {
			return e.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid signature"})
		}

		ref, ok := webhook.RefFromPayload(body)
		if !ok {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "malformed payload"})
		}

		if ref != "" {
			if repo, err := rr.app.FindRecordById("repositories", stack.GetString("repository")); err == nil {
				branch := repo.GetString("branch")
				if branch == "" {
					branch = "main"
				}
				if webhook.BranchFromRef(ref) != branch {
					return e.JSON(http.StatusOK, map[string]string{"status": "skipped", "reason": "branch_mismatch"})
				}
			}
		}

		rr.scheduler.TriggerSync(id, "webhook", 0, "webhook")
		return e.JSON(http.StatusOK, map[string]string{"status": "triggered"})
	})
}

func (rr routeRegistrar) registerStackInspectionRoutes() {
	rr.r.POST("/api/custom/stacks/{id}/force-redeploy", func(e *core.RequestEvent) error {
		stackID := e.Request.PathValue("id")
		if stackID == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "missing id"})
		}
		if !rr.workerOnline(e, stackID) {
			return nil
		}
		var body struct {
			RecreateContainers bool `json:"recreate_containers"`
			RecreateVolumes    bool `json:"recreate_volumes"`
			RecreateNetworks   bool `json:"recreate_networks"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
		}
		var userID string
		if e.Auth != nil {
			userID = e.Auth.Id
		}
		// Any persisted render_overrides are reapplied automatically by
		// ForceRedeployStack, same as every other reconcile path — no need to
		// load and pass them explicitly here.
		rr.scheduler.TriggerForceRedeploy(stackID, body.RecreateContainers, body.RecreateVolumes, body.RecreateNetworks, userID)
		return e.JSON(http.StatusOK, map[string]string{"status": "triggered"})
	}).BindFunc(rbac.Require(rbac.CapOperateStacks))

	rr.r.GET("/api/custom/stacks/{id}/render-overrides", func(e *core.RequestEvent) error {
		stackID := e.Request.PathValue("id")
		stack, err := rr.app.FindRecordById("stacks", stackID)
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "stack not found"})
		}

		overrides := sync.LoadRenderOverrides(stack)
		resp := map[string]interface{}{"overrides": overrides}
		if len(overrides) == 0 {
			return e.JSON(http.StatusOK, resp)
		}

		// Best-effort: resolve what each overridden service's image/ports/networks would
		// be from Git alone, so the UI can show a diff against the active override.
		composeFile := stack.GetString("compose_file")
		if composeFile == "" {
			composeFile = "docker-compose.yml"
		}
		if err := safepath.ValidateComposeFile(composeFile); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		envVars, envErr := rr.scheduler.LoadStackEnvVars(e.Request.Context(), stackID)
		if envErr != nil {
			resp["git_error"] = "failed to resolve env vars: " + envErr.Error()
			return e.JSON(http.StatusOK, resp)
		}
		configOut, err := compose.Config(e.Request.Context(), compose.ConfigOptions{
			WorkDir:     stackWorkDir(rr.app, stack),
			ComposeFile: composeFile,
			EnvVars:     envVars,
		}, true)
		if err != nil {
			resp["git_error"] = "failed to resolve Git-defined values: " + err.Error()
			return e.JSON(http.StatusOK, resp)
		}
		configMap, err := compose.ParseConfigJSON(configOut)
		if err != nil {
			resp["git_error"] = "failed to parse Git-defined compose config: " + err.Error()
			return e.JSON(http.StatusOK, resp)
		}
		services, _ := configMap["services"].(map[string]interface{})
		gitValues := map[string]interface{}{}
		for name := range overrides {
			svc, ok := services[name].(map[string]interface{})
			if !ok {
				continue
			}
			image, _ := svc["image"].(string)
			gitValues[name] = map[string]interface{}{
				"image":    image,
				"ports":    composePortsToShortForm(svc["ports"]),
				"networks": composeNetworksToList(svc["networks"]),
			}
		}
		resp["git"] = gitValues
		return e.JSON(http.StatusOK, resp)
	}).BindFunc(rbac.Require(rbac.CapOperateStacks))

	rr.r.PUT("/api/custom/stacks/{id}/render-overrides", func(e *core.RequestEvent) error {
		stackID := e.Request.PathValue("id")
		if stackID == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "missing id"})
		}
		stack, err := rr.app.FindRecordById("stacks", stackID)
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "stack not found"})
		}
		if !rr.workerOnline(e, stackID) {
			return nil
		}

		var body struct {
			Overrides map[string]sync.ServiceOverride `json:"overrides"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
		}
		if len(body.Overrides) == 0 {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "overrides must not be empty"})
		}

		// Validate before persisting: a rejected or unresolvable override must never
		// be written to the stack record, since every future reconcile reapplies
		// whatever is stored there.
		wp, err := policy.Load(rr.app, stack.GetString("worker"))
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to load worker policy"})
		}
		if !wp.AllowRenderOverrides {
			return e.JSON(http.StatusForbidden, map[string]string{"error": "render-time overrides are disabled by the worker policy"})
		}

		composeFile := stack.GetString("compose_file")
		if composeFile == "" {
			composeFile = "docker-compose.yml"
		}
		if err := safepath.ValidateComposeFile(composeFile); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		envVars, err := rr.scheduler.LoadStackEnvVars(e.Request.Context(), stackID)
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to resolve env vars: " + err.Error()})
		}
		configOut, err := compose.Config(e.Request.Context(), compose.ConfigOptions{
			WorkDir:     stackWorkDir(rr.app, stack),
			ComposeFile: composeFile,
			EnvVars:     envVars,
		}, true)
		if err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "failed to resolve compose config: " + err.Error()})
		}
		configMap, err := compose.ParseConfigJSON(configOut)
		if err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "failed to parse compose config: " + err.Error()})
		}
		services, _ := configMap["services"].(map[string]interface{})
		for name := range body.Overrides {
			if _, ok := services[name]; !ok {
				return e.JSON(http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("render override targets unknown service %q", name)})
			}
		}

		// Apply the overrides to the resolved config and run them through the same
		// policy check the renderer applies at deploy time, so a blocked image/network
		// or a :latest tag is rejected here instead of being persisted and only failing
		// (and getting auto-cleared) on the next reconcile.
		if err := sync.ApplyServiceOverrides(configMap, body.Overrides); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		if err := wp.ValidateComposeConfig(configMap); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}

		stack.Set("render_overrides", body.Overrides)
		if err := rr.app.Save(stack); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to save overrides"})
		}

		var userID string
		if e.Auth != nil {
			userID = e.Auth.Id
		}
		// Applying overrides always changes the service definition (image/ports/networks),
		// so force-recreate the containers rather than relying on docker compose's own
		// diffing to notice the change.
		rr.scheduler.TriggerForceRedeploy(stackID, true, false, false, userID)
		return e.JSON(http.StatusOK, map[string]string{"status": "triggered"})
	}).BindFunc(rbac.Require(rbac.CapOperateStacks))

	rr.r.DELETE("/api/custom/stacks/{id}/render-overrides", func(e *core.RequestEvent) error {
		stackID := e.Request.PathValue("id")
		if stackID == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "missing id"})
		}
		stack, err := rr.app.FindRecordById("stacks", stackID)
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "stack not found"})
		}
		if !rr.workerOnline(e, stackID) {
			return nil
		}

		stack.Set("render_overrides", nil)
		if err := rr.app.Save(stack); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to clear overrides"})
		}

		var userID string
		if e.Auth != nil {
			userID = e.Auth.Id
		}
		// Reverting to Git state changes the service definition back, same as applying
		// overrides — force-recreate so the revert is guaranteed to take effect.
		rr.scheduler.TriggerForceRedeploy(stackID, true, false, false, userID)
		return e.JSON(http.StatusOK, map[string]string{"status": "triggered"})
	}).BindFunc(rbac.Require(rbac.CapOperateStacks))

	rr.r.GET("/api/custom/stacks/{id}/services", func(e *core.RequestEvent) error {
		stackID := e.Request.PathValue("id")
		stack, err := rr.app.FindRecordById("stacks", stackID)
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "stack not found"})
		}
		workerID := stack.GetString("worker")
		worker, err := rr.resolveWorker(workerID)
		if err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		if rr.workerSvc == nil || !rr.workerSvc.IsConnected(workerID) {
			return e.JSON(http.StatusServiceUnavailable, map[string]string{"error": fmt.Sprintf(OfflineWorkerMsg, worker.GetString("hostname"))})
		}

		projectName := compose.ProjectName(stackWorkDir(rr.app, stack))
		res, dispatchErr := rr.workerSvc.Dispatch(e.Request.Context(), workerID, protocol.GetStatusCommand{
			CommandID:   fmt.Sprintf("status-%s", stackID),
			ProjectName: projectName,
		})
		if dispatchErr == nil && res.Error == "" {
			var statuses []compose.ServiceStatus
			if err := json.Unmarshal([]byte(res.Output), &statuses); err == nil {
				result := make([]map[string]interface{}, 0, len(statuses))
				for _, s := range statuses {
					result = append(result, map[string]interface{}{
						"service_name":   s.ServiceName,
						"status":         s.Status,
						"container_id":   s.ContainerID,
						"container_name": s.ContainerName,
						"ports":          s.Ports,
					})
				}
				return e.JSON(http.StatusOK, result)
			}
		}

		services, err := rr.app.FindAllRecords("stack_services", dbx.HashExp{"stack": stackID})
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		result := make([]map[string]interface{}, 0, len(services))
		for _, s := range services {
			// No "ports" key here: stack_services has no ports column, so
			// this DB-backed fallback (worker offline) has no port data to
			// report. Omitting the key lets the frontend tell "unknown"
			// apart from "genuinely no ports published" — don't add a
			// fabricated empty array.
			result = append(result, map[string]interface{}{
				"service_name":   s.GetString("service_name"),
				"status":         s.GetString("status"),
				"container_id":   s.GetString("container_id"),
				"container_name": s.GetString("container_name"),
			})
		}
		return e.JSON(http.StatusOK, result)
	}).BindFunc(rbac.Require(rbac.CapViewStacks))

	rr.r.GET("/api/custom/stacks/{id}/resources", func(e *core.RequestEvent) error {
		stackID := e.Request.PathValue("id")
		stack, err := rr.app.FindRecordById("stacks", stackID)
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "stack not found"})
		}

		empty := protocol.GetResourcesResult{
			Volumes:  []protocol.VolumeInfo{},
			Networks: []protocol.NetworkInfo{},
		}

		workerID := stack.GetString("worker")
		worker, err := rr.resolveWorker(workerID)
		if err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		if rr.workerSvc == nil || !rr.workerSvc.IsConnected(workerID) {
			return e.JSON(http.StatusServiceUnavailable, map[string]string{"error": fmt.Sprintf(OfflineWorkerMsg, worker.GetString("hostname"))})
		}

		projectName := compose.ProjectName(stackWorkDir(rr.app, stack))
		result, dispatchErr := rr.workerSvc.Dispatch(e.Request.Context(), workerID, protocol.GetResourcesCommand{
			CommandID:   fmt.Sprintf("resources-%s", stackID),
			StackID:     stackID,
			ProjectName: projectName,
		})
		if dispatchErr != nil {
			log.Printf("[routes] get_resources dispatch error stack=%s: %v", stackID, dispatchErr)
			return e.JSON(http.StatusOK, empty)
		}
		if result.Error != "" {
			log.Printf("[routes] get_resources worker error stack=%s: %s", stackID, result.Error)
			return e.JSON(http.StatusOK, empty)
		}
		var resources protocol.GetResourcesResult
		if err := json.Unmarshal([]byte(result.Output), &resources); err != nil {
			log.Printf("[routes] get_resources decode error stack=%s: %v", stackID, err)
			return e.JSON(http.StatusOK, empty)
		}
		return e.JSON(http.StatusOK, resources)
	}).BindFunc(rbac.Require(rbac.CapViewStacks))
}

func (rr routeRegistrar) registerContainerReadRoutes() {
	rr.r.GET("/api/custom/stacks/{id}/container/{containerId}/stats", func(e *core.RequestEvent) error {
		stackID := e.Request.PathValue("id")
		containerID := e.Request.PathValue("containerId")
		if containerID == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "missing containerId"})
		}

		_, projectName, workerID, ok := rr.resolveStackAndWorker(e, stackID)
		if !ok {
			return nil
		}

		res, dispatchErr := rr.workerSvc.Dispatch(e.Request.Context(), workerID, protocol.GetContainerStatsCommand{
			CommandID:   fmt.Sprintf("stats-container-%s", containerID),
			StackID:     stackID,
			ProjectName: projectName,
			ContainerID: containerID,
		})
		if dispatchErr != nil {
			return e.JSON(http.StatusServiceUnavailable, map[string]string{"error": dispatchErr.Error()})
		}
		if res.Error != "" {
			status := http.StatusInternalServerError
			if strings.Contains(strings.ToLower(res.Error), "does not belong to stack") {
				status = http.StatusForbidden
			}
			return e.JSON(status, map[string]string{"error": res.Error})
		}

		var stats compose.ContainerStats
		if err := json.Unmarshal([]byte(res.Output), &stats); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to decode stats: " + err.Error()})
		}
		return e.JSON(http.StatusOK, stats)
	}).BindFunc(rbac.Require(rbac.CapViewStacks))

	rr.r.GET("/api/custom/stacks/{id}/container/{containerId}/logs", func(e *core.RequestEvent) error {
		stackID := e.Request.PathValue("id")
		containerID := e.Request.PathValue("containerId")
		if containerID == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "missing containerId"})
		}

		_, projectName, workerID, ok := rr.resolveStackAndWorker(e, stackID)
		if !ok {
			return nil
		}

		tail := e.Request.URL.Query().Get("tail")
		if tail == "" {
			tail = "100"
		}

		res, dispatchErr := rr.workerSvc.Dispatch(e.Request.Context(), workerID, protocol.GetContainerLogsCommand{
			CommandID:   fmt.Sprintf("logs-container-%s", containerID),
			StackID:     stackID,
			ProjectName: projectName,
			ContainerID: containerID,
			Tail:        tail,
		})
		if dispatchErr != nil {
			return e.JSON(http.StatusServiceUnavailable, map[string]string{"error": dispatchErr.Error()})
		}
		if res.Error != "" {
			status := http.StatusInternalServerError
			if strings.Contains(strings.ToLower(res.Error), "does not belong to stack") {
				status = http.StatusForbidden
			}
			return e.JSON(status, map[string]string{"error": res.Error})
		}

		return e.JSON(http.StatusOK, map[string]string{"logs": res.Output})
	}).BindFunc(rbac.Require(rbac.CapViewLogs))
}

func (rr routeRegistrar) registerStackComposeRoute() {
	rr.r.GET("/api/custom/stacks/{id}/compose", func(e *core.RequestEvent) error {
		stackID := e.Request.PathValue("id")
		stack, err := rr.app.FindRecordById("stacks", stackID)
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "stack not found"})
		}

		currentVersion := stack.GetInt("current_version")
		if currentVersion > 0 {
			renderer := sync.NewRenderer(rr.app)
			filePath := renderer.GetRevisionFilePath(stackID, currentVersion)
			data, err := os.ReadFile(filePath)
			if err == nil {
				return e.JSON(http.StatusOK, map[string]string{
					"content":  string(data),
					"filename": fmt.Sprintf("v%d.yml", currentVersion),
				})
			}
			log.Printf("[routes] rendered compose v%d missing for stack %s: %v. Falling back to repo file.", currentVersion, stackID, err)
		}

		if stack.GetString("source_type") == "local" {
			workerID := stack.GetString("worker")
			worker, err := rr.resolveWorker(workerID)
			if err != nil {
				return e.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
			}
			if rr.workerSvc == nil || !rr.workerSvc.IsConnected(workerID) {
				return e.JSON(http.StatusServiceUnavailable, map[string]string{"error": fmt.Sprintf(OfflineWorkerMsg, worker.GetString("hostname"))})
			}
			importPath := stack.GetString("import_path")
			result, dispatchErr := rr.workerSvc.Dispatch(e.Request.Context(), workerID, protocol.ReadFileCommand{
				CommandID: fmt.Sprintf("compose-%s", stackID),
				Path:      importPath,
			})
			if dispatchErr != nil || result.Error != "" {
				return e.JSON(http.StatusNotFound, map[string]string{"error": "compose file not found"})
			}
			data, decodeErr := base64.StdEncoding.DecodeString(result.Output)
			if decodeErr != nil {
				return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to decode compose file"})
			}
			return e.JSON(http.StatusOK, map[string]string{"content": string(data), "filename": filepath.Base(importPath)})
		}

		composePath := stack.GetString("compose_path")
		if err := safepath.ValidateComposePath(composePath); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		composeFile := stack.GetString("compose_file")
		if composeFile == "" {
			composeFile = "docker-compose.yml"
		}
		if err := safepath.ValidateComposeFile(composeFile); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}

		workDir := filepath.Join(config.GetReposWorkspace(), stack.GetString("repository"))
		if composePath != "" && composePath != "." {
			workDir = filepath.Join(workDir, composePath)
		}

		data, err := os.ReadFile(filepath.Join(workDir, composeFile))
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "compose file not found"})
		}
		return e.JSON(http.StatusOK, map[string]string{"content": string(data), "filename": composeFile})
	}).BindFunc(rbac.Require(rbac.CapViewStacks))
}

func (rr routeRegistrar) registerContainerActionRoutes() {
	rr.r.POST("/api/custom/stacks/{id}/container/stop", func(e *core.RequestEvent) error {
		stackID := e.Request.PathValue("id")
		var body struct {
			ContainerID string `json:"container_id"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil || body.ContainerID == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "container_id required"})
		}

		_, projectName, workerID, ok := rr.resolveStackAndWorker(e, stackID)
		if !ok {
			return nil
		}

		res, dispatchErr := rr.workerSvc.Dispatch(e.Request.Context(), workerID, protocol.ContainerActionCommand{
			CommandID:   fmt.Sprintf("stop-container-%s", body.ContainerID),
			StackID:     stackID,
			ProjectName: projectName,
			ContainerID: body.ContainerID,
		})
		if dispatchErr != nil {
			return e.JSON(http.StatusServiceUnavailable, map[string]string{"error": dispatchErr.Error()})
		}
		if res.Error != "" {
			status := http.StatusInternalServerError
			if strings.Contains(strings.ToLower(res.Error), "does not belong to stack") {
				status = http.StatusForbidden
			}
			return e.JSON(status, map[string]string{"error": res.Error})
		}
		return e.JSON(http.StatusOK, map[string]string{"status": "stopped"})
	}).BindFunc(rbac.Require(rbac.CapOperateStacks))

	rr.r.POST("/api/custom/stacks/{id}/container/restart", func(e *core.RequestEvent) error {
		stackID := e.Request.PathValue("id")
		var body struct {
			ContainerID string `json:"container_id"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil || body.ContainerID == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "container_id required"})
		}

		_, projectName, workerID, ok := rr.resolveStackAndWorker(e, stackID)
		if !ok {
			return nil
		}

		res, dispatchErr := rr.workerSvc.Dispatch(e.Request.Context(), workerID, protocol.ContainerActionCommand{
			CommandID:   fmt.Sprintf("restart-container-%s", body.ContainerID),
			StackID:     stackID,
			ProjectName: projectName,
			ContainerID: body.ContainerID,
		})
		if dispatchErr != nil {
			return e.JSON(http.StatusServiceUnavailable, map[string]string{"error": dispatchErr.Error()})
		}
		if res.Error != "" {
			status := http.StatusInternalServerError
			if strings.Contains(strings.ToLower(res.Error), "does not belong to stack") {
				status = http.StatusForbidden
			}
			return e.JSON(status, map[string]string{"error": res.Error})
		}
		return e.JSON(http.StatusOK, map[string]string{"status": "restarted"})
	}).BindFunc(rbac.Require(rbac.CapOperateStacks))
}

func (rr routeRegistrar) registerStackDeleteRoute() {
	rr.r.DELETE("/api/custom/stacks/{id}", func(e *core.RequestEvent) error {
		stackID := e.Request.PathValue("id")
		if stackID == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "missing id"})
		}

		force := e.Request.URL.Query().Get("force") == "true"
		agentIsOnline := true
		if !force {
			if !rr.workerOnline(e, stackID) {
				return nil
			}
		} else if rr.workerSvc != nil {
			if stack, err := rr.app.FindRecordById("stacks", stackID); err == nil {
				assignedWorkerID := stack.GetString("worker")
				if assignedWorkerID == "" || !rr.workerSvc.IsConnected(assignedWorkerID) {
					agentIsOnline = false
				}
			}
		}

		stack, err := rr.app.FindRecordById("stacks", stackID)
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "stack not found"})
		}

		workerID := stack.GetString("worker")
		if workerID == "" && !force {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "stack has no worker assigned"})
		}

		var composeContent []byte
		renderer := sync.NewRenderer(rr.app)
		currentVersion := stack.GetInt("current_version")
		if currentVersion > 0 {
			filePath := renderer.GetRevisionFilePath(stackID, currentVersion)
			var readErr error
			composeContent, readErr = os.ReadFile(filePath)
			if readErr != nil && !force {
				return e.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to read rendered compose file for teardown: %v", readErr)})
			}
		}

		var teardownOutput string
		if len(composeContent) > 0 && agentIsOnline {
			var teardownEnvFileB64 string
			secretKey := crypto.NormalizeSecretKey(os.Getenv("SECRET_KEY"))
			envVars, envLoadErr := envvars.LoadStack(e.Request.Context(), rr.app, secrets.NewDefaultRegistry(rr.app, secretKey), stackID)
			if envLoadErr != nil {
				return e.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to load env vars for teardown: %v", envLoadErr)})
			}
			if len(envVars) > 0 {
				var b64Err error
				teardownEnvFileB64, b64Err = sync.BuildEnvFileB64(envVars)
				if b64Err != nil {
					return e.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to serialize env vars for teardown: %v", b64Err)})
				}
			}

			result, dispatchErr := rr.workerSvc.Dispatch(e.Request.Context(), workerID, protocol.TeardownCommand{
				CommandID:      fmt.Sprintf("teardown-%s", stackID),
				StackID:        stackID,
				ComposeFileB64: base64.StdEncoding.EncodeToString(composeContent),
				EnvFileB64:     teardownEnvFileB64,
			})
			teardownOutput = result.Output
			if dispatchErr != nil {
				return e.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("teardown dispatch failed: %v", dispatchErr)})
			}
			if result.Error != "" {
				return e.JSON(http.StatusInternalServerError, map[string]string{
					"error":          fmt.Sprintf("worker teardown failed: %s", result.Error),
					"compose_output": result.Output,
				})
			}
		}

		rr.scheduler.UnregisterStack(stackID)

		for _, col := range []string{"sync_logs", "stack_services", "stack_env_vars", "stack_global_env_vars", "stack_revisions", "stack_pending_reconciles"} {
			records, err := rr.app.FindAllRecords(col, dbx.HashExp{"stack": stackID})
			if err != nil {
				return e.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to query related %s records: %v", col, err)})
			}
			for _, rec := range records {
				if err := rr.app.Delete(rec); err != nil {
					return e.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to delete related %s record %s: %v", col, rec.Id, err)})
				}
			}
		}

		stackStorageDir := filepath.Dir(renderer.GetRevisionFilePath(stackID, 1))
		if err := os.RemoveAll(stackStorageDir); err != nil {
			log.Printf("[routes] failed to remove stack storage dir for %s: %v", stackID, err)
		}

		if err := rr.app.Delete(stack); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return e.JSON(http.StatusOK, map[string]string{"status": "deleted", "compose_output": teardownOutput})
	}).BindFunc(rbac.Require(rbac.CapOperateStacks))
}

func (rr routeRegistrar) registerStackTransferRoute() {
	rr.r.POST("/api/custom/stacks/{id}/transfer", func(e *core.RequestEvent) error {
		stackID := e.Request.PathValue("id")
		if stackID == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "missing id"})
		}

		var body struct {
			TargetWorkerID string `json:"target_worker_id"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil || body.TargetWorkerID == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "target_worker_id required"})
		}

		stack, err := rr.app.FindRecordById("stacks", stackID)
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "stack not found"})
		}
		if stack.GetString("worker") == body.TargetWorkerID {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "target worker is the same as the current worker"})
		}

		if !rr.workerOnline(e, stackID) {
			return nil
		}
		if rr.workerSvc != nil && !rr.workerSvc.IsConnected(body.TargetWorkerID) {
			targetHost := body.TargetWorkerID
			if a, err := rr.app.FindRecordById("workers", body.TargetWorkerID); err == nil {
				targetHost = a.GetString("hostname")
			}
			return e.JSON(http.StatusServiceUnavailable, map[string]string{
				"error": fmt.Sprintf("target worker '%s' is offline — connect the worker before transferring", targetHost),
			})
		}

		var userID string
		if e.Auth != nil {
			userID = e.Auth.Id
		}
		rr.scheduler.TriggerTransfer(stackID, body.TargetWorkerID, userID)
		return e.JSON(http.StatusAccepted, map[string]string{"status": "transfer_started"})
	}).BindFunc(rbac.Require(rbac.CapOperateStacks))
}

func (rr routeRegistrar) registerImportRoutes() {
	rr.r.GET("/api/custom/stacks/import/discover", func(e *core.RequestEvent) error {
		workerID := e.Request.URL.Query().Get("worker")
		worker, err := rr.resolveWorker(workerID)
		if err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		if rr.workerSvc == nil || !rr.workerSvc.IsConnected(workerID) {
			return e.JSON(http.StatusServiceUnavailable, map[string]string{"error": fmt.Sprintf(OfflineWorkerMsg, worker.GetString("hostname"))})
		}
		result, err := rr.workerSvc.Dispatch(e.Request.Context(), workerID, protocol.DiscoverProjectsCommand{
			CommandID: fmt.Sprintf("discover-%s", workerID),
		})
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		if result.Error != "" {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": result.Error})
		}
		var res protocol.DiscoverProjectsResult
		if err := json.Unmarshal([]byte(result.Output), &res); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to decode worker response"})
		}
		return e.JSON(http.StatusOK, res.Projects)
	}).BindFunc(rbac.Require(rbac.CapOperateStacks))

	rr.r.POST("/api/custom/stacks/import", func(e *core.RequestEvent) error {
		var body struct {
			Name            string `json:"name"`
			WorkerID        string `json:"worker_id"`
			ImportPath      string `json:"import_path"`
			RecreateVolumes bool   `json:"recreate_volumes"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
		}
		if body.Name == "" || body.ImportPath == "" || body.WorkerID == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "name, worker_id, and import_path are required"})
		}
		if !strings.HasPrefix(body.ImportPath, "/") {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "import_path must be an absolute path"})
		}
		ext := strings.ToLower(filepath.Ext(body.ImportPath))
		if ext != ".yml" && ext != ".yaml" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "import_path must point to a .yml or .yaml file"})
		}
		if strings.Contains(body.ImportPath, "..") {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "import_path must not contain path traversal"})
		}

		workerRecord, err := rr.app.FindRecordById("workers", body.WorkerID)
		if err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "worker not found"})
		}
		if rr.workerSvc == nil || !rr.workerSvc.IsConnected(body.WorkerID) {
			return e.JSON(http.StatusServiceUnavailable, map[string]string{"error": fmt.Sprintf(OfflineWorkerMsg, workerRecord.GetString("hostname"))})
		}

		result, dispatchErr := rr.workerSvc.Dispatch(e.Request.Context(), body.WorkerID, protocol.ReadFileCommand{
			CommandID: fmt.Sprintf("validate-import-%s", body.WorkerID),
			Path:      body.ImportPath,
		})
		if dispatchErr != nil || result.Error != "" {
			return e.JSON(http.StatusBadRequest, map[string]string{
				"error": fmt.Sprintf("cannot access compose file on worker: %v %s", dispatchErr, result.Error),
			})
		}

		stacksCol, err := rr.app.FindCollectionByNameOrId("stacks")
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		stack := core.NewRecord(stacksCol)
		stack.Set("name", body.Name)
		stack.Set("worker", body.WorkerID)
		stack.Set("source_type", "local")
		stack.Set("import_path", body.ImportPath)
		stack.Set("import_recreate_volumes", body.RecreateVolumes)
		stack.Set("status", "pending")
		stack.Set("auto_sync", false)
		if err := rr.app.Save(stack); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		var userID string
		if e.Auth != nil {
			userID = e.Auth.Id
		}
		rr.scheduler.TriggerSync(stack.Id, "manual", 0, userID)
		log.Printf("[routes] import stack=%s worker=%s path=%s", stack.Id, body.WorkerID, body.ImportPath)
		return e.JSON(http.StatusOK, map[string]string{"id": stack.Id, "status": "import_triggered"})
	}).BindFunc(rbac.Require(rbac.CapOperateStacks))
}

// registerCreateFromWireopsRoute creates a stack entirely from a wireops.yaml
// found in a cloned repository. Unlike the manual create flow (raw PocketBase
// collection.create from the frontend), the client only supplies repository,
// worker, and the wireops.yaml path — every wireops.yaml-derived field (name,
// compose_path/compose_file, remove_orphans, force_pull,
// deploy_timeout_seconds, wait_running_jobs, worker_tags) is computed here by
// re-parsing the file server-side, never trusted from client input. This
// keeps a single source of truth (the file) and matches the immutability
// rule enforced on update in internal/hooks/pb_hooks.go.
// resolveWireopsStackFields applies the defaults for wireops.yaml fields that
// are optional in the file: remove_orphans defaults to true (preserving the
// pre-wireops.yaml unconditional `--remove-orphans` behavior), force_pull
// defaults to false, wait_running_jobs maps the YAML bool to the stacks
// select (true -> "always", absent/false -> "never"), and worker_tags
// defaults to an empty (non-nil) slice.
func resolveWireopsStackFields(def *manifest.Definition) (removeOrphans, forcePull bool, waitRunningJobs string, workerTags []string) {
	removeOrphans = true
	forcePull = false
	if def.Compose != nil {
		if def.Compose.RemoveOrphans != nil {
			removeOrphans = *def.Compose.RemoveOrphans
		}
		if def.Compose.ForcePull != nil {
			forcePull = *def.Compose.ForcePull
		}
	}

	waitRunningJobs = "never"
	if def.Jobs != nil && def.Jobs.WaitRunning != nil && *def.Jobs.WaitRunning {
		waitRunningJobs = "always"
	}

	workerTags = []string{}
	if def.Worker != nil && def.Worker.Tags != nil {
		workerTags = def.Worker.Tags
	}
	return removeOrphans, forcePull, waitRunningJobs, workerTags
}

func (rr routeRegistrar) registerCreateFromWireopsRoute() {
	rr.r.POST("/api/custom/stacks/from-wireops", func(e *core.RequestEvent) error {
		var body struct {
			Repository  string `json:"repository"`
			Worker      string `json:"worker"`
			WireopsFile string `json:"wireops_file"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
		}
		if body.Repository == "" || body.Worker == "" || body.WireopsFile == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "repository, worker, and wireops_file are required"})
		}

		workerRecord, err := rr.app.FindRecordById("workers", body.Worker)
		if err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "worker not found"})
		}

		repoDir, ok := rr.repoFilesSetupByID(e, body.Repository)
		if !ok {
			return nil
		}

		def, err := manifest.ParseWireopsFile(config.GetReposWorkspace(), body.Repository, body.WireopsFile)
		if err != nil {
			return e.JSON(http.StatusUnprocessableEntity, map[string]any{
				"error":  err.Error(),
				"errors": wireopsValidationErrors(err),
			})
		}
		resolveWireopsComposeFile(repoDir, body.WireopsFile, def)
		if def.ResolutionError != "" {
			return e.JSON(http.StatusUnprocessableEntity, map[string]string{"error": def.ResolutionError})
		}

		removeOrphans, forcePull, waitRunningJobs, workerTags := resolveWireopsStackFields(def)

		stacksCol, err := rr.app.FindCollectionByNameOrId("stacks")
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		stack := core.NewRecord(stacksCol)
		stack.Set("name", def.Name)
		stack.Set("repository", body.Repository)
		stack.Set("worker", body.Worker)
		stack.Set("compose_path", def.ResolvedComposePath)
		stack.Set("compose_file", def.ResolvedComposeFile)
		stack.Set("auto_sync", true)
		stack.Set("status", "pending")
		stack.Set("remove_orphans", removeOrphans)
		stack.Set("force_pull", forcePull)
		stack.Set("deploy_timeout_seconds", def.DeployTimeoutSeconds)
		stack.Set("sync_interval_seconds", def.SyncIntervalSeconds)
		stack.Set("wait_running_jobs", waitRunningJobs)
		stack.Set("worker_tags", workerTags)
		stack.Set("config_source", "wireops_file")
		stack.Set("wireops_file_path", body.WireopsFile)
		if err := rr.app.Save(stack); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		log.Printf("[routes] create-from-wireops stack=%s repository=%s worker=%s file=%s", stack.Id, body.Repository, workerRecord.GetString("hostname"), body.WireopsFile)
		return e.JSON(http.StatusOK, map[string]string{"id": stack.Id, "name": def.Name, "status": "pending"})
	}).BindFunc(rbac.Require(rbac.CapManageRepos))
}

// composePortsToShortForm converts a service's "ports" value from `docker compose config`
// JSON output (long-form objects, e.g. {"published":"8080","target":80,"protocol":"tcp"})
// into short-syntax strings ("8080:80") comparable to a ServiceOverride.Ports value.
func composePortsToShortForm(raw interface{}) []string {
	list, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	var out []string
	for _, item := range list {
		switch v := item.(type) {
		case string:
			out = append(out, v)
		case map[string]interface{}:
			published := portNumberString(v["published"])
			target := portNumberString(v["target"])
			if published == "" || target == "" {
				continue
			}
			short := published + ":" + target
			if protocol, _ := v["protocol"].(string); protocol != "" && protocol != "tcp" {
				short += "/" + protocol
			}
			out = append(out, short)
		}
	}
	return out
}

// portNumberString formats a `docker compose config` JSON port field (decoded as
// float64, string, or absent) as a plain integer string, avoiding fmt.Sprint's
// scientific-notation rendering of large float64 values.
func portNumberString(v interface{}) string {
	switch n := v.(type) {
	case float64:
		return strconv.FormatFloat(n, 'f', 0, 64)
	case string:
		return n
	default:
		return ""
	}
}

// composeNetworksToList converts a service's "networks" value (map or list form) into a
// plain list of network names comparable to a ServiceOverride.Networks value.
func composeNetworksToList(raw interface{}) []string {
	switch v := raw.(type) {
	case map[string]interface{}:
		names := make([]string, 0, len(v))
		for name := range v {
			names = append(names, name)
		}
		sort.Strings(names)
		return names
	case []interface{}:
		var out []string
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
