package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/wireops/wireops/internal/compose"
	"github.com/wireops/wireops/internal/protocol"
	"github.com/wireops/wireops/internal/rbac"
	wiresync "github.com/wireops/wireops/internal/sync"
)

const (
	observationTimeout = 5 * time.Second
	inventoryTimeout   = 25 * time.Second
	inventoryParallel  = 5
)

// resourceError preserves the distinction between a transport failure and a
// worker response that could not be trusted. In particular, neither becomes an
// empty inventory: empty has a useful, positive meaning to callers.
type resourceError struct {
	status int
	err    error
}

func (e *resourceError) Error() string { return e.err.Error() }

func (rr routeRegistrar) stackResources(ctx context.Context, stack *core.Record) (protocol.GetResourcesResult, error) {
	empty := protocol.GetResourcesResult{Volumes: []protocol.VolumeInfo{}, Networks: []protocol.NetworkInfo{}}
	workerID := stack.GetString("worker")
	worker, err := rr.resolveWorker(workerID)
	if err != nil {
		return empty, &resourceError{status: http.StatusBadRequest, err: err}
	}
	if rr.workerSvc == nil || !rr.workerSvc.IsConnected(workerID) {
		return empty, &resourceError{status: http.StatusServiceUnavailable, err: fmt.Errorf(OfflineWorkerMsg, worker.GetString("hostname"))}
	}
	projectName, err := wiresync.DeployedProjectName(stack)
	if err != nil {
		return empty, &resourceError{status: http.StatusConflict, err: err}
	}
	commandCtx, cancel := context.WithTimeout(ctx, observationTimeout)
	defer cancel()
	result, dispatchErr := rr.workerSvc.Dispatch(commandCtx, workerID, protocol.GetResourcesCommand{
		CommandID:   fmt.Sprintf("resources-%s-%s", stack.Id, core.GenerateDefaultRandomId()),
		StackID:     stack.Id,
		ProjectName: projectName,
	})
	if dispatchErr != nil {
		return empty, &resourceError{status: http.StatusServiceUnavailable, err: dispatchErr}
	}
	if result.Error != "" {
		return empty, &resourceError{status: http.StatusBadGateway, err: fmt.Errorf("worker resource inventory failed: %s", result.Error)}
	}
	var resources protocol.GetResourcesResult
	if err := json.Unmarshal([]byte(result.Output), &resources); err != nil {
		return empty, &resourceError{status: http.StatusBadGateway, err: fmt.Errorf("invalid worker resource inventory: %w", err)}
	}
	if resources.Volumes == nil {
		resources.Volumes = []protocol.VolumeInfo{}
	}
	if resources.Networks == nil {
		resources.Networks = []protocol.NetworkInfo{}
	}
	if !recordHasCapability(worker, protocol.CapabilityResourceInventoryV1) {
		resources.Partial = true
		resources.Warnings = append(resources.Warnings, "the assigned worker does not confirm complete resource inventory; upgrade it for authoritative bind mounts and external networks")
	}
	return resources, nil
}

func healthUnknown(reason string) map[string]any {
	return map[string]any{
		"state":       "unknown",
		"observed_at": time.Now().UTC().Format(time.RFC3339),
		"reason":      reason,
	}
}

// stackHealth is deliberately a read-only observation. It uses the deployed
// revision as its source of truth, so an unrendered change in Git does not make
// a currently-running stack look unhealthy.
func (rr routeRegistrar) stackHealth(ctx context.Context, stack *core.Record) map[string]any {
	version := stack.GetInt("deployed_version")
	if version <= 0 {
		return healthUnknown("no deployed revision is available")
	}
	content, err := os.ReadFile(wiresync.NewRenderer(rr.app).GetRevisionFilePath(stack.Id, version))
	if err != nil {
		return healthUnknown("the deployed revision could not be read")
	}
	expected, err := compose.ExpectedServices(content)
	if err != nil {
		return healthUnknown("the deployed revision could not be parsed")
	}
	workerID := stack.GetString("worker")
	worker, err := rr.resolveWorker(workerID)
	if err != nil {
		return healthUnknown(err.Error())
	}
	if rr.workerSvc == nil || !rr.workerSvc.IsConnected(workerID) {
		return healthUnknown(fmt.Sprintf(OfflineWorkerMsg, worker.GetString("hostname")))
	}
	projectName, err := compose.ExtractProjectName(content)
	if err != nil {
		projectName, err = wiresync.DeployedProjectName(stack)
		if err != nil {
			return healthUnknown(err.Error())
		}
	}
	commandCtx, cancel := context.WithTimeout(ctx, observationTimeout)
	defer cancel()
	res, err := rr.workerSvc.Dispatch(commandCtx, workerID, protocol.GetStatusCommand{
		CommandID:   fmt.Sprintf("health-%s-%s", stack.Id, core.GenerateDefaultRandomId()),
		StackID:     stack.Id,
		ProjectName: projectName,
	})
	if err != nil {
		return healthUnknown("could not query worker status: " + err.Error())
	}
	if res.Error != "" {
		return healthUnknown("worker status query failed: " + res.Error)
	}
	var statuses []compose.ServiceStatus
	if err := json.Unmarshal([]byte(res.Output), &statuses); err != nil {
		return healthUnknown("worker returned invalid service status")
	}
	byService := make(map[string][]compose.ServiceStatus, len(statuses))
	for _, status := range statuses {
		byService[status.ServiceName] = append(byService[status.ServiceName], status)
	}
	var healthy, unhealthy, starting, missing int
	for _, service := range expected {
		instances := byService[service.Name]
		state := compose.ClassifyServiceInstances(instances, service.IsInit, true)
		if !service.IsInit && state == compose.ServiceHealthy && len(instances) < service.Replicas {
			state = compose.ServiceMissing
		}
		switch state {
		case compose.ServiceHealthy:
			healthy++
		case compose.ServiceStarting:
			starting++
		case compose.ServiceMissing:
			missing++
		default:
			unhealthy++
		}
	}
	state := "healthy"
	switch {
	case unhealthy+missing > 0 && healthy > 0:
		state = "degraded"
	case unhealthy+missing > 0:
		state = "unhealthy"
	case starting > 0:
		state = "starting"
	}
	return map[string]any{
		"state":              state,
		"observed_at":        time.Now().UTC().Format(time.RFC3339),
		"services":           statuses,
		"expected_services":  len(expected),
		"healthy_services":   healthy,
		"unhealthy_services": unhealthy,
		"missing_services":   missing,
		"starting_services":  starting,
	}
}

