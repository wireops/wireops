package routes

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	stdsync "sync"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/google/uuid"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/wireops/wireops/internal/compose"
	"github.com/wireops/wireops/internal/config"
	"github.com/wireops/wireops/internal/crypto"
	"github.com/wireops/wireops/internal/git"
	"github.com/wireops/wireops/internal/integrations"
	"github.com/wireops/wireops/internal/job"
	"github.com/wireops/wireops/internal/notify"
	"github.com/wireops/wireops/internal/protocol"
	"github.com/wireops/wireops/internal/rbac"
	"github.com/wireops/wireops/internal/safepath"
	"github.com/wireops/wireops/internal/sync"
)

type routeRegistrar struct {
	r         *router.Router[*core.RequestEvent]
	app       core.App
	scheduler *sync.Scheduler
	workerSvc sync.WorkerDispatcher
}

func (rr routeRegistrar) resolveWorker(workerID string) (*core.Record, error) {
	if workerID == "" {
		return nil, fmt.Errorf("stack has no worker assigned")
	}
	worker, err := rr.app.FindRecordById("workers", workerID)
	if err != nil {
		return nil, fmt.Errorf("worker %s not found", workerID)
	}
	return worker, nil
}

func (rr routeRegistrar) workerOnline(e *core.RequestEvent, stackID string) bool {
	if rr.workerSvc == nil {
		e.JSON(http.StatusServiceUnavailable, map[string]string{"error": "worker service is unavailable"})
		return false
	}
	stack, findErr := rr.app.FindRecordById("stacks", stackID)
	if findErr != nil {
		return true
	}
	assignedWorkerID := stack.GetString("worker")
	worker, err := rr.resolveWorker(assignedWorkerID)
	if err != nil {
		e.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		return false
	}
	if !rr.workerSvc.IsConnected(assignedWorkerID) {
		e.JSON(http.StatusServiceUnavailable, map[string]string{
			"error": fmt.Sprintf("worker '%s' is offline — connect the worker before performing this action", worker.GetString("hostname")),
		})
		return false
	}
	return true
}

func (rr routeRegistrar) resolveStackAndWorker(e *core.RequestEvent, stackID string) (*core.Record, string, string, bool) {
	if !rr.workerOnline(e, stackID) {
		return nil, "", "", false
	}
	stack, err := rr.app.FindRecordById("stacks", stackID)
	if err != nil {
		_ = e.JSON(http.StatusNotFound, map[string]string{"error": "stack not found"})
		return nil, "", "", false
	}
	projectName := compose.ProjectName(stackWorkDir(rr.app, stack))
	workerID := stack.GetString("worker")
	return stack, projectName, workerID, true
}

func (rr routeRegistrar) listYAMLFiles(repoDir string, filter func([]byte) bool) ([]string, error) {
	var candidates []string
	if err := filepath.WalkDir(repoDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if ext == ".yml" || ext == ".yaml" {
			candidates = append(candidates, path)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	var (
		mu      stdsync.Mutex
		wg      stdsync.WaitGroup
		matched []string
	)
	for _, path := range candidates {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			data, err := os.ReadFile(p)
			if err != nil || !filter(data) {
				return
			}
			rel, err := filepath.Rel(repoDir, p)
			if err != nil {
				return
			}
			mu.Lock()
			matched = append(matched, rel)
			mu.Unlock()
		}(path)
	}
	wg.Wait()
	sort.Strings(matched)
	return matched, nil
}

func (rr routeRegistrar) repoFilesSetup(e *core.RequestEvent) (string, bool) {
	repoID := e.Request.PathValue("id")
	if repoID == "" {
		_ = e.JSON(http.StatusBadRequest, map[string]string{"error": "missing id"})
		return "", false
	}
	repo, err := rr.app.FindRecordById("repositories", repoID)
	if err != nil {
		_ = e.JSON(http.StatusNotFound, map[string]string{"error": "repository not found"})
		return "", false
	}
	workspace := config.GetReposWorkspace()
	var auth transport.AuthMethod
	if cred, err := loadRepositoryCredential(rr.app, repoID); err == nil && cred != nil {
		resolvedAuth, err := git.ResolveTransportAuth(*cred)
		if err != nil {
			_ = e.JSON(http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid repository credential: %v", err)})
			return "", false
		}
		auth = resolvedAuth
	}
	branch := repo.GetString("branch")
	if branch == "" {
		branch = "main"
	}
	if _, err := git.CloneOrFetch(repoID, repo.GetString("git_url"), branch, auth, workspace); err != nil {
		_ = e.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to sync repository: %v", err)})
		return "", false
	}
	return filepath.Join(workspace, repoID), true
}

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
		rr.scheduler.TriggerSync(id, "webhook", 0, "webhook")
		return e.JSON(http.StatusOK, map[string]string{"status": "triggered"})
	})
}

