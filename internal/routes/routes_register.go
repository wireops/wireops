package routes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	stdsync "sync"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/wireops/wireops/internal/buildinfo"
	"github.com/wireops/wireops/internal/compose"
	"github.com/wireops/wireops/internal/config"
	"github.com/wireops/wireops/internal/git"
	"github.com/wireops/wireops/internal/gitprovider"
	"github.com/wireops/wireops/internal/integrations"
	"github.com/wireops/wireops/internal/job"
	"github.com/wireops/wireops/internal/logstream"
	"github.com/wireops/wireops/internal/manifest"
	"github.com/wireops/wireops/internal/notify"
	"github.com/wireops/wireops/internal/protocol"
	"github.com/wireops/wireops/internal/rbac"
	"github.com/wireops/wireops/internal/sync"
)

type routeRegistrar struct {
	r         *router.Router[*core.RequestEvent]
	app       core.App
	scheduler *sync.Scheduler
	workerSvc sync.WorkerDispatcher
	logBroker *logstream.Broker
}

// isNotificationIntegration reports whether slug is one of the notify-backed
// integrations, i.e. whatever declares CapNotifier in its Descriptor — the
// /test route below only knows how to build+dispatch a notify.Config, so
// this must stay in sync with what notify.Notifier.dispatch iterates, which
// it now is by construction since both read the same descriptor metadata.
func isNotificationIntegration(slug string) bool {
	entry, ok := integrations.Get(slug)
	return ok && entry.Descriptor.HasCapability(integrations.CapNotifier)
}

// integrationDescriptor looks up slug's registered Descriptor, returning the
// zero Descriptor if the slug isn't registered — callers only use this for
// key-list lookups (SensitiveKeys/EncryptedKeys/RequiredKeys), which are
// nil-safe on a zero Descriptor.
func integrationDescriptor(slug string) integrations.Descriptor {
	entry, _ := integrations.Get(slug)
	return entry.Descriptor
}

func validateRequiredIntegrationConfig(slug string, cfg map[string]interface{}) error {
	for _, key := range integrationDescriptor(slug).RequiredKeys() {
		val, _ := cfg[key].(string)
		if strings.TrimSpace(val) == "" {
			return fmt.Errorf("%s is required", key)
		}
	}
	return nil
}

// resolveMaskedConfigForValidation returns a copy of cfg with the mask
// sentinel ("••••••••") replaced, for every Descriptor.SensitiveKeys() field,
// by the corresponding value already on file — decrypted, via
// integrations.Store.Load. Non-sensitive fields are copied through
// untouched, so callers (config validation, the /test routes) see whatever
// the operator just typed for those, even if unsaved — matching the
// pre-Store behavior this replaces.
//
// The original cfg is left untouched: callers that persist afterward
// (PUT /api/custom/integrations/{slug}) must pass the ORIGINAL cfg — with
// its sentinel still in place — to Store.Save, not this resolved copy, so
// Save's own internal sentinel-resolution carries the existing ciphertext
// through byte-identical rather than re-encrypting the now-plaintext value
// this function resolved to.
func (rr routeRegistrar) resolveMaskedConfigForValidation(store integrations.Store, slug string, cfg map[string]interface{}) (map[string]interface{}, error) {
	instance, err := store.Load(slug)
	if err != nil {
		return nil, fmt.Errorf("failed to load existing integration record: %w", err)
	}
	return resolveMaskedConfigAgainst(instance, slug, cfg)
}