func (rr routeRegistrar) registerStackObservabilityRoutes() {
	rr.r.GET("/api/custom/stacks/{id}/health", func(e *core.RequestEvent) error {
		stack, err := rr.app.FindRecordById("stacks", e.Request.PathValue("id"))
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "stack not found"})
		}
		return e.JSON(http.StatusOK, rr.stackHealth(e.Request.Context(), stack))
	}).BindFunc(rbac.Require(rbac.CapViewStacks))

	rr.r.GET("/api/custom/inventory", func(e *core.RequestEvent) error {
		limit := 50
		if raw := e.Request.URL.Query().Get("per_page"); raw != "" {
			if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
				limit = parsed
			}
		}
		if limit > 200 {
			limit = 200
		}
		page := 1
		if raw := e.Request.URL.Query().Get("page"); raw != "" {
			if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
				page = parsed
			}
		}
		clauses := []string{"1=1"}
		params := dbx.Params{}
		if worker := e.Request.URL.Query().Get("worker"); worker != "" {
			clauses = append(clauses, "worker = {:worker}")
			params["worker"] = worker
		}
		if group := e.Request.URL.Query().Get("group"); group != "" {
			clauses = append(clauses, "group = {:group}")
			params["group"] = group
		}
		records, err := rr.app.FindRecordsByFilter("stacks", joinFilter(clauses), "-updated", limit, (page-1)*limit, params)
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		ctx, cancel := context.WithTimeout(e.Request.Context(), inventoryTimeout)
		defer cancel()
		items := make([]map[string]any, len(records))
		sem := make(chan struct{}, inventoryParallel)
		var wg sync.WaitGroup
		for i, stack := range records {
			i, stack := i, stack
			wg.Add(1)
			go func() {
				defer wg.Done()
				base := map[string]any{"id": stack.Id, "name": stack.GetString("name"), "group": stack.GetString("group"), "status": stack.GetString("status"), "deployed_commit": stack.GetString("deployed_commit"), "desired_commit": stack.GetString("desired_commit")}
				if worker, err := rr.resolveWorker(stack.GetString("worker")); err == nil {
					var capabilities []string
					_ = worker.UnmarshalJSONField("capabilities", &capabilities)
					base["worker"] = map[string]any{"id": worker.Id, "hostname": worker.GetString("hostname"), "version": worker.GetString("version"), "capabilities": capabilities}
				}
				select {
				case sem <- struct{}{}:
					defer func() { <-sem }()
				case <-ctx.Done():
					base["health"] = healthUnknown("inventory timed out")
					items[i] = base
					return
				}
				health := rr.stackHealth(ctx, stack)
				base["health"] = health
				if services, ok := health["services"]; ok {
					base["services"] = services
				}
				if resources, err := rr.stackResources(ctx, stack); err != nil {
					base["resources_error"] = err.Error()
				} else {
					base["resources"] = resources
				}
				items[i] = base
			}()
		}
		wg.Wait()
		return e.JSON(http.StatusOK, map[string]any{"page": page, "per_page": limit, "stacks": items})
	}).BindFunc(rbac.Require(rbac.CapViewStacks))
}

func joinFilter(parts []string) string {
	result := ""
	for _, part := range parts {
		if result != "" {
			result += " && "
		}
		result += part
	}
	return result
}