func (rr routeRegistrar) registerBackupAndStreamRoutes() {
	rr.r.POST("/api/custom/backups", func(e *core.RequestEvent) error {
		var body struct {
			Filename string `json:"filename"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil && err.Error() != "EOF" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
		}

		filename := strings.TrimSpace(body.Filename)
		if filename == "" {
			filename = fmt.Sprintf("wireops_backup_%d.zip", time.Now().Unix())
		}
		filename = filepath.Base(filename)
		if !strings.HasSuffix(strings.ToLower(filename), ".zip") {
			filename += ".zip"
		}
		if filename == "." || filename == "/" || filename == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid filename"})
		}

		storageFilename := fmt.Sprintf("wireops_backup_%s.zip", uuid.NewString())
		if err := rr.app.CreateBackup(context.Background(), storageFilename); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to create backup"})
		}

		backupPath := filepath.Join(rr.app.DataDir(), core.LocalBackupsDirName, storageFilename)
		file, err := os.Open(backupPath)
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to open backup"})
		}
		defer os.Remove(backupPath)
		defer file.Close()

		info, err := file.Stat()
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to stat backup"})
		}

		e.Response.Header().Set("Content-Type", "application/zip")
		e.Response.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
		http.ServeContent(e.Response, e.Request, filename, info.ModTime(), file)
		return nil
	}).BindFunc(rbac.Require(rbac.CapManageSettings))

	rr.r.GET("/api/custom/stacks/{id}/stream", func(e *core.RequestEvent) error {
		id := e.Request.PathValue("id")
		if id == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "missing id"})
		}

		e.Response.Header().Set("Content-Type", "text/event-stream")
		e.Response.Header().Set("Cache-Control", "no-cache")
		e.Response.Header().Set("Connection", "keep-alive")

		logs, err := rr.app.FindAllRecords("sync_logs", dbx.HashExp{"stack": id})
		if err != nil {
			return err
		}

		flusher, ok := e.Response.(http.Flusher)
		for _, logRecord := range logs {
			for _, line := range strings.Split(logRecord.GetString("output"), "\n") {
				fmt.Fprintf(e.Response, "data: %s\n\n", line)
			}
			if ok {
				flusher.Flush()
			}
		}

		return nil
	}).BindFunc(rbac.Require(rbac.CapViewLogs))
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
		rr.scheduler.TriggerForceRedeploy(stackID, body.RecreateContainers, body.RecreateVolumes, body.RecreateNetworks, userID)
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

func (rr routeRegistrar) registerRepositoryRoutes() {
	rr.r.GET("/api/custom/repositories/{id}/commits", func(e *core.RequestEvent) error {
		repoID := e.Request.PathValue("id")
		if repoID == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "missing id"})
		}

		cleaned := filepath.Clean(repoID)
		if filepath.IsAbs(cleaned) || strings.Contains(repoID, "..") || strings.Contains(repoID, string(os.PathSeparator)) {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid repository id"})
		}

		repoDir := filepath.Join(config.GetReposWorkspace(), cleaned)
		gitRepo, err := gogit.PlainOpen(repoDir)
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "repository not cloned yet"})
		}

		repo, err := rr.app.FindRecordById("repositories", repoID)
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "repository not found"})
		}
		branch := repo.GetString("branch")
		if branch == "" {
			branch = "main"
		}

		remoteRef, err := gitRepo.Reference(plumbing.NewRemoteReferenceName("origin", branch), true)
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "branch ref not found"})
		}

		iter, err := gitRepo.Log(&gogit.LogOptions{From: remoteRef.Hash()})
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		type commitInfo struct {
			SHA     string `json:"sha"`
			Message string `json:"message"`
			Author  string `json:"author"`
			Date    string `json:"date"`
		}
		var commits []commitInfo
		count := 0
		const limit = 5
		iter.ForEach(func(c *object.Commit) error {
			if count >= limit {
				return fmt.Errorf("done")
			}
			commits = append(commits, commitInfo{
				SHA:     c.Hash.String(),
				Message: strings.TrimSpace(c.Message),
				Author:  c.Author.Name,
				Date:    c.Author.When.UTC().Format("2006-01-02T15:04:05Z"),
			})
			count++
			return nil
		})

		return e.JSON(http.StatusOK, commits)
	}).BindFunc(rbac.Require(rbac.CapViewStacks))

	rr.r.GET("/api/custom/repositories/{id}/files", func(e *core.RequestEvent) error {
		repoID := e.Request.PathValue("id")
		if repoID == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "missing id"})
		}

		repo, err := rr.app.FindRecordById("repositories", repoID)
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "repository not found"})
		}

		workspace := config.GetReposWorkspace()
		var auth transport.AuthMethod
		if cred, err := loadRepositoryCredential(rr.app, repoID); err == nil && cred != nil {
			resolvedAuth, err := git.ResolveTransportAuth(*cred)
			if err != nil {
				return e.JSON(http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid repository credential: %v", err)})
			}
			auth = resolvedAuth
		}

		branch := repo.GetString("branch")
		if branch == "" {
			branch = "main"
		}

		if _, err := git.CloneOrFetch(repoID, repo.GetString("git_url"), branch, auth, workspace); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to sync repository: %v", err)})
		}

		repoDir := filepath.Join(workspace, repoID)
		var files []string
		err = filepath.WalkDir(repoDir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == "vendor" {
					return filepath.SkipDir
				}
				return nil
			}
			ext := strings.ToLower(filepath.Ext(d.Name()))
			if ext == ".yml" || ext == ".yaml" {
				relPath, err := filepath.Rel(repoDir, path)
				if err == nil {
					files = append(files, relPath)
				}
			}
			return nil
		})
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to list files"})
		}
		if files == nil {
			files = []string{}
		}
		return e.JSON(http.StatusOK, files)
	}).BindFunc(rbac.Require(rbac.CapManageRepos))

	rr.r.GET("/api/custom/repositories/{id}/stack-files", func(e *core.RequestEvent) error {
		repoDir, ok := rr.repoFilesSetup(e)
		if !ok {
			return nil
		}
		files, err := rr.listYAMLFiles(repoDir, compose.IsComposeFile)
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to list files"})
		}
		if files == nil {
			files = []string{}
		}
		return e.JSON(http.StatusOK, files)
	}).BindFunc(rbac.Require(rbac.CapManageRepos))

	rr.r.GET("/api/custom/repositories/{id}/job-files", func(e *core.RequestEvent) error {
		repoDir, ok := rr.repoFilesSetup(e)
		if !ok {
			return nil
		}
		files, err := rr.listYAMLFiles(repoDir, job.IsJobFile)
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to list files"})
		}
		if files == nil {
			files = []string{}
		}
		return e.JSON(http.StatusOK, files)
	}).BindFunc(rbac.Require(rbac.CapManageRepos))

	rr.r.GET("/api/custom/repositories/{id}/job-definition", func(e *core.RequestEvent) error {
		if _, ok := rr.repoFilesSetup(e); !ok {
			return nil
		}

		jobFile := e.Request.URL.Query().Get("file")
		if jobFile == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "missing file parameter"})
		}

		repoID := e.Request.PathValue("id")
		def, err := job.ParseJobFile(config.GetReposWorkspace(), repoID, jobFile)
		if err != nil {
			return e.JSON(http.StatusUnprocessableEntity, map[string]any{
				"error":  err.Error(),
				"errors": validationErrors(err),
			})
		}
		return e.JSON(http.StatusOK, def)
	}).BindFunc(rbac.Require(rbac.CapManageRepos))

	rr.r.POST("/api/custom/repositories/{id}/sync", func(e *core.RequestEvent) error {
		repoDir, ok := rr.repoFilesSetup(e)
		if !ok {
			return nil
		}

		repo, err := gogit.PlainOpen(repoDir)
		if err == nil {
			if ref, err := repo.Head(); err == nil {
				repoID := e.Request.PathValue("id")
				if rec, err := rr.app.FindRecordById("repositories", repoID); err == nil {
					rec.Set("last_commit_sha", ref.Hash().String())
					_ = rr.app.Save(rec)
				}
			}
		}

		return e.JSON(http.StatusOK, map[string]string{"success": "true"})
	}).BindFunc(rbac.Require(rbac.CapManageRepos))
}

func (rr routeRegistrar) registerCredentialRoutes() {
	rr.r.POST("/api/custom/credentials/test", func(e *core.RequestEvent) error {
		var body struct {
			RepositoryID  string `json:"repository_id"`
			RepositoryKey string `json:"repository_key_id"`
			GitURL        string `json:"git_url"`
			AuthType      string `json:"auth_type"`
			SSHKey        string `json:"ssh_private_key"`
			Passphrase    string `json:"ssh_passphrase"`
			KnownHost     string `json:"ssh_known_host"`
			GitUsername   string `json:"git_username"`
			GitPassword   string `json:"git_password"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
		}

		cred := git.Credential{
			AuthType:      git.AuthType(body.AuthType),
			SSHPrivateKey: []byte(body.SSHKey),
			SSHPassphrase: []byte(body.Passphrase),
			SSHKnownHost:  body.KnownHost,
			GitUsername:   body.GitUsername,
			GitPassword:   body.GitPassword,
		}

		if body.RepositoryID != "" || body.RepositoryKey != "" {
			var savedCred *git.Credential
			var err error
			if body.RepositoryKey != "" {
				savedCred, err = git.LoadCredentialByID(rr.app, body.RepositoryKey)
			} else {
				savedCred, err = loadRepositoryCredential(rr.app, body.RepositoryID)
			}
			if err != nil {
				log.Printf("TestConnection: failed to load credentials: %v", err)
			}
			if err == nil && savedCred != nil {
				if cred.AuthType == git.AuthTypeNone || cred.AuthType == "" {
					cred.AuthType = savedCred.AuthType
				}
				if len(cred.SSHPrivateKey) == 0 && len(savedCred.SSHPrivateKey) > 0 {
					cred.SSHPrivateKey = savedCred.SSHPrivateKey
				}
				if len(cred.SSHPassphrase) == 0 && len(savedCred.SSHPassphrase) > 0 {
					cred.SSHPassphrase = savedCred.SSHPassphrase
				}
				if cred.SSHKnownHost == "" && savedCred.SSHKnownHost != "" {
					cred.SSHKnownHost = savedCred.SSHKnownHost
				}
				if cred.GitUsername == "" && savedCred.GitUsername != "" {
					cred.GitUsername = savedCred.GitUsername
				}
				if cred.GitPassword == "" && savedCred.GitPassword != "" {
					cred.GitPassword = savedCred.GitPassword
				}
			}
		}

		auth, err := git.ResolveTransportAuth(cred)
		if err != nil {
			return e.JSON(http.StatusOK, map[string]string{"success": "false", "error": err.Error()})
		}
		if err := git.TestConnection(body.GitURL, auth); err != nil {
			return e.JSON(http.StatusOK, map[string]string{"success": "false", "error": err.Error()})
		}
		return e.JSON(http.StatusOK, map[string]string{"success": "true"})
	}).BindFunc(rbac.Require(rbac.CapManageRepos))

	rr.r.POST("/api/custom/credentials/keyscan", func(e *core.RequestEvent) error {
		var body struct {
			Host string `json:"host"`
			Port int    `json:"port"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
		}
		if body.Host == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "host is required"})
		}

		var ips []net.IP
		if ip := net.ParseIP(body.Host); ip != nil {
			ips = append(ips, ip)
		} else {
			if !regexp.MustCompile(`^[a-zA-Z0-9.-]+$`).MatchString(body.Host) {
				return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid host format"})
			}
			resolved, err := net.LookupIP(body.Host)
			if err != nil {
				return e.JSON(http.StatusBadRequest, map[string]string{"error": "failed to resolve host"})
			}
			ips = resolved
		}

		allowedRanges := os.Getenv("ALLOWED_PRIVATE_IP_RANGES")
		isIPAllowed := func(ip net.IP) bool {
			if allowedRanges == "" {
				return false
			}
			for _, part := range strings.Split(allowedRanges, ",") {
				part = strings.TrimSpace(part)
				if part == "" {
					continue
				}
				if _, ipNet, err := net.ParseCIDR(part); err == nil {
					if ipNet.Contains(ip) {
						return true
					}
				} else if parsedIP := net.ParseIP(part); parsedIP != nil && parsedIP.Equal(ip) {
					return true
				}
			}
			return false
		}

		for _, ip := range ips {
			if isIPAllowed(ip) {
				continue
			}
			if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalMulticast() || ip.IsLinkLocalUnicast() {
				return e.JSON(http.StatusForbidden, map[string]string{"error": "scanning private or loopback addresses is not allowed"})
			}
		}
		result, err := git.ScanHostKey(body.Host, body.Port)
		if err != nil {
			return e.JSON(http.StatusOK, map[string]string{"success": "false", "error": err.Error()})
		}
		return e.JSON(http.StatusOK, map[string]string{"success": "true", "result": result})
	}).BindFunc(rbac.Require(rbac.CapManageRepos))
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
			if envRecords, envLoadErr := rr.app.FindAllRecords("stack_env_vars", dbx.HashExp{"stack": stackID}); envLoadErr == nil {
				secretKey := crypto.NormalizeSecretKey(os.Getenv("SECRET_KEY"))
				var envVars []string
				for _, rec := range envRecords {
					key := rec.GetString("key")
					if key == "" {
						continue
					}
					val := rec.GetString("value")
					if rec.GetBool("secret") {
						dec, decErr := crypto.Decrypt(val, secretKey)
						if decErr != nil {
							log.Printf("[routes] teardown: skipping secret env var %q for stack %s: %v", key, stackID, decErr)
							continue
						}
						val = string(dec)
					}
					envVars = append(envVars, key+"="+val)
				}
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

		for _, col := range []string{"sync_logs", "stack_services", "stack_env_vars", "stack_revisions", "stack_pending_reconciles"} {
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

func (rr routeRegistrar) registerSystemRoutes() {
	rr.r.GET("/api/custom/orphans", func(e *core.RequestEvent) error {
		workspace := config.GetReposWorkspace()
		entries, err := os.ReadDir(workspace)
		if err != nil {
			return e.JSON(http.StatusOK, []any{})
		}

		repos, _ := rr.app.FindAllRecords("repositories")
		tracked := make(map[string]bool, len(repos))
		for _, repo := range repos {
			tracked[repo.Id] = true
		}

		type orphanInfo struct {
			DirName     string `json:"dir_name"`
			ComposeFile string `json:"compose_file"`
			HasCompose  bool   `json:"has_compose"`
		}

		var orphans []orphanInfo
		for _, entry := range entries {
			if !entry.IsDir() || tracked[entry.Name()] {
				continue
			}
			dirPath := filepath.Join(workspace, entry.Name())
			composeFile := ""
			hasCompose := false
			for _, name := range []string{"docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml"} {
				if _, err := os.Stat(filepath.Join(dirPath, name)); err == nil {
					composeFile = name
					hasCompose = true
					break
				}
			}
			orphans = append(orphans, orphanInfo{DirName: entry.Name(), ComposeFile: composeFile, HasCompose: hasCompose})
		}

		if orphans == nil {
			orphans = []orphanInfo{}
		}
		return e.JSON(http.StatusOK, orphans)
	}).BindFunc(rbac.Require(rbac.CapManageSettings))

	rr.r.GET("/api/custom/system/info", func(e *core.RequestEvent) error {
		workspace := config.GetReposWorkspace()
		var diskUsage int64
		filepath.Walk(workspace, func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				diskUsage += info.Size()
			}
			return nil
		})

		return e.JSON(http.StatusOK, map[string]interface{}{
			"version":        "1.0.0",
			"disk_usage":     diskUsage,
			"workspace_path": workspace,
		})
	}).BindFunc(rbac.Require(rbac.CapManageSettings))

	rr.r.DELETE("/api/custom/orphans/{dirName}", func(e *core.RequestEvent) error {
		dirName := e.Request.PathValue("dirName")
		if dirName == "" || strings.Contains(dirName, "..") || strings.Contains(dirName, "/") {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid directory name"})
		}

		dirPath := filepath.Join(config.GetReposWorkspace(), dirName)
		if _, err := os.Stat(dirPath); os.IsNotExist(err) {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "directory not found"})
		}

		for _, name := range []string{"docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml"} {
			if _, err := os.Stat(filepath.Join(dirPath, name)); err == nil {
				output, downErr := compose.RunDownPurge(context.Background(), compose.RunOptions{
					WorkDir:     dirPath,
					ComposeFile: name,
				})
				if downErr != nil {
					log.Printf("[routes] orphan compose down for %s: %v (output: %s)", dirName, downErr, output)
				}
				break
			}
		}

		if err := os.RemoveAll(dirPath); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to remove directory: %v", err)})
		}
		return e.JSON(http.StatusOK, map[string]string{"status": "purged"})
	}).BindFunc(rbac.Require(rbac.CapManageSettings))
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

func (rr routeRegistrar) registerIntegrationRoutes() {
	rr.r.GET("/api/custom/integrations", func(e *core.RequestEvent) error {
		recs, err := rr.app.FindAllRecords("integrations", dbx.NewExp("1=1"))
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		saved := make(map[string]*core.Record)
		for _, rec := range recs {
			saved[rec.GetString("slug")] = rec
		}

		type IntegrationOutput struct {
			Slug     string                 `json:"slug"`
			Name     string                 `json:"name"`
			Category string                 `json:"category"`
			Enabled  bool                   `json:"enabled"`
			Config   map[string]interface{} `json:"config"`
		}

		var out []IntegrationOutput
		for _, impl := range integrations.All() {
			slug := impl.Slug()
			item := IntegrationOutput{
				Slug:     slug,
				Name:     impl.Name(),
				Category: impl.Category(),
				Enabled:  false,
				Config:   map[string]interface{}{},
			}
			if rec, exists := saved[slug]; exists {
				item.Enabled = rec.GetBool("enabled")
				var cfg map[string]interface{}
				if err := rec.UnmarshalJSONField("config", &cfg); err == nil && cfg != nil {
					if slug == "webhook" || slug == "ntfy" {
						if secretVal, ok := cfg["secret"].(string); ok && secretVal != "" {
							cfg["secret"] = notify.MaskSecret(secretVal)
						}
					}
					item.Config = cfg
				}
			}
			out = append(out, item)
		}
		return e.JSON(http.StatusOK, out)
	}).BindFunc(rbac.Require(rbac.CapManageSettings))

	rr.r.PUT("/api/custom/integrations/{slug}", func(e *core.RequestEvent) error {
		slug := e.Request.PathValue("slug")
		if _, exists := integrations.Get(slug); !exists {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid integration slug"})
		}

		var body struct {
			Enabled bool                   `json:"enabled"`
			Config  map[string]interface{} `json:"config"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
		}
		if body.Config != nil {
			if secretVal, ok := body.Config["secret"].(string); ok && secretVal == "••••••••" {
				recs, err := rr.app.FindAllRecords("integrations", dbx.HashExp{"slug": slug})
				if err != nil {
					return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to query existing integration record: " + err.Error()})
				}
				if len(recs) == 0 {
					return e.JSON(http.StatusBadRequest, map[string]string{"error": "cannot resolve masked secret: no existing integration record found"})
				}
				var savedConfig map[string]interface{}
				if err := recs[0].UnmarshalJSONField("config", &savedConfig); err != nil {
					return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to unmarshal existing configuration: " + err.Error()})
				}
				if savedConfig == nil {
					return e.JSON(http.StatusBadRequest, map[string]string{"error": "cannot resolve masked secret: existing configuration is empty"})
				}
				savedSecret, ok := savedConfig["secret"].(string)
				if !ok || savedSecret == "" {
					return e.JSON(http.StatusBadRequest, map[string]string{"error": "cannot resolve masked secret: no secret found in existing configuration"})
				}
				body.Config["secret"] = savedSecret
			}
		}

		col, err := rr.app.FindCollectionByNameOrId("integrations")
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		var rec *core.Record
		recs, err := rr.app.FindAllRecords("integrations", dbx.HashExp{"slug": slug})
		if err == nil && len(recs) > 0 {
			rec = recs[0]
		} else {
			rec = core.NewRecord(col)
			rec.Set("slug", slug)
		}

		rec.Set("enabled", body.Enabled)
		rec.Set("config", body.Config)
		if err := rr.app.Save(rec); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		if body.Config != nil {
			if secretVal, ok := body.Config["secret"].(string); ok && secretVal != "" {
				body.Config["secret"] = "••••••••"
			}
		}
		return e.JSON(http.StatusOK, map[string]interface{}{
			"slug":    slug,
			"enabled": body.Enabled,
			"config":  body.Config,
		})
	}).BindFunc(rbac.Require(rbac.CapManageSettings))

	rr.r.DELETE("/api/custom/integrations/{slug}", func(e *core.RequestEvent) error {
		slug := e.Request.PathValue("slug")
		if _, exists := integrations.Get(slug); !exists {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid integration slug"})
		}
		recs, err := rr.app.FindRecordsByFilter("integrations", "slug = {:slug}", "", 0, 1, dbx.Params{"slug": slug})
		if err != nil || len(recs) == 0 {
			return e.JSON(http.StatusOK, map[string]string{"status": "deleted"})
		}
		if err := rr.app.Delete(recs[0]); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return e.JSON(http.StatusOK, map[string]string{"status": "deleted"})
	}).BindFunc(rbac.Require(rbac.CapManageSettings))

	rr.r.GET("/api/custom/stacks/{id}/integration-actions", func(e *core.RequestEvent) error {
		stackID := e.Request.PathValue("id")
		recs, err := rr.app.FindAllRecords("integrations", dbx.HashExp{"enabled": true})
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		if len(recs) == 0 {
			return e.JSON(http.StatusOK, map[string][]integrations.ContainerAction{})
		}

		activePlugins := make([]struct {
			Plugin integrations.Integration
			Config map[string]interface{}
		}, 0)
		for _, rec := range recs {
			slug := rec.GetString("slug")
			if plugin, exists := integrations.Get(slug); exists {
				var cfg map[string]interface{}
				_ = rec.UnmarshalJSONField("config", &cfg)
				if cfg == nil {
					cfg = make(map[string]interface{})
				}
				activePlugins = append(activePlugins, struct {
					Plugin integrations.Integration
					Config map[string]interface{}
				}{plugin, cfg})
			}
		}

		stack, err := rr.app.FindRecordById("stacks", stackID)
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "stack not found"})
		}

		projectName := compose.ProjectName(stackWorkDir(rr.app, stack))
		assignedWorkerID := stack.GetString("worker")
		worker, err := rr.resolveWorker(assignedWorkerID)
		if err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		if rr.workerSvc == nil || !rr.workerSvc.IsConnected(assignedWorkerID) {
			return e.JSON(http.StatusServiceUnavailable, map[string]string{"error": fmt.Sprintf(OfflineWorkerMsg, worker.GetString("hostname"))})
		}

		res, err := rr.workerSvc.Dispatch(e.Request.Context(), assignedWorkerID, protocol.GetStatusCommand{
			CommandID:   fmt.Sprintf("status-actions-%s", stackID),
			ProjectName: projectName,
		})
		if err != nil || res.Error != "" {
			log.Printf("[routes] remote status dispatch failed for worker %s stack %s: %v (res.Error=%s)", assignedWorkerID, stackID, err, res.Error)
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to get remote stack status"})
		}

		var statuses []compose.ServiceStatus
		if err := json.Unmarshal([]byte(res.Output), &statuses); err != nil {
			log.Printf("[routes] failed to unmarshal remote status for worker %s stack %s: %v", assignedWorkerID, stackID, err)
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to unmarshal worker response"})
		}
		if len(statuses) == 0 {
			return e.JSON(http.StatusOK, map[string][]integrations.ContainerAction{})
		}

		result := make(map[string][]integrations.ContainerAction)
		for _, status := range statuses {
			ctx := integrations.ContainerContext{
				ContainerID:   status.ContainerID,
				ContainerName: status.ContainerName,
				Labels:        status.Labels,
			}
			for _, ap := range activePlugins {
				actions := ap.Plugin.ResolveContainerActions(ap.Config, ctx)
				if len(actions) > 0 {
					result[status.ContainerID] = append(result[status.ContainerID], actions...)
				}
			}
		}

		return e.JSON(http.StatusOK, result)
	}).BindFunc(rbac.Require(rbac.CapViewStacks))

	rr.r.POST("/api/custom/integrations/{slug}/test", func(e *core.RequestEvent) error {
		slug := e.Request.PathValue("slug")
		if slug != "webhook" && slug != "ntfy" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "only webhook and ntfy integrations can be tested"})
		}

		var body struct {
			Enabled bool                   `json:"enabled"`
			Config  map[string]interface{} `json:"config"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
		}

		if secretVal, ok := body.Config["secret"].(string); ok && secretVal == "••••••••" {
			recs, err := rr.app.FindAllRecords("integrations", dbx.HashExp{"slug": slug})
			if err != nil {
				return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to query existing integration record: " + err.Error()})
			}
			if len(recs) == 0 {
				return e.JSON(http.StatusBadRequest, map[string]string{"error": "cannot resolve masked secret: no existing integration record found"})
			}
			var savedConfig map[string]interface{}
			if err := recs[0].UnmarshalJSONField("config", &savedConfig); err != nil {
				return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to unmarshal existing configuration: " + err.Error()})
			}
			if savedConfig == nil {
				return e.JSON(http.StatusBadRequest, map[string]string{"error": "cannot resolve masked secret: existing configuration is empty"})
			}
			savedSecret, ok := savedConfig["secret"].(string)
			if !ok || savedSecret == "" {
				return e.JSON(http.StatusBadRequest, map[string]string{"error": "cannot resolve masked secret: no secret found in existing configuration"})
			}
			body.Config["secret"] = savedSecret
		}

		notifier := notify.New(rr.app)
		cfg := notifier.BuildConfig(slug, body.Config)
		cfg.Enabled = true

		payload := notify.Payload{
			Event:     notify.SyncTest,
			StackID:   "test-stack",
			StackName: "Test Stack",
			Trigger:   "manual",
			CommitSHA: "0000000",
		}

		if err := notifier.DispatchWithConfig(e.Request.Context(), cfg, payload); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}

		return e.JSON(http.StatusOK, map[string]string{"status": "dispatched"})
	}).BindFunc(rbac.Require(rbac.CapManageSettings))
}
