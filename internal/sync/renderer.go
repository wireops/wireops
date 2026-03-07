package sync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/pocketbase/pocketbase/core"
	"github.com/wireops/wireops/internal/compose"
)

// RenderResult represents the result of the label injection process.
type RenderResult struct {
	Version      int
	Checksum     string
	RenderedPath string // e.g., v5.yml
}

// Renderer is responsible for intercepting compose files and injecting deterministic labels
type Renderer struct {
	app          core.App
	stackStorage string
}

func NewRenderer(app core.App) *Renderer {
	// The storage layout for rendered compose files
	// e.g., /pb_data/stacks/<stack_id>/v1.yml
	storagePath := os.Getenv("STACKS_STORAGE_PATH")
	if storagePath == "" {
		storagePath = filepath.Join(app.DataDir(), "stacks")
	}

	return &Renderer{
		app:          app,
		stackStorage: storagePath,
	}
}

// GenerateRevision runs docker compose config, injects labels, computes the checksum, and saves v<N>.yml
func (r *Renderer) GenerateRevision(
	ctx context.Context,
	stack *core.Record,
	repo *core.Record,
	workDir string,
	composeFile string,
	envVars []string,
	commitSHA string,
	forceIncrement bool,
	agentFingerprint string,
) (*RenderResult, error) {

	stackID := stack.Id
	stackName := stack.GetString("name")

	var repoName, repoURL, branch string
	if repo != nil {
		repoName = repo.GetString("name")
		repoURL = repo.GetString("git_url")
		branch = repo.GetString("branch")
	}
	if branch == "" {
		branch = "main"
	}

	// 1. Get current compose config as JSON
	configOut, err := compose.Config(ctx, compose.ConfigOptions{
		WorkDir:     workDir,
		ComposeFile: composeFile,
		EnvVars:     envVars,
	}, true)
	if err != nil {
		return nil, fmt.Errorf("failed to get compose config: %w", err)
	}

	configMap, err := compose.ParseConfigJSON(configOut)
	if err != nil {
		return nil, err
	}

	// Validation: ensure top-level name exists
	if _, ok := configMap["name"]; !ok {
		return nil, fmt.Errorf("rendered compose file missing top-level 'name' field")
	}

	// Validation: Ensure services exist
	servicesRaw, ok := configMap["services"]
	if !ok || servicesRaw == nil {
		return nil, fmt.Errorf("no services defined in compose file")
	}
	services, ok := servicesRaw.(map[string]interface{})
	if !ok || len(services) == 0 {
		return nil, fmt.Errorf("services block is invalid or empty")
	}

	// Determine version number
	currentVersion := stack.GetInt("current_version")
	if currentVersion == 0 {
		currentVersion = 1
	}

	// Prepare labels (excluding checksum/version for initial hashing if needed,
	// but requirement says checksum is over the rendered fully).
	// Let's do a 2-pass or inject an empty checksum, serialize, hash, then update.
	// We will hash the config without `wireops.checksum`, then add it.

	// To guarantee determinism, let's inject everything except checksum
	generatedAt := time.Now().UTC().Format(time.RFC3339)

	var nextVersion = currentVersion
	if forceIncrement {
		nextVersion++
	}

	// Inject base labels into configMap
	for serviceName, svcRaw := range services {
		svc, ok := svcRaw.(map[string]interface{})
		if !ok {
			continue // skip invalid
		}

		labelsRaw, hasLabels := svc["labels"]
		var labels map[string]interface{}
		if hasLabels && labelsRaw != nil {
			var ok bool
			labels, ok = labelsRaw.(map[string]interface{})
			if !ok {
				continue // skip service with malformed labels
			}
		} else {
			labels = make(map[string]interface{})
		}

		labels["dev.wireops.managed"] = "true"
		labels["dev.wireops.stack_id"] = stackID
		labels["dev.wireops.stack_name"] = stackName
		labels["dev.wireops.repository"] = repoName
		labels["dev.wireops.repository.url"] = repoURL
		labels["dev.wireops.repository.branch"] = branch
		labels["dev.wireops.repository.file"] = composeFile
		labels["dev.wireops.version"] = strconv.Itoa(nextVersion)
		labels["dev.wireops.repository.commit_sha"] = commitSHA
		if agentFingerprint != "" {
			labels["dev.wireops.agent.fingerprint"] = agentFingerprint
		}

		svc["labels"] = labels
		services[serviceName] = svc
	}
	configMap["services"] = services

	// Calculate checksum WITHOUT generated_at
	normalizedYAML, err := normalizeYAML(configMap)
	if err != nil {
		return nil, fmt.Errorf("failed to normalize YAML for checksum: %w", err)
	}
	checksum := computeSHA256(normalizedYAML)

	// Inject checksum and generated_at AFTER hashing
	for serviceName, svcRaw := range services {
		svc, ok := svcRaw.(map[string]interface{})
		if !ok {
			continue
		}
		labels, ok := svc["labels"].(map[string]interface{})
		if !ok {
			continue
		}
		labels["dev.wireops.checksum"] = checksum
		labels["dev.wireops.generated_at"] = generatedAt
		svc["labels"] = labels
		services[serviceName] = svc
	}
	configMap["services"] = services

	// Compare with current stack state to see if we actually need a version bump
	// if we didn't already force it.
	if !forceIncrement && stack.GetString("checksum") != checksum {
		// Content changed, we need to bump the version
		if stack.GetString("checksum") != "" {
			nextVersion++
			// Need to re-inject version and re-calculate checksum
			for serviceName, svcRaw := range services {
				svc, ok := svcRaw.(map[string]interface{})
				if !ok {
					continue
				}
				labels, ok := svc["labels"].(map[string]interface{})
				if !ok {
					continue
				}
				labels["dev.wireops.version"] = strconv.Itoa(nextVersion)
				delete(labels, "dev.wireops.checksum")     // remove old
				delete(labels, "dev.wireops.generated_at") // remove time-dependent metadata for deterministic checksum
				svc["labels"] = labels
				services[serviceName] = svc
			}
			configMap["services"] = services

			normalizedYAML, _ = normalizeYAML(configMap)
			checksum = computeSHA256(normalizedYAML)

			for serviceName, svcRaw := range services {
				svc, ok := svcRaw.(map[string]interface{})
				if !ok {
					continue
				}
				labels, ok := svc["labels"].(map[string]interface{})
				if !ok {
					continue
				}
				labels["dev.wireops.checksum"] = checksum
				labels["dev.wireops.generated_at"] = generatedAt // re-inject after checksum
				svc["labels"] = labels
				services[serviceName] = svc
			}
			configMap["services"] = services
		}
	} else if !forceIncrement && stack.GetString("checksum") == checksum {
		// Important: If it's identical, keep the OLD version
		// to maintain perfect reproducibility if requested.
		expectedFilePath := r.GetRevisionFilePath(stackID, currentVersion)
		if _, err := os.Stat(expectedFilePath); err != nil {
			// Self-heal: If the file is missing from disk (e.g. user deleted pb_data/stacks),
			// re-serialize and write the yaml without bumping the database version.
			if finalYAML, err := yaml.Marshal(configMap); err == nil {
				stackDir := filepath.Join(r.stackStorage, stackID)
				os.MkdirAll(stackDir, 0755)
				os.WriteFile(expectedFilePath, finalYAML, 0644)
			}
		}

		// For now, if the checksum matches, we can just return the existing version info.
		return &RenderResult{
			Version:      currentVersion,
			Checksum:     checksum,
			RenderedPath: fmt.Sprintf("v%d.yml", currentVersion),
		}, nil
	}

	// Re-serialize final map to YAML
	finalYAML, err := yaml.Marshal(configMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal final compose yaml: %w", err)
	}

	// Prepare storage directory
	stackDir := filepath.Join(r.stackStorage, stackID)
	if err := os.MkdirAll(stackDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create stack storage dir: %w", err)
	}

	fileName := fmt.Sprintf("v%d.yml", nextVersion)
	filePath := filepath.Join(stackDir, fileName)

	// Write to disk
	if err := os.WriteFile(filePath, finalYAML, 0644); err != nil {
		return nil, fmt.Errorf("failed to write rendered compose file: %w", err)
	}

	// Create Stack Revision Record
	err = r.createRevisionRecord(stackID, nextVersion, commitSHA, checksum, filePath)
	if err != nil {
		// Clean up file if db fails
		_ = os.Remove(filePath)
		return nil, fmt.Errorf("failed to create revision record: %w", err)
	}

	// Update Stack Record
	stack.Set("current_version", nextVersion)
	stack.Set("desired_commit", commitSHA)
	stack.Set("checksum", checksum)
	if err := r.app.Save(stack); err != nil {
		return nil, fmt.Errorf("failed to update stack record: %w", err)
	}

	return &RenderResult{
		Version:      nextVersion,
		Checksum:     checksum,
		RenderedPath: fileName,
	}, nil
}

func (r *Renderer) createRevisionRecord(stackID string, version int, commitSHA, checksum, composePath string) error {
	collection, err := r.app.FindCollectionByNameOrId("stack_revisions")
	if err != nil {
		return err
	}

	record := core.NewRecord(collection)
	record.Set("stack", stackID)
	record.Set("version", version)
	record.Set("commit_sha", commitSHA)
	record.Set("checksum", checksum)
	record.Set("compose_path", composePath)

	return r.app.Save(record)
}

func (r *Renderer) GetRevisionFilePath(stackID string, version int) string {
	return filepath.Join(r.stackStorage, stackID, fmt.Sprintf("v%d.yml", version))
}

// computeSHA256 returns hex string of SHA256 of the data
func computeSHA256(data []byte) string {
	h := sha256.New()
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

// normalizeYAML ensures keys are sorted deterministically and outputs a consistent string
func normalizeYAML(data map[string]interface{}) ([]byte, error) {
	// yaml.v3 marshals maps in a stable sorted order by default.
	// We can trust yaml.Marshal to be deterministic for map[string]interface{}
	// because go-yaml source code specifically sorts map keys before encoding.
	return yaml.Marshal(data)
}
