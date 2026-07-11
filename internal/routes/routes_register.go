package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
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
	"github.com/wireops/wireops/internal/git"
	"github.com/wireops/wireops/internal/integrations"
	"github.com/wireops/wireops/internal/job"
	"github.com/wireops/wireops/internal/notify"
	"github.com/wireops/wireops/internal/protocol"
	"github.com/wireops/wireops/internal/rbac"
	"github.com/wireops/wireops/internal/sync"
	"github.com/wireops/wireops/internal/wireops"
)

type routeRegistrar struct {
	r         *router.Router[*core.RequestEvent]
	app       core.App
	scheduler *sync.Scheduler
	workerSvc sync.WorkerDispatcher
}

func isNotificationIntegration(slug string) bool {
	return slug == "webhook" || slug == "ntfy" || slug == "discord" || slug == "slack"
}

func sensitiveIntegrationConfigKeys(slug string) []string {
	switch slug {
	case "discord", "slack":
		return []string{"url"}
	case "webhook", "ntfy":
		return []string{"secret"}
	default:
		return nil
	}
}

func maskIntegrationConfig(slug string, cfg map[string]interface{}) {
	for _, key := range sensitiveIntegrationConfigKeys(slug) {
		if val, ok := cfg[key].(string); ok && val != "" {
			cfg[key] = notify.MaskSecret(val)
		}
	}
}

func (rr routeRegistrar) resolveMaskedIntegrationConfig(slug string, cfg map[string]interface{}) error {
	if cfg == nil {
		return nil
	}

	var savedConfig map[string]interface{}
	for _, key := range sensitiveIntegrationConfigKeys(slug) {
		val, ok := cfg[key].(string)
		if !ok || val != notify.MaskSecret("x") {
			continue
		}

		if savedConfig == nil {
			recs, err := rr.app.FindAllRecords("integrations", dbx.HashExp{"slug": slug})
			if err != nil {
				return fmt.Errorf("failed to query existing integration record: %w", err)
			}
			if len(recs) == 0 {
				return fmt.Errorf("cannot resolve masked %s: no existing integration record found", key)
			}
			if err := recs[0].UnmarshalJSONField("config", &savedConfig); err != nil {
				return fmt.Errorf("failed to unmarshal existing configuration: %w", err)
			}
			if savedConfig == nil {
				return fmt.Errorf("cannot resolve masked %s: existing configuration is empty", key)
			}
		}

		savedVal, ok := savedConfig[key].(string)
		if !ok || savedVal == "" {
			return fmt.Errorf("cannot resolve masked %s: no saved value found in existing configuration", key)
		}
		cfg[key] = savedVal
	}
	return nil
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

func (rr routeRegistrar) listFilesByBasename(repoDir string, match func(name string) bool) ([]string, error) {
	var matched []string
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
		if !match(d.Name()) {
			return nil
		}
		rel, err := filepath.Rel(repoDir, path)
		if err != nil {
			return nil
		}
		matched = append(matched, rel)
		return nil
	}); err != nil {
		return nil, err
	}
	sort.Strings(matched)
	return matched, nil
}

func (rr routeRegistrar) repoFilesSetup(e *core.RequestEvent) (string, bool) {
	repoID := e.Request.PathValue("id")
	if repoID == "" {
		_ = e.JSON(http.StatusBadRequest, map[string]string{"error": "missing id"})
		return "", false
	}
	return rr.repoFilesSetupByID(e, repoID)
}

// repoFilesSetupByID is the repoFilesSetup logic for callers that already
// have a repository ID (e.g. from a JSON body) instead of a URL path param.
func (rr routeRegistrar) repoFilesSetupByID(e *core.RequestEvent, repoID string) (string, bool) {
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

	rr.r.GET("/api/custom/repositories/{id}/wireops-files", func(e *core.RequestEvent) error {
		repoDir, ok := rr.repoFilesSetup(e)
		if !ok {
			return nil
		}
		files, err := rr.listFilesByBasename(repoDir, wireops.IsWireopsFile)
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to list files"})
		}
		if files == nil {
			files = []string{}
		}
		return e.JSON(http.StatusOK, files)
	}).BindFunc(rbac.Require(rbac.CapManageRepos))

	rr.r.GET("/api/custom/repositories/{id}/wireops-definition", func(e *core.RequestEvent) error {
		repoDir, ok := rr.repoFilesSetup(e)
		if !ok {
			return nil
		}

		wireopsFile := e.Request.URL.Query().Get("file")
		if wireopsFile == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "missing file parameter"})
		}

		repoID := e.Request.PathValue("id")
		def, err := wireops.ParseWireopsFile(config.GetReposWorkspace(), repoID, wireopsFile)
		if err != nil {
			return e.JSON(http.StatusUnprocessableEntity, map[string]any{
				"error":  err.Error(),
				"errors": wireopsValidationErrors(err),
			})
		}

		resolveWireopsComposeFile(repoDir, wireopsFile, def)

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
					maskIntegrationConfig(slug, cfg)
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
		if err := rr.resolveMaskedIntegrationConfig(slug, body.Config); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		if err := notify.ValidateIntegrationConfig(slug, body.Config); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
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
			maskIntegrationConfig(slug, body.Config)
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
		if !isNotificationIntegration(slug) {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "only notification integrations can be tested"})
		}

		var body struct {
			Enabled bool                   `json:"enabled"`
			Config  map[string]interface{} `json:"config"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
		}

		if body.Config == nil {
			body.Config = map[string]interface{}{}
		}
		if err := rr.resolveMaskedIntegrationConfig(slug, body.Config); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		if err := notify.ValidateIntegrationConfig(slug, body.Config); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
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