// resolveMaskedConfigAgainst is resolveMaskedConfigForValidation's logic
// against an already-loaded Instance, for callers (the PUT handler) that
// have already done their own store.Load moments earlier — avoids a second
// DB fetch + decrypt pass over the same row within one request.
func resolveMaskedConfigAgainst(instance integrations.Instance, slug string, cfg map[string]interface{}) (map[string]interface{}, error) {
	resolved := make(map[string]interface{}, len(cfg))
	for k, v := range cfg {
		resolved[k] = v
	}

	for _, key := range integrationDescriptor(slug).SensitiveKeys() {
		val, ok := resolved[key].(string)
		if !ok || val != integrations.MaskSecret("x") {
			continue
		}

		storedVal, ok := instance.Config[key].(string)
		if !ok || storedVal == "" {
			return nil, fmt.Errorf("cannot resolve masked %s: no saved value found in existing configuration", key)
		}
		resolved[key] = storedVal
	}
	return resolved, nil
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

// skipDir reports whether a directory encountered during a repo file walk
// should be skipped entirely (filepath.SkipDir), e.g. VCS metadata and
// dependency directories that are never relevant to compose/job/wireops files.
func skipDir(name string) bool {
	return name == ".git" || name == "node_modules" || name == "vendor"
}

func (rr routeRegistrar) listYAMLFiles(repoDir string, filter func([]byte) bool) ([]string, error) {
	var candidates []string
	if err := filepath.WalkDir(repoDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDir(d.Name()) {
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
			if skipDir(d.Name()) {
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
	return rr.repoFilesSetupByIDContext(e, e.Request.Context(), repoID)
}

// repoFilesSetupByIDContext is repoFilesSetupByID with an explicit context for
// the clone/fetch. Callers that bound their own work with a timeout need the
// git operation inside that bound too — otherwise a hung remote holds the
// handler open regardless of the caller's deadline.
func (rr routeRegistrar) repoFilesSetupByIDContext(e *core.RequestEvent, ctx context.Context, repoID string) (string, bool) {
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
	if _, err := git.CloneOrFetchContext(ctx, repoID, repo.GetString("git_url"), branch, auth, workspace); err != nil {
		_ = e.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to sync repository: %v", err)})
		return "", false
	}
	return filepath.Join(workspace, filepath.Clean(repoID)), true
}

func (rr routeRegistrar) registerStreamRoutes() {
	rr.r.GET("/api/custom/stacks/{id}/stream", func(e *core.RequestEvent) error {
		id := e.Request.PathValue("id")
		if id == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "missing id"})
		}

		e.Response.Header().Set("Content-Type", "text/event-stream")
		e.Response.Header().Set("Cache-Control", "no-cache")
		e.Response.Header().Set("Connection", "keep-alive")

		// Commit the 200 status line as soon as the RBAC check above has
		// passed, before the (possibly long) wait for the first log line —
		// callers that only need to confirm access (e.g. the MCP live-log
		// bridge's pre-subscribe authorization check) must not block on
		// there being any log output yet.
		e.Response.WriteHeader(http.StatusOK)
		if flusher, ok := e.Response.(http.Flusher); ok {
			flusher.Flush()
		}

		// Subscribe before loading the historical snapshot so writes that land
		// in the gap between the two are queued on the channel rather than
		// lost; the handoff state below reconciles them against the snapshot
		// by RecordID + output length so each byte is emitted exactly once.
		sub, unsubscribe := rr.logBroker.Subscribe(id)
		defer unsubscribe()

		logs, err := rr.app.FindAllRecords("sync_logs", dbx.HashExp{"stack": id})
		if err != nil {
			return err
		}

		flusher, _ := e.Response.(http.Flusher)
		writeLines := func(text string) {
			for _, line := range strings.Split(text, "\n") {
				fmt.Fprintf(e.Response, "data: %s\n\n", line)
			}
			if flusher != nil {
				flusher.Flush()
			}
		}

		state := newStreamHandoffState()
		for _, logRecord := range logs {
			writeLines(logRecord.GetString("output"))
			state.observeSnapshot(logRecord.Id, logRecord.GetString("output"))
		}

		// Drain events queued during the gap between Subscribe and the
		// snapshot query above: apply() no-ops for anything already covered
		// by the snapshot and emits only the unseen tail for genuine gaps.
	drain:
		for {
			select {
			case ev, ok := <-sub:
				if !ok {
					return nil
				}
				if delta := state.apply(ev); delta != "" {
					writeLines(delta)
				}
			default:
				break drain
			}
		}

		ctx := e.Request.Context()
		for {
			select {
			case <-ctx.Done():
				return nil
			case ev, ok := <-sub:
				if !ok {
					return nil
				}
				if delta := state.apply(ev); delta != "" {
					writeLines(delta)
				}
			}
		}
	}).BindFunc(rbac.Require(rbac.CapViewLogs))
}

// streamHandoffState tracks, per sync_logs RecordID, how many bytes of
// cumulative output have already been written to an SSE stream. It lets the
// /stream handler reconcile broker events against the historical snapshot
// (and against each other) so the same bytes are never replayed and no
// bytes published during the subscribe/snapshot handoff are dropped.
type streamHandoffState struct {
	lastLen map[string]int
}

func newStreamHandoffState() *streamHandoffState {
	return &streamHandoffState{lastLen: make(map[string]int)}
}

// observeSnapshot records that recordID's output, as loaded from the
// historical snapshot, has already been written in full.
func (s *streamHandoffState) observeSnapshot(recordID, output string) {
	s.lastLen[recordID] = len(output)
}

// apply returns the slice of ev.Output not yet written for its record ("" if
// ev carries nothing new — a duplicate of, or already covered by, the
// snapshot or a prior event) and advances the recorded length.
func (s *streamHandoffState) apply(ev logstream.Event) string {
	prev := s.lastLen[ev.RecordID]
	if len(ev.Output) <= prev {
		return ""
	}
	delta := ev.Output[prev:]
	s.lastLen[ev.RecordID] = len(ev.Output)
	return delta
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
		files, err := rr.listFilesByBasename(repoDir, manifest.IsWireopsFile)
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
		def, err := manifest.ParseWireopsFile(config.GetReposWorkspace(), repoID, wireopsFile)
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
			"version":        buildinfo.Version,
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

func (rr routeRegistrar) registerIntegrationRoutes(secretKey []byte) {
	store := integrations.NewStore(rr.app, secretKey)

	rr.r.GET("/api/custom/integrations", func(e *core.RequestEvent) error {
		type IntegrationOutput struct {
			Slug     string                 `json:"slug"`
			Name     string                 `json:"name"`
			Category string                 `json:"category"`
			Enabled  bool                   `json:"enabled"`
			Locked   bool                   `json:"locked"`
			Config   map[string]interface{} `json:"config"`
		}

		instances, err := store.LoadAll()
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		var out []IntegrationOutput
		for _, entry := range integrations.All() {
			slug := entry.Descriptor.Slug
			instance := instances[slug]
			item := IntegrationOutput{
				Slug:     slug,
				Name:     entry.Descriptor.Name,
				Category: entry.Descriptor.Category.Label,
				Enabled:  instance.Enabled,
				Locked:   instance.Locked,
				Config:   store.Mask(slug, instance.Config),
			}
			if provider, ok := gitprovider.Get(slug); ok {
				// Git provider integrations (e.g. github) have no DB-stored
				// enabled flag to read — "enabled" is derived from whether
				// the provider's OAuth client id/secret env vars are set,
				// recomputed on every request rather than toggled manually.
				item.Enabled = provider.Configured()
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
		existing, err := store.Load(slug)
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		if existing.Locked {
			return e.JSON(http.StatusForbidden, map[string]string{"error": "this integration is always active and cannot be reconfigured"})
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

		// Resolve the mask sentinel against the decrypted stored value for
		// validation purposes only — body.Config itself keeps the sentinel
		// (or a genuinely new value) intact for Store.Save, which does its
		// own sentinel resolution against the still-encrypted stored value.
		resolved, err := resolveMaskedConfigAgainst(existing, slug, body.Config)
		if err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		if err := notify.ValidateIntegrationConfig(slug, resolved); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		if body.Enabled {
			if err := validateRequiredIntegrationConfig(slug, resolved); err != nil {
				return e.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
			}
		}

		if err := store.Save(slug, body.Enabled, body.Config); err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, integrations.ErrMaskUnresolvable) {
				status = http.StatusBadRequest
			}
			return e.JSON(status, map[string]string{"error": err.Error()})
		}

		saved, err := store.Load(slug)
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return e.JSON(http.StatusOK, map[string]interface{}{
			"slug":    slug,
			"enabled": body.Enabled,
			"config":  store.Mask(slug, saved.Config),
		})
	}).BindFunc(rbac.Require(rbac.CapManageSettings))

	rr.r.DELETE("/api/custom/integrations/{slug}", func(e *core.RequestEvent) error {
		slug := e.Request.PathValue("slug")
		if _, exists := integrations.Get(slug); !exists {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid integration slug"})
		}
		existing, err := store.Load(slug)
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		if existing.Locked {
			return e.JSON(http.StatusForbidden, map[string]string{"error": "this integration is always active and cannot be disabled"})
		}
		recs, err := rr.app.FindRecordsByFilter("integrations", "slug = {:slug}", "", 1, 0, dbx.Params{"slug": slug})
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
		instances, err := store.LoadAll()
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		activePlugins := make([]struct {
			Plugin integrations.ActionProvider
			Config map[string]interface{}
		}, 0)
		for slug, instance := range instances {
			if !instance.Enabled {
				continue
			}
			if plugin, exists := integrations.GetImpl[integrations.ActionProvider](slug); exists {
				activePlugins = append(activePlugins, struct {
					Plugin integrations.ActionProvider
					Config map[string]interface{}
				}{plugin, instance.Config})
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
			return e.JSON(http.StatusOK, map[string][]integrations.Action{})
		}

		result := make(map[string][]integrations.Action)
		for _, status := range statuses {
			scope := integrations.ActionScope{
				Kind:   integrations.ScopeContainer,
				ID:     status.ContainerID,
				Name:   status.ContainerName,
				Labels: status.Labels,
			}
			for _, ap := range activePlugins {
				actions := ap.Plugin.ResolveActions(ap.Config, scope)
				if len(actions) > 0 {
					result[status.ContainerID] = append(result[status.ContainerID], actions...)
				}
			}
		}

		return e.JSON(http.StatusOK, result)
	}).BindFunc(rbac.Require(rbac.CapViewStacks))

	rr.r.POST("/api/custom/integrations/{slug}/test", func(e *core.RequestEvent) error {
		slug := e.Request.PathValue("slug")
		if provider, ok := gitprovider.Get(slug); ok {
			return rr.testGitProviderConnection(e, provider)
		}
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
		resolved, err := rr.resolveMaskedConfigForValidation(store, slug, body.Config)
		if err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		if err := notify.ValidateIntegrationConfig(slug, resolved); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}

		notifier := notify.New(rr.app)
		cfg := notifier.BuildConfig(slug, resolved)
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

// testGitProviderConnection is the "Test connection" action for a git
// provider integration card (e.g. github): it doesn't dispatch anything,
// it just confirms the globally-connected repository_keys token (if any)
// still authenticates against the provider's API.
func (rr routeRegistrar) testGitProviderConnection(e *core.RequestEvent, provider gitprovider.Provider) error {
	if !provider.Configured() {
		return e.JSON(http.StatusOK, map[string]string{"success": "false", "error": "provider not configured"})
	}

	key, err := findOAuthRepositoryKey(rr.app, provider.Slug())
	if err != nil {
		return e.JSON(http.StatusOK, map[string]string{"success": "false", "error": "not connected yet — connect from a repository first"})
	}

	_, token, err := git.LoadOAuthToken(rr.app, key.Id)
	if err != nil {
		return e.JSON(http.StatusOK, map[string]string{"success": "false", "error": err.Error()})
	}

	if _, err := provider.ListOrganizations(e.Request.Context(), token); err != nil {
		return e.JSON(http.StatusOK, map[string]string{"success": "false", "error": err.Error()})
	}

	return e.JSON(http.StatusOK, map[string]string{"success": "true"})
}
