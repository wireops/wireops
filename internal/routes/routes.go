package routes

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/wireops/wireops/internal/compose"
	"github.com/wireops/wireops/internal/config"
	"github.com/wireops/wireops/internal/docker"
	"github.com/wireops/wireops/internal/git"
	"github.com/wireops/wireops/internal/integrations"
	"github.com/wireops/wireops/internal/notify"
	"github.com/wireops/wireops/internal/protocol"
	"github.com/wireops/wireops/internal/safepath"
	"github.com/wireops/wireops/internal/sync"
)

func Register(r *router.Router[*core.RequestEvent], app core.App, scheduler *sync.Scheduler, dockerClient *docker.Client, agentSvc sync.AgentDispatcher) {

	// agentOnline checks if the agent assigned to the given stack is currently connected.
	// It returns true if online. If offline, writes a 503 JSON error and returns false.
	// Embedded agents and nil dispatchers are always considered online.
	agentOnline := func(e *core.RequestEvent, stackID string) bool {
		if agentSvc == nil {
			return true
		}
		stack, findErr := app.FindRecordById("stacks", stackID)
		if findErr != nil {
			return true // stack not found is handled by the individual handler
		}
		assignedAgentID := stack.GetString("agent")
		if assignedAgentID == "" || agentSvc.IsEmbedded(assignedAgentID) {
			return true
		}
		if !agentSvc.IsConnected(assignedAgentID) {
			agentHost := assignedAgentID
			if a, err := app.FindRecordById("agents", assignedAgentID); err == nil {
				agentHost = a.GetString("hostname")
			}
			e.JSON(http.StatusServiceUnavailable, map[string]string{
				"error": fmt.Sprintf("agent '%s' is offline — connect the agent before performing this action", agentHost),
			})
			return false
		}
		return true
	}

	// Trigger sync for a stack
	r.POST("/api/custom/stacks/{id}/sync", func(e *core.RequestEvent) error {
		id := e.Request.PathValue("id")
		if id == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "missing id"})
		}
		if !agentOnline(e, id) {
			return nil
		}
		scheduler.TriggerSync(id, "manual", 0)
		return e.JSON(http.StatusOK, map[string]string{"status": "triggered"})
	})

	// Rollback a stack to a specific commit
	r.POST("/api/custom/stacks/{id}/rollback", func(e *core.RequestEvent) error {
		id := e.Request.PathValue("id")
		var body struct {
			CommitSHA string `json:"commit_sha"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil || body.CommitSHA == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "commit_sha required"})
		}
		if !agentOnline(e, id) {
			return nil
		}
		scheduler.TriggerRollback(id, body.CommitSHA)
		return e.JSON(http.StatusOK, map[string]string{"status": "triggered"})
	})

	// Get webhook URL for a stack
	r.GET("/api/custom/stacks/{id}/webhook-url", func(e *core.RequestEvent) error {
		id := e.Request.PathValue("id")
		if id == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "missing id"})
		}
		webhookURL := config.GetWebhookURL(id)
		return e.JSON(http.StatusOK, map[string]string{"webhook_url": webhookURL})
	})

	// Webhook trigger for a stack
	r.POST("/api/custom/webhook/{id}", func(e *core.RequestEvent) error {
		id := e.Request.PathValue("id")
		if id == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "missing id"})
		}
		scheduler.TriggerSync(id, "webhook", 0)
		return e.JSON(http.StatusOK, map[string]string{"status": "triggered"})
	})

	// SSE log stream for a stack
	r.GET("/api/custom/stacks/{id}/stream", func(e *core.RequestEvent) error {
		id := e.Request.PathValue("id")
		if id == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "missing id"})
		}

		e.Response.Header().Set("Content-Type", "text/event-stream")
		e.Response.Header().Set("Cache-Control", "no-cache")
		e.Response.Header().Set("Connection", "keep-alive")

		logs, err := app.FindAllRecords("sync_logs",
			dbx.HashExp{"stack": id},
		)
		if err != nil {
			return err
		}

		flusher, ok := e.Response.(http.Flusher)
		for _, log := range logs {
			output := log.GetString("output")
			for _, line := range strings.Split(output, "\n") {
				fmt.Fprintf(e.Response, "data: %s\n\n", line)
			}
			if ok {
				flusher.Flush()
			}
		}

		return nil
	})

	// Force redeploy a stack with recreate options
	r.POST("/api/custom/stacks/{id}/force-redeploy", func(e *core.RequestEvent) error {
		stackID := e.Request.PathValue("id")
		if stackID == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "missing id"})
		}
		if !agentOnline(e, stackID) {
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
		scheduler.TriggerForceRedeploy(stackID, body.RecreateContainers, body.RecreateVolumes, body.RecreateNetworks)
		return e.JSON(http.StatusOK, map[string]string{"status": "triggered"})
	})

	// Get stack services (live container statuses from Docker)
	r.GET("/api/custom/stacks/{id}/services", func(e *core.RequestEvent) error {
		stackID := e.Request.PathValue("id")

		isOffline := false
		if agentSvc != nil {
			if stack, err := app.FindRecordById("stacks", stackID); err == nil {
				assignedAgentID := stack.GetString("agent")
				if assignedAgentID != "" && !agentSvc.IsEmbedded(assignedAgentID) && !agentSvc.IsConnected(assignedAgentID) {
					isOffline = true
				}
			}
		}

		// Try live Docker query first
		if dockerClient != nil && !isOffline {
			stack, err := app.FindRecordById("stacks", stackID)
			if err != nil {
				return e.JSON(http.StatusNotFound, map[string]string{"error": "stack not found"})
			}
			workDir := stackWorkDir(app, stack)
			projectName := compose.ProjectName(workDir)
			statuses, err := compose.GetStackStatus(context.Background(), dockerClient.Raw(), projectName)
			if err == nil {
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
			log.Printf("[routes] live docker query failed, falling back to DB: %v", err)
		}

		// Fallback to DB records
		services, err := app.FindAllRecords("stack_services",
			dbx.HashExp{"stack": stackID},
		)
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		result := make([]map[string]interface{}, 0, len(services))
		for _, s := range services {
			status := s.GetString("status")
			if isOffline {
				status = "unknown"
			}
			result = append(result, map[string]interface{}{
				"service_name":   s.GetString("service_name"),
				"status":         status,
				"container_id":   s.GetString("container_id"),
				"container_name": s.GetString("container_name"),
			})
		}
		return e.JSON(http.StatusOK, result)
	})

	// Get stack resources (volumes and networks) from the agent or local Docker
	r.GET("/api/custom/stacks/{id}/resources", func(e *core.RequestEvent) error {
		stackID := e.Request.PathValue("id")

		stack, err := app.FindRecordById("stacks", stackID)
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "stack not found"})
		}

		agentID := stack.GetString("agent")
		isRemote := agentSvc != nil && agentID != "" && !agentSvc.IsEmbedded(agentID)
		isOffline := isRemote && !agentSvc.IsConnected(agentID)

		empty := protocol.GetResourcesResult{
			Volumes:  []protocol.VolumeInfo{},
			Networks: []protocol.NetworkInfo{},
		}

		if isOffline {
			return e.JSON(http.StatusOK, empty)
		}

		// Derive project name from the stack's working directory.
		projectName := compose.ProjectName(stackWorkDir(app, stack))

		if isRemote {
			// Remote agent: dispatch command and decode result
			cmdID := fmt.Sprintf("resources-%s", stackID)
			result, dispatchErr := agentSvc.Dispatch(e.Request.Context(), agentID, protocol.GetResourcesCommand{
				CommandID:   cmdID,
				StackID:     stackID,
				ProjectName: projectName,
			})
			if dispatchErr != nil {
				log.Printf("[routes] get_resources dispatch error stack=%s: %v", stackID, dispatchErr)
				return e.JSON(http.StatusOK, empty)
			}
			if result.Error != "" {
				log.Printf("[routes] get_resources agent error stack=%s: %s", stackID, result.Error)
				return e.JSON(http.StatusOK, empty)
			}
			var resources protocol.GetResourcesResult
			if err := json.Unmarshal([]byte(result.Output), &resources); err != nil {
				log.Printf("[routes] get_resources decode error stack=%s: %v", stackID, err)
				return e.JSON(http.StatusOK, empty)
			}
			return e.JSON(http.StatusOK, resources)
		}

		// Embedded mode: query Docker directly
		if dockerClient == nil {
			return e.JSON(http.StatusOK, empty)
		}

		volumes, err := compose.GetStackVolumes(e.Request.Context(), dockerClient.Raw(), projectName)
		if err != nil {
			log.Printf("[routes] get_resources volumes error stack=%s: %v", stackID, err)
			volumes = []protocol.VolumeInfo{}
		}
		networks, err := compose.GetStackNetworks(e.Request.Context(), dockerClient.Raw(), projectName)
		if err != nil {
			log.Printf("[routes] get_resources networks error stack=%s: %v", stackID, err)
			networks = []protocol.NetworkInfo{}
		}

		return e.JSON(http.StatusOK, protocol.GetResourcesResult{Volumes: volumes, Networks: networks})
	})

	// Get container stats (CPU, memory, network)
	r.GET("/api/custom/stacks/{id}/container/{containerId}/stats", func(e *core.RequestEvent) error {
		if dockerClient == nil {
			return e.JSON(http.StatusServiceUnavailable, map[string]string{"error": "docker not available"})
		}
		stackID := e.Request.PathValue("id")
		containerID := e.Request.PathValue("containerId")
		if containerID == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "missing containerId"})
		}

		stack, err := app.FindRecordById("stacks", stackID)
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "stack not found"})
		}
		projectName := compose.ProjectName(stackWorkDir(app, stack))
		belongs, err := compose.ContainerBelongsToProject(e.Request.Context(), dockerClient.Raw(), containerID, projectName)
		if err != nil || !belongs {
			return e.JSON(http.StatusForbidden, map[string]string{"error": "container does not belong to stack"})
		}

		stats, err := compose.GetContainerStats(e.Request.Context(), dockerClient.Raw(), containerID)
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return e.JSON(http.StatusOK, stats)
	})

	// Get container logs (last N lines)
	r.GET("/api/custom/stacks/{id}/container/{containerId}/logs", func(e *core.RequestEvent) error {
		if dockerClient == nil {
			return e.JSON(http.StatusServiceUnavailable, map[string]string{"error": "docker not available"})
		}
		stackID := e.Request.PathValue("id")
		containerID := e.Request.PathValue("containerId")
		if containerID == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "missing containerId"})
		}

		stack, err := app.FindRecordById("stacks", stackID)
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "stack not found"})
		}
		projectName := compose.ProjectName(stackWorkDir(app, stack))
		belongs, err := compose.ContainerBelongsToProject(e.Request.Context(), dockerClient.Raw(), containerID, projectName)
		if err != nil || !belongs {
			return e.JSON(http.StatusForbidden, map[string]string{"error": "container does not belong to stack"})
		}

		tail := e.Request.URL.Query().Get("tail")
		if tail == "" {
			tail = "100"
		}
		reader, err := dockerClient.Raw().ContainerLogs(e.Request.Context(), containerID, container.LogsOptions{
			ShowStdout: true,
			ShowStderr: true,
			Tail:       tail,
			Timestamps: true,
		})
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		defer reader.Close()

		buf := new(strings.Builder)
		// Docker multiplexed stream: 8-byte header per frame
		header := make([]byte, 8)
		for {
			_, err := io.ReadFull(reader, header)
			if err != nil {
				break
			}
			size := int(header[4])<<24 | int(header[5])<<16 | int(header[6])<<8 | int(header[7])
			if size == 0 {
				continue
			}
			payload := make([]byte, size)
			_, err = io.ReadFull(reader, payload)
			buf.Write(payload)
			if err != nil {
				break
			}
		}

		return e.JSON(http.StatusOK, map[string]string{"logs": buf.String()})
	})

	// Get last N commits for a repository
	r.GET("/api/custom/repositories/{id}/commits", func(e *core.RequestEvent) error {
		repoID := e.Request.PathValue("id")
		if repoID == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "missing id"})
		}

		cleaned := filepath.Clean(repoID)
		if filepath.IsAbs(cleaned) || strings.Contains(repoID, "..") || strings.Contains(repoID, string(os.PathSeparator)) {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid repository id"})
		}

		workspace := filepath.Join(app.DataDir(), "repositories")
		repoDir := filepath.Join(workspace, cleaned)

		gitRepo, err := gogit.PlainOpen(repoDir)
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "repository not cloned yet"})
		}

		repo, err := app.FindRecordById("repositories", repoID)
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

		const limit = 5

		type commitInfo struct {
			SHA     string `json:"sha"`
			Message string `json:"message"`
			Author  string `json:"author"`
			Date    string `json:"date"`
		}
		var commits []commitInfo
		count := 0
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
	})

	// Get files for a repository (filtered to .yml and .yaml)
	r.GET("/api/custom/repositories/{id}/files", func(e *core.RequestEvent) error {
		repoID := e.Request.PathValue("id")
		if repoID == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "missing id"})
		}

		repo, err := app.FindRecordById("repositories", repoID)
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "repository not found"})
		}

		workspace := filepath.Join(app.DataDir(), "repositories")

		// Ensure we have the latest files by cloning or fetching
		var auth transport.AuthMethod
		if cred, err := loadRepositoryCredential(app, repoID); err == nil && cred != nil {
			if resolvedAuth, err := git.ResolveAuth(*cred); err == nil {
				auth = toTransportAuth(resolvedAuth)
			}
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

			// Skip .git and common large directories
			if d.IsDir() {
				if d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == "vendor" {
					return filepath.SkipDir
				}
				return nil
			}

			// Add only .yml and .yaml files
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
			files = []string{} // Return empty array instead of null
		}

		return e.JSON(http.StatusOK, files)
	})

	// Test git credentials
	r.POST("/api/custom/credentials/test", func(e *core.RequestEvent) error {
		var body struct {
			RepositoryID string `json:"repository_id"`
			GitURL       string `json:"git_url"`
			AuthType     string `json:"auth_type"`
			SSHKey       string `json:"ssh_private_key"`
			Passphrase   string `json:"ssh_passphrase"`
			KnownHost    string `json:"ssh_known_host"`
			GitUsername  string `json:"git_username"`
			GitPassword  string `json:"git_password"`
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

		// Load saved credentials if repository_id is provided and fields are empty
		if body.RepositoryID != "" {
			savedCred, err := loadRepositoryCredential(app, body.RepositoryID)
			if err == nil && savedCred != nil {
				// Override with saved values only if form fields are empty
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

		auth, err := git.ResolveAuth(cred)
		if err != nil {
			return e.JSON(http.StatusOK, map[string]string{"success": "false", "error": err.Error()})
		}

		transportAuth := toTransportAuth(auth)
		if err := git.TestConnection(body.GitURL, transportAuth); err != nil {
			return e.JSON(http.StatusOK, map[string]string{"success": "false", "error": err.Error()})
		}

		return e.JSON(http.StatusOK, map[string]string{"success": "true"})
	})

	// SSH keyscan
	r.POST("/api/custom/credentials/keyscan", func(e *core.RequestEvent) error {
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
				} else if parsedIP := net.ParseIP(part); parsedIP != nil {
					if parsedIP.Equal(ip) {
						return true
					}
				}
			}
			return false
		}

		for _, ip := range ips {
			if isIPAllowed(ip) {
				continue // Skip restrictions for explicitly allowed IPs
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
	})

	// Get docker-compose file content
	r.GET("/api/custom/stacks/{id}/compose", func(e *core.RequestEvent) error {
		stackID := e.Request.PathValue("id")
		stack, err := app.FindRecordById("stacks", stackID)
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "stack not found"})
		}

		currentVersion := stack.GetInt("current_version")
		if currentVersion > 0 {
			// Serve the rendered, label-injected file
			renderer := sync.NewRenderer(app)
			filePath := renderer.GetRevisionFilePath(stackID, currentVersion)
			data, err := os.ReadFile(filePath)
			if err == nil {
				return e.JSON(http.StatusOK, map[string]string{
					"content":  string(data),
					"filename": fmt.Sprintf("v%d.yml", currentVersion),
				})
			}
			// If file missing but version > 0, fallback to original logic
			log.Printf("[routes] rendered compose v%d missing for stack %s: %v. Falling back to repo file.", currentVersion, stackID, err)
		}

		// For local stacks, fall back to reading from import_path directly.
		if stack.GetString("source_type") == "local" {
			importPath := stack.GetString("import_path")
			data, err := os.ReadFile(importPath)
			if err != nil {
				return e.JSON(http.StatusNotFound, map[string]string{"error": "compose file not found"})
			}
			return e.JSON(http.StatusOK, map[string]string{
				"content":  string(data),
				"filename": filepath.Base(importPath),
			})
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

		repoID := stack.GetString("repository")
		workspace := filepath.Join(app.DataDir(), "repositories")
		workDir := filepath.Join(workspace, repoID)
		if composePath != "" && composePath != "." {
			workDir = filepath.Join(workDir, composePath)
		}

		fullPath := filepath.Join(workDir, composeFile)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "compose file not found"})
		}

		return e.JSON(http.StatusOK, map[string]string{"content": string(data), "filename": composeFile})
	})

	// Stop a container
	r.POST("/api/custom/stacks/{id}/container/stop", func(e *core.RequestEvent) error {
		if dockerClient == nil {
			return e.JSON(http.StatusServiceUnavailable, map[string]string{"error": "docker not available"})
		}
		stackID := e.Request.PathValue("id")
		var body struct {
			ContainerID string `json:"container_id"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil || body.ContainerID == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "container_id required"})
		}

		stack, err := app.FindRecordById("stacks", stackID)
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "stack not found"})
		}
		projectName := compose.ProjectName(stackWorkDir(app, stack))
		belongs, err := compose.ContainerBelongsToProject(e.Request.Context(), dockerClient.Raw(), body.ContainerID, projectName)
		if err != nil || !belongs {
			return e.JSON(http.StatusForbidden, map[string]string{"error": "container does not belong to stack"})
		}

		timeout := 10
		if err := dockerClient.Raw().ContainerStop(e.Request.Context(), body.ContainerID, container.StopOptions{Timeout: &timeout}); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return e.JSON(http.StatusOK, map[string]string{"status": "stopped"})
	})

	// Restart a container
	r.POST("/api/custom/stacks/{id}/container/restart", func(e *core.RequestEvent) error {
		if dockerClient == nil {
			return e.JSON(http.StatusServiceUnavailable, map[string]string{"error": "docker not available"})
		}
		stackID := e.Request.PathValue("id")
		var body struct {
			ContainerID string `json:"container_id"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil || body.ContainerID == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "container_id required"})
		}

		stack, err := app.FindRecordById("stacks", stackID)
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "stack not found"})
		}
		projectName := compose.ProjectName(stackWorkDir(app, stack))
		belongs, err := compose.ContainerBelongsToProject(e.Request.Context(), dockerClient.Raw(), body.ContainerID, projectName)
		if err != nil || !belongs {
			return e.JSON(http.StatusForbidden, map[string]string{"error": "container does not belong to stack"})
		}

		timeout := 10
		if err := dockerClient.Raw().ContainerRestart(e.Request.Context(), body.ContainerID, container.StopOptions{Timeout: &timeout}); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return e.JSON(http.StatusOK, map[string]string{"status": "restarted"})
	})

	// Delete a stack: teardown via agent first, then remove DB records
	r.DELETE("/api/custom/stacks/{id}", func(e *core.RequestEvent) error {
		stackID := e.Request.PathValue("id")
		if stackID == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "missing id"})
		}

		force := e.Request.URL.Query().Get("force") == "true"
		agentIsOnline := true

		if !force {
			if !agentOnline(e, stackID) {
				return nil
			}
		} else {
			// Even if force is true, we should check if the agent is actually online
			// to decide whether to attempt teardown or skip it
			if agentSvc != nil {
				if stack, err := app.FindRecordById("stacks", stackID); err == nil {
					assignedAgentID := stack.GetString("agent")
					if assignedAgentID != "" && !agentSvc.IsEmbedded(assignedAgentID) && !agentSvc.IsConnected(assignedAgentID) {
						agentIsOnline = false
					}
				}
			}
		}

		stack, err := app.FindRecordById("stacks", stackID)
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "stack not found"})
		}

		agentID := stack.GetString("agent")

		// Read the current rendered compose file to send to the agent
		var composeContent []byte
		renderer := sync.NewRenderer(app)
		currentVersion := stack.GetInt("current_version")
		if currentVersion > 0 {
			filePath := renderer.GetRevisionFilePath(stackID, currentVersion)
			composeContent, _ = os.ReadFile(filePath)
		}

		// If no rendered file exists (stack was never synced), skip teardown
		var teardownOutput string
		if len(composeContent) > 0 && agentIsOnline {
			// Generate a unique command ID for this teardown
			cmdID := fmt.Sprintf("teardown-%s", stackID)

			if agentSvc == nil || agentSvc.IsEmbedded(agentID) {
				// Embedded agent: run compose down directly using the rendered file
				tmpDir, err := os.MkdirTemp("", "wireops-teardown-*")
				if err == nil {
					defer os.RemoveAll(tmpDir)
					composeFilePath := filepath.Join(tmpDir, "docker-compose.yml")
					if writeErr := os.WriteFile(composeFilePath, composeContent, 0600); writeErr == nil {
						out, downErr := compose.RunDown(e.Request.Context(), compose.RunOptions{
							WorkDir:     tmpDir,
							ComposeFile: "docker-compose.yml",
						})
						teardownOutput = out
						if downErr != nil {
							log.Printf("[routes] compose down for stack %s: %v (output: %s)", stackID, downErr, out)
						}
					}
				}
			} else {
				// Remote agent: dispatch TeardownCommand and wait for result
				result, dispatchErr := agentSvc.Dispatch(e.Request.Context(), agentID, protocol.TeardownCommand{
					CommandID:      cmdID,
					StackID:        stackID,
					ComposeFileB64: base64.StdEncoding.EncodeToString(composeContent),
				})
				teardownOutput = result.Output
				if dispatchErr != nil {
					return e.JSON(http.StatusInternalServerError, map[string]string{
						"error": fmt.Sprintf("teardown dispatch failed: %v", dispatchErr),
					})
				}
				if result.Error != "" {
					return e.JSON(http.StatusInternalServerError, map[string]string{
						"error":          fmt.Sprintf("agent teardown failed: %s", result.Error),
						"compose_output": result.Output,
					})
				}
			}
		}

		// Unregister from scheduler
		scheduler.UnregisterStack(stackID)

		// Delete all related records before deleting the stack itself
		// (PocketBase enforces that relations are deleted first)
		for _, col := range []string{"sync_logs", "stack_services", "stack_env_vars", "stack_revisions"} {
			records, _ := app.FindAllRecords(col, dbx.HashExp{"stack": stackID})
			for _, rec := range records {
				_ = app.Delete(rec)
			}
		}

		// Delete rendered compose files from disk
		stackStorageDir := filepath.Dir(renderer.GetRevisionFilePath(stackID, 1))
		if err := os.RemoveAll(stackStorageDir); err != nil {
			log.Printf("[routes] failed to remove stack storage dir for %s: %v", stackID, err)
		}

		// Delete the stack record
		if err := app.Delete(stack); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		return e.JSON(http.StatusOK, map[string]string{"status": "deleted", "compose_output": teardownOutput})
	})

	// Transfer a stack from its current agent to another agent
	r.POST("/api/custom/stacks/{id}/transfer", func(e *core.RequestEvent) error {
		stackID := e.Request.PathValue("id")
		if stackID == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "missing id"})
		}

		var body struct {
			TargetAgentID string `json:"target_agent_id"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil || body.TargetAgentID == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "target_agent_id required"})
		}

		stack, err := app.FindRecordById("stacks", stackID)
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "stack not found"})
		}

		if stack.GetString("agent") == body.TargetAgentID {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "target agent is the same as the current agent"})
		}

		// Both source and target agents must be online
		if !agentOnline(e, stackID) {
			return nil
		}
		if agentSvc != nil && !agentSvc.IsConnected(body.TargetAgentID) {
			targetHost := body.TargetAgentID
			if a, err := app.FindRecordById("agents", body.TargetAgentID); err == nil {
				targetHost = a.GetString("hostname")
			}
			return e.JSON(http.StatusServiceUnavailable, map[string]string{
				"error": fmt.Sprintf("target agent '%s' is offline — connect the agent before transferring", targetHost),
			})
		}

		scheduler.TriggerTransfer(stackID, body.TargetAgentID)
		return e.JSON(http.StatusAccepted, map[string]string{"status": "transfer_started"})
	})

	// List orphan directories in repos workspace
	r.GET("/api/custom/orphans", func(e *core.RequestEvent) error {
		workspace := filepath.Join(app.DataDir(), "repositories")

		entries, err := os.ReadDir(workspace)
		if err != nil {
			return e.JSON(http.StatusOK, []any{})
		}

		// Collect all repository IDs that are actively tracked
		repos, _ := app.FindAllRecords("repositories")
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
			if !entry.IsDir() {
				continue
			}
			if tracked[entry.Name()] {
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
			orphans = append(orphans, orphanInfo{
				DirName:     entry.Name(),
				ComposeFile: composeFile,
				HasCompose:  hasCompose,
			})
		}

		if orphans == nil {
			orphans = []orphanInfo{}
		}
		return e.JSON(http.StatusOK, orphans)
	})

	// Get system info
	r.GET("/api/custom/system/info", func(e *core.RequestEvent) error {
		ctx := context.Background()

		// Get Docker version
		dockerVersion := "N/A"
		composeVersion := "N/A"
		if dockerClient != nil {
			version, err := dockerClient.Raw().ServerVersion(ctx)
			if err == nil {
				dockerVersion = version.Version
			}
			// Get compose version by running docker compose version
			cmd := exec.CommandContext(ctx, "docker", "compose", "version", "--short")
			if output, err := cmd.Output(); err == nil {
				composeVersion = strings.TrimSpace(string(output))
			}
		}

		// Count repos and stacks
		repos, _ := app.FindAllRecords("repositories")
		stacks, _ := app.FindAllRecords("stacks")

		// Get workspace disk usage
		workspace := filepath.Join(app.DataDir(), "repositories")
		var diskUsage int64
		filepath.Walk(workspace, func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				diskUsage += info.Size()
			}
			return nil
		})

		return e.JSON(http.StatusOK, map[string]interface{}{
			"version":         "1.0.0", // TODO: read from version file or build tag
			"docker_version":  dockerVersion,
			"compose_version": composeVersion,
			"repositories":    len(repos),
			"stacks":          len(stacks),
			"disk_usage":      diskUsage,
			"workspace_path":  workspace,
		})
	})

	// Purge an orphan directory (compose down -v + remove dir)
	r.DELETE("/api/custom/orphans/{dirName}", func(e *core.RequestEvent) error {
		dirName := e.Request.PathValue("dirName")
		if dirName == "" || strings.Contains(dirName, "..") || strings.Contains(dirName, "/") {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid directory name"})
		}

		workspace := filepath.Join(app.DataDir(), "repositories")

		dirPath := filepath.Join(workspace, dirName)
		if _, err := os.Stat(dirPath); os.IsNotExist(err) {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "directory not found"})
		}

		// Try compose down -v if a compose file exists
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

		// Remove the directory
		if err := os.RemoveAll(dirPath); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to remove directory: %v", err)})
		}

		return e.JSON(http.StatusOK, map[string]string{"status": "purged"})
	})

	// --- Sync event webhook (global singleton) ---

	// GET: return current config (secret masked)
	r.GET("/api/custom/sync-events-webhook", func(e *core.RequestEvent) error {
		records, err := app.FindAllRecords("stack_sync_events")
		if err != nil || len(records) == 0 {
			return e.JSON(http.StatusOK, nil)
		}
		rec := records[0]
		return e.JSON(http.StatusOK, map[string]interface{}{
			"id":            rec.Id,
			"provider":      rec.GetString("provider"),
			"url":           rec.GetString("url"),
			"secret":        notify.MaskSecret(rec.GetString("secret")),
			"events":        rec.GetStringSlice("events"),
			"headers":       rec.GetString("headers"),
			"enabled":       rec.GetBool("enabled"),
			"ntfy_user":     rec.GetString("ntfy_user"),
			"ntfy_topic":    rec.GetString("ntfy_topic"),
			"ntfy_template": rec.GetString("ntfy_template"),
		})
	})

	// PUT: upsert provider config (enabled flag is managed separately via PATCH)
	r.PUT("/api/custom/sync-events-webhook", func(e *core.RequestEvent) error {
		var body struct {
			Provider     string   `json:"provider"`
			URL          string   `json:"url"`
			Secret       string   `json:"secret"`
			Events       []string `json:"events"`
			Headers      string   `json:"headers"`
			NtfyUser     string   `json:"ntfy_user"`
			NtfyTopic    string   `json:"ntfy_topic"`
			NtfyTemplate string   `json:"ntfy_template"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
		}

		records, _ := app.FindAllRecords("stack_sync_events")
		var rec *core.Record
		if len(records) > 0 {
			rec = records[0]
		} else {
			col, err := app.FindCollectionByNameOrId("stack_sync_events")
			if err != nil {
				return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
			}
			rec = core.NewRecord(col)
		}

		rec.Set("provider", body.Provider)
		rec.Set("url", body.URL)
		rec.Set("events", body.Events)
		rec.Set("ntfy_user", body.NtfyUser)
		rec.Set("ntfy_topic", body.NtfyTopic)
		rec.Set("ntfy_template", body.NtfyTemplate)
		if body.Headers == "" {
			body.Headers = "[]"
		}
		rec.Set("headers", body.Headers)
		// Only update secret if a non-masked value is provided.
		if body.Secret != "" && body.Secret != notify.MaskSecret(body.Secret) {
			rec.Set("secret", body.Secret)
		}

		if err := app.Save(rec); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return e.JSON(http.StatusOK, map[string]string{"status": "saved"})
	})

	// PATCH: toggle notifications enabled at the settings level
	r.PATCH("/api/custom/sync-events-webhook/enabled", func(e *core.RequestEvent) error {
		var body struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
		}

		records, _ := app.FindAllRecords("stack_sync_events")
		var rec *core.Record
		if len(records) > 0 {
			rec = records[0]
		} else {
			col, err := app.FindCollectionByNameOrId("stack_sync_events")
			if err != nil {
				return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
			}
			rec = core.NewRecord(col)
		}

		rec.Set("enabled", body.Enabled)
		if err := app.Save(rec); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return e.JSON(http.StatusOK, map[string]string{"status": "saved"})
	})

	// DELETE: remove config
	r.DELETE("/api/custom/sync-events-webhook", func(e *core.RequestEvent) error {
		records, err := app.FindAllRecords("stack_sync_events")
		if err != nil || len(records) == 0 {
			return e.JSON(http.StatusOK, map[string]string{"status": "not_found"})
		}
		if err := app.Delete(records[0]); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return e.JSON(http.StatusOK, map[string]string{"status": "deleted"})
	})

	// Discover unmanaged Docker Compose projects on a given agent host
	r.GET("/api/custom/stacks/import/discover", func(e *core.RequestEvent) error {
		agentID := e.Request.URL.Query().Get("agent")

		isRemote := agentSvc != nil && agentID != "" && !agentSvc.IsEmbedded(agentID)
		if isRemote && !agentSvc.IsConnected(agentID) {
			agentHost := agentID
			if a, err := app.FindRecordById("agents", agentID); err == nil {
				agentHost = a.GetString("hostname")
			}
			return e.JSON(http.StatusServiceUnavailable, map[string]string{
				"error": fmt.Sprintf("agent '%s' is offline", agentHost),
			})
		}

		if isRemote {
			cmdID := fmt.Sprintf("discover-%s", agentID)
			result, err := agentSvc.Dispatch(e.Request.Context(), agentID, protocol.DiscoverProjectsCommand{
				CommandID: cmdID,
			})
			if err != nil {
				return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
			}
			if result.Error != "" {
				return e.JSON(http.StatusInternalServerError, map[string]string{"error": result.Error})
			}
			var res protocol.DiscoverProjectsResult
			if err := json.Unmarshal([]byte(result.Output), &res); err != nil {
				return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to decode agent response"})
			}
			return e.JSON(http.StatusOK, res.Projects)
		}

		// Embedded mode: query Docker directly
		if dockerClient == nil {
			return e.JSON(http.StatusServiceUnavailable, map[string]string{"error": "docker not available"})
		}
		f := filters.NewArgs()
		f.Add("label", "com.docker.compose.project")
		containers, err := dockerClient.Raw().ContainerList(e.Request.Context(), container.ListOptions{
			All:     true,
			Filters: f,
		})
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		projects := make(map[string]*protocol.DiscoveredProject)
		for _, cnt := range containers {
			if cnt.Labels["dev.wireops.managed"] == "true" {
				continue
			}
			projectName := cnt.Labels["com.docker.compose.project"]
			if projectName == "" {
				continue
			}
			if _, exists := projects[projectName]; !exists {
				projects[projectName] = &protocol.DiscoveredProject{
					ProjectName: projectName,
					ComposePath: cnt.Labels["com.docker.compose.project.working_dir"],
					Services:    []string{},
				}
			}
			svcName := cnt.Labels["com.docker.compose.service"]
			if svcName == "" {
				continue
			}
			proj := projects[projectName]
			found := false
			for _, s := range proj.Services {
				if s == svcName {
					found = true
					break
				}
			}
			if !found {
				proj.Services = append(proj.Services, svcName)
			}
		}

		result := make([]protocol.DiscoveredProject, 0, len(projects))
		for _, p := range projects {
			result = append(result, *p)
		}
		return e.JSON(http.StatusOK, result)
	})

	// Import a local Docker Compose stack into wireops
	r.POST("/api/custom/stacks/import", func(e *core.RequestEvent) error {
		var body struct {
			Name            string `json:"name"`
			AgentID         string `json:"agent_id"`
			ImportPath      string `json:"import_path"`
			RecreateVolumes bool   `json:"recreate_volumes"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
		}
		if body.Name == "" || body.ImportPath == "" || body.AgentID == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "name, agent_id, and import_path are required"})
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

		agentRecord, err := app.FindRecordById("agents", body.AgentID)
		if err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "agent not found"})
		}

		isEmbedded := agentRecord.GetString("fingerprint") == "embedded" || agentSvc == nil || agentSvc.IsEmbedded(body.AgentID)
		isRemote := !isEmbedded

		if isRemote && !agentSvc.IsConnected(body.AgentID) {
			return e.JSON(http.StatusServiceUnavailable, map[string]string{
				"error": fmt.Sprintf("agent '%s' is offline", agentRecord.GetString("hostname")),
			})
		}

		// Validate the file exists by reading it
		if isEmbedded {
			if _, err := os.Stat(body.ImportPath); err != nil {
				return e.JSON(http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("compose file not found: %v", err)})
			}
		} else {
			validateID := fmt.Sprintf("validate-import-%s", body.AgentID)
			result, dispatchErr := agentSvc.Dispatch(e.Request.Context(), body.AgentID, protocol.ReadFileCommand{
				CommandID: validateID,
				Path:      body.ImportPath,
			})
			if dispatchErr != nil || result.Error != "" {
				errMsg := fmt.Sprintf("cannot access compose file on agent: %v %s", dispatchErr, result.Error)
				return e.JSON(http.StatusBadRequest, map[string]string{"error": errMsg})
			}
		}

		// Create the stack record
		stacksCol, err := app.FindCollectionByNameOrId("stacks")
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		stack := core.NewRecord(stacksCol)
		stack.Set("name", body.Name)
		stack.Set("agent", body.AgentID)
		stack.Set("source_type", "local")
		stack.Set("import_path", body.ImportPath)
		stack.Set("import_recreate_volumes", body.RecreateVolumes)
		stack.Set("status", "pending")
		stack.Set("auto_sync", false)
		if err := app.Save(stack); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		scheduler.TriggerSync(stack.Id, "manual", 0)
		log.Printf("[routes] import stack=%s agent=%s path=%s", stack.Id, body.AgentID, body.ImportPath)
		return e.JSON(http.StatusOK, map[string]string{"id": stack.Id, "status": "import_triggered"})
	})

	// API custom integrations
	r.GET("/api/custom/integrations", func(e *core.RequestEvent) error {
		// Pass an empty string query or dbx.NewExp("") rather than empty HashExp{} which fails parsing
		recs, err := app.FindAllRecords("integrations", dbx.NewExp("1=1"))
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		
		saved := make(map[string]*core.Record)
		for _, r := range recs {
			saved[r.GetString("slug")] = r
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
					item.Config = cfg
				}
			}
			out = append(out, item)
		}
		return e.JSON(http.StatusOK, out)
	})

	r.PUT("/api/custom/integrations/{slug}", func(e *core.RequestEvent) error {
		slug := e.Request.PathValue("slug")
		
		var body struct {
			Enabled bool                   `json:"enabled"`
			Config  map[string]interface{} `json:"config"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
		}

		col, err := app.FindCollectionByNameOrId("integrations")
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		var rec *core.Record
		recs, err := app.FindAllRecords("integrations", dbx.HashExp{"slug": slug})
		if err == nil && len(recs) > 0 {
			rec = recs[0]
		} else {
			rec = core.NewRecord(col)
			rec.Set("slug", slug)
		}

		rec.Set("enabled", body.Enabled)
		rec.Set("config", body.Config)
		
		if err := app.Save(rec); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		return e.JSON(http.StatusOK, map[string]interface{}{
			"slug":    slug,
			"enabled": body.Enabled,
			"config":  body.Config,
		})
	})

	r.DELETE("/api/custom/integrations/{slug}", func(e *core.RequestEvent) error {
		slug := e.Request.PathValue("slug")
		recs, err := app.FindAllRecords("integrations", dbx.HashExp{"slug": slug})
		if err != nil || len(recs) == 0 {
			return e.JSON(http.StatusOK, map[string]string{"status": "deleted"})
		}
		if err := app.Delete(recs[0]); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return e.JSON(http.StatusOK, map[string]string{"status": "deleted"})
	})

	r.GET("/api/custom/stacks/{id}/integration-actions", func(e *core.RequestEvent) error {
		stackID := e.Request.PathValue("id")
		
		// 1. Get enabled integrations
		recs, err := app.FindAllRecords("integrations", dbx.HashExp{"enabled": true})
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		if len(recs) == 0 {
			return e.JSON(http.StatusOK, map[string][]integrations.ContainerAction{})
		}

		activePlugins := make([]struct{
			Plugin integrations.Integration
			Config map[string]interface{}
		}, 0)

		for _, r := range recs {
			slug := r.GetString("slug")
			if plugin, exists := integrations.Get(slug); exists {
				var cfg map[string]interface{}
				_ = r.UnmarshalJSONField("config", &cfg)
				if cfg == nil {
					cfg = make(map[string]interface{})
				}
				activePlugins = append(activePlugins, struct{
					Plugin integrations.Integration
					Config map[string]interface{}
				}{plugin, cfg})
			}
		}

		// 2. Fetch live containers for the stack
		stack, err := app.FindRecordById("stacks", stackID)
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "stack not found"})
		}
		
		projectName := compose.ProjectName(stackWorkDir(app, stack))
		var statuses []compose.ServiceStatus
		
		isOffline := false
		if agentSvc != nil {
			assignedAgentID := stack.GetString("agent")
			if assignedAgentID != "" && !agentSvc.IsEmbedded(assignedAgentID) && !agentSvc.IsConnected(assignedAgentID) {
				isOffline = true
			}
		}

		if dockerClient != nil && !isOffline {
			statuses, _ = compose.GetStackStatus(e.Request.Context(), dockerClient.Raw(), projectName)
		} else {
			return e.JSON(http.StatusOK, map[string][]integrations.ContainerAction{})
		}

		// 3. Resolve actions
		result := make(map[string][]integrations.ContainerAction)
		for _, s := range statuses {
			ctx := integrations.ContainerContext{
				ContainerID:   s.ContainerID,
				ContainerName: s.ContainerName,
				Labels:        s.Labels,
			}
			
			for _, ap := range activePlugins {
				actions := ap.Plugin.ResolveContainerActions(ap.Config, ctx)
				if len(actions) > 0 {
					result[s.ContainerID] = append(result[s.ContainerID], actions...)
				}
			}
		}

		return e.JSON(http.StatusOK, result)
	})

	RegisterUserRoutes(r, app)

	// POST /test: send a sync.test event to the configured URL
	r.POST("/api/custom/sync-events-webhook/test", func(e *core.RequestEvent) error {
		notifier := notify.New(app)

		// 1. Load existing config (if any)
		cfg, err := notifier.LoadConfig()
		if err != nil {
			// No saved config, start fresh
			cfg = &notify.Config{}
		}

		// 2. Parse request body for overrides
		var body struct {
			Provider     string `json:"provider"`
			URL          string `json:"url"`
			Secret       string `json:"secret"`
			Headers      string `json:"headers"`
			NtfyUser     string `json:"ntfy_user"`
			NtfyTopic    string `json:"ntfy_topic"`
			NtfyTemplate string `json:"ntfy_template"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err == nil {
			if body.Provider != "" {
				cfg.Provider = body.Provider
			}
			if body.URL != "" {
				cfg.URL = body.URL
			}
			if body.Secret != "" && body.Secret != notify.MaskSecret(body.Secret) {
				cfg.Secret = body.Secret
			}
			if body.Headers != "" {
				cfg.Headers = notify.UnmarshalHeaders(body.Headers)
			}
			if body.NtfyUser != "" {
				cfg.NtfyUser = body.NtfyUser
			}
			if body.NtfyTopic != "" {
				cfg.NtfyTopic = body.NtfyTopic
			}
			if body.NtfyTemplate != "" {
				cfg.NtfyTemplate = body.NtfyTemplate
			}
		}

		// Force enable for test
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
	})
}
