package sync

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/pocketbase/pocketbase/core"
	"github.com/wireops/wireops/internal/compose"
	"github.com/wireops/wireops/internal/config"
	"github.com/wireops/wireops/internal/crypto"
	"github.com/wireops/wireops/internal/policy"
)

// RenderResult represents the result of the label injection process.
type RenderResult struct {
	Version      int
	Checksum     string
	RenderedPath string // e.g., v5.yml
}

const (
	labelManaged     = "dev.wireops.managed"
	labelStackID     = "dev.wireops.stack_id"
	labelCommitSHA   = "dev.wireops.repository.commit_sha"
	labelChecksum    = "dev.wireops.checksum"
	labelGeneratedAt = "dev.wireops.generated_at"
)

// Renderer is responsible for intercepting compose files and injecting deterministic labels
type Renderer struct {
	app          core.App
	stackStorage string
}

func NewRenderer(app core.App) *Renderer {
	// The storage layout for rendered compose files
	// e.g., /data/stacks/<stack_id>/v1.yml
	storagePath := config.GetStacksStoragePath()

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
	workerID string,
	workerFingerprint string,
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

	// Validate against worker policy
	wp, err := policy.Load(r.app, workerID)
	if err != nil {
		return nil, fmt.Errorf("failed to load worker policy: %w", err)
	}
	if err := wp.ValidateComposeConfig(configMap); err != nil {
		return nil, err
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

		// Prepare blocks (supporting both map and list formats)
		labels := NormalizeToMap(svc["labels"])
		annotations := NormalizeToMap(svc["annotations"])

		// Scrub any user-supplied metadata using the reserved dev.wireops namespace
		stripWireopsMetadata(labels)
		stripWireopsMetadata(annotations)

		// Identity Labels (Required for runtime filtering)
		labels[labelManaged] = "true"
		labels[labelStackID] = stackID

		// Metadata Annotations
		annotations["dev.wireops.stack_name"] = stackName
		annotations["dev.wireops.repository"] = repoName
		annotations["dev.wireops.repository.url"] = repoURL
		annotations["dev.wireops.repository.branch"] = branch
		annotations["dev.wireops.repository.file"] = composeFile
		if workerFingerprint != "" {
			annotations["dev.wireops.worker.fingerprint"] = workerFingerprint
		}

		svc["labels"] = labels
		svc["annotations"] = annotations
		services[serviceName] = svc
	}
	configMap["services"] = services

	// Calculate checksum WITHOUT time-varying metadata (generated_at, commit_sha, version).
	// This ensures the checksum reflects only the structural compose content, so that
	// commits touching unrelated files (non-compose) do not trigger unnecessary redeploys.
	//
	// The compose config above is rendered with --no-interpolate, so `${VAR}` placeholders
	// stay literal in normalizedYAML — a value-only change to an env var (plain or
	// SOPS-decrypted) would otherwise produce an identical checksum and be silently
	// skipped as "no changes detected". Folding a hash of the resolved env vars into the
	// checksum ensures env-only changes still trigger a redeploy.
	normalizedYAML, err := normalizeYAML(configMap)
	if err != nil {
		return nil, fmt.Errorf("failed to normalize YAML for checksum: %w", err)
	}
	checksum := computeSHA256(append(normalizedYAML, []byte(hashEnvVars(envVars))...))

	// Inject commit_sha, version, checksum, and generated_at AFTER hashing.
	injectVersionMetadata(services, commitSHA, checksum, generatedAt, nextVersion)
	configMap["services"] = services

	// Compare with current stack state to see if we actually need a version bump
	// if we didn't already force it.
	if !forceIncrement && stack.GetString("checksum") != checksum {
		// Content changed, we need to bump the version
		if stack.GetString("checksum") != "" {
			nextVersion++
			// Bump version label and re-inject post-hash metadata.
			// The structural checksum (without commit_sha/version/generated_at) doesn't
			// change here — only the version counter and commit_sha are updated.
			injectVersionMetadata(services, commitSHA, checksum, generatedAt, nextVersion)
			configMap["services"] = services
		}
	} else if !forceIncrement && stack.GetString("checksum") == checksum {
		// Important: If it's identical, keep the OLD version
		// to maintain perfect reproducibility if requested.
		expectedFilePath := r.GetRevisionFilePath(stackID, currentVersion)
		if _, err := os.Stat(expectedFilePath); err != nil {
			// Self-heal: If the file is missing from disk (e.g. user deleted DATA_DIR/stacks),
			// re-serialize and write the yaml without bumping the database version.
			if finalYAML, err := yaml.Marshal(configMap); err == nil {
				stackDir := filepath.Join(r.stackStorage, stackID)
				os.MkdirAll(stackDir, 0700)
				os.WriteFile(expectedFilePath, finalYAML, 0600)
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
	if err := os.MkdirAll(stackDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create stack storage dir: %w", err)
	}

	fileName := fmt.Sprintf("v%d.yml", nextVersion)
	filePath := filepath.Join(stackDir, fileName)

	// Write to disk
	if err := os.WriteFile(filePath, finalYAML, 0600); err != nil {
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

func injectVersionMetadata(services map[string]interface{}, commitSHA, checksum, generatedAt string, version int) {
	for serviceName, svcRaw := range services {
		svc, ok := svcRaw.(map[string]interface{})
		if !ok {
			continue
		}
		labels, _ := svc["labels"].(map[string]interface{})
		annotations, _ := svc["annotations"].(map[string]interface{})

		// Version-sensitive metadata as annotations
		annotations["dev.wireops.repository.commit_sha"] = commitSHA
		annotations["dev.wireops.version"] = strconv.Itoa(version)
		annotations[labelChecksum] = checksum
		annotations[labelGeneratedAt] = generatedAt

		// Commit SHA is also kept as a label for easy container filtering/inspection
		labels[labelCommitSHA] = commitSHA

		svc["labels"] = labels
		svc["annotations"] = annotations
		services[serviceName] = svc
	}
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

// hashEnvVars returns a deterministic digest of "KEY=VALUE" entries,
// independent of input order, for folding into the compose checksum. The
// checksum is persisted (stack/revision records) and baked into the
// deployed compose file as a label, so an unkeyed hash of resolved env vars
// (which may include SOPS-decrypted secret values) would let anyone with
// read access to that checksum brute-force or dictionary-guess the
// underlying secret value. HMAC with the server-only SECRET_KEY keeps the
// digest one-way even when the plaintext value is weak/guessable.
func hashEnvVars(envVars []string) string {
	sorted := append([]string(nil), envVars...)
	sort.Strings(sorted)
	secretKey := crypto.NormalizeSecretKey(os.Getenv("SECRET_KEY"))
	h := hmac.New(sha256.New, secretKey)
	for _, kv := range sorted {
		h.Write([]byte(kv))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// normalizeYAML ensures keys are sorted deterministically and outputs a consistent string
func normalizeYAML(data map[string]interface{}) ([]byte, error) {
	// yaml.v3 marshals maps in a stable sorted order by default.
	// We can trust yaml.Marshal to be deterministic for map[string]interface{}
	// because go-yaml source code specifically sorts map keys before encoding.
	return yaml.Marshal(data)
}

// stripWireopsMetadata removes any user-supplied metadata using the reserved dev.wireops namespace.
func stripWireopsMetadata(m map[string]interface{}) {
	for k := range m {
		if k == "dev.wireops" || strings.HasPrefix(k, "dev.wireops.") {
			delete(m, k)
		}
	}
}

// NormalizeToMap converts a label/annotation block (which can be a map or a list) into a map.
func NormalizeToMap(input interface{}) map[string]interface{} {
	if m, ok := input.(map[string]interface{}); ok {
		// Return a copy to avoid mutating the original if shared (though unlikely here)
		res := make(map[string]interface{})
		for k, v := range m {
			res[k] = v
		}
		return res
	}

	res := make(map[string]interface{})
	if list, ok := input.([]interface{}); ok {
		for _, item := range list {
			if s, ok := item.(string); ok {
				parts := strings.SplitN(s, "=", 2)
				if len(parts) == 2 {
					res[parts[0]] = parts[1]
				} else {
					res[parts[0]] = ""
				}
			}
		}
	}
	return res
}
