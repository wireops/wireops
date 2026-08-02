package sync

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
	"github.com/wireops/wireops/internal/configfiles"
	"github.com/wireops/wireops/internal/crypto"
	"github.com/wireops/wireops/internal/lint"
	"github.com/wireops/wireops/internal/policy"
	"github.com/wireops/wireops/internal/safepath"
)

// RenderResult represents the result of the label injection process.
type RenderResult struct {
	Version      int
	Checksum     string
	RenderedPath string // e.g., v5.yml

	// ConfigFiles lists every git-committed file resolved from
	// dev.wireops.config.<name> service annotations and embedded into this
	// revision. Used by the caller to populate the stack_config_files
	// tracking table.
	ConfigFiles []configfiles.TrackedFile
}

// ServiceOverride holds render-time (not committed to git) overrides for a single
// compose service. Fields left empty are not overridden.
type ServiceOverride struct {
	Image    string   `json:"image,omitempty"`
	Ports    []string `json:"ports,omitempty"`
	Networks []string `json:"networks,omitempty"`
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

// containmentRootFor returns the directory the compose file must resolve
// inside once symlinks are followed.
//
// The repository checkout is the right boundary when workDir is under it: the
// stack's compose_path is user-supplied, so the directory has to be contained
// as well as the file. But workDir does not always live there — a local-source
// stack renders from a temporary directory, and tests render from a fixture
// dir — so fall back to workDir itself rather than rejecting those outright.
// Either way the compose file cannot be a symlink pointing out of the tree it
// was found in, which is the vector that matters.
func containmentRootFor(repo *core.Record, workDir string) string {
	if repo == nil {
		return workDir
	}
	repoDir := filepath.Join(config.GetReposWorkspace(), repo.Id)
	cleanWork := filepath.Clean(workDir)
	if cleanWork == repoDir || strings.HasPrefix(cleanWork, repoDir+string(filepath.Separator)) {
		return repoDir
	}
	return workDir
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
	overrides map[string]ServiceOverride,
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

	root := containmentRootFor(repo, workDir)

	// 1. Get current compose config as JSON.
	configOut, err := compose.Config(ctx, compose.ConfigOptions{
		WorkDir:     workDir,
		ComposeFile: composeFile,
		EnvVars:     envVars,
		Root:        root,
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

	// Validation: Ensure services exist
	servicesRaw, ok := configMap["services"]
	if !ok || servicesRaw == nil {
		return nil, fmt.Errorf("no services defined in compose file")
	}
	services, ok := servicesRaw.(map[string]interface{})
	if !ok || len(services) == 0 {
		return nil, fmt.Errorf("services block is invalid or empty")
	}

	// Synthesize the compose config's native `configs:` element (top-level
	// content block + each service's own `configs:` list entry) entirely
	// from dev.wireops.config.<name> annotations on the service — no
	// wireops.yaml involvement at all. The annotation value is
	// "<repo-relative source>:<in-container target>"; source may be a file
	// or a directory (recursively expanded, one config entry per file).
	trackedConfigs, err := injectConfigContent(configMap, services, workDir)
	if err != nil {
		return nil, err
	}

	if len(overrides) > 0 {
		// AllowRenderOverrides gates this capability on its own, independent of
		// wp.Disabled — disabling the rest of the policy's allowlist/Block* checks
		// must not implicitly grant override capability too.
		if !wp.AllowRenderOverrides {
			return nil, fmt.Errorf("render-time overrides are disabled by the worker policy")
		}
		configMap["services"] = services
		if err := ApplyServiceOverrides(configMap, overrides); err != nil {
			return nil, err
		}
	} else {
		configMap["services"] = services
	}

	// Validated after overrides are applied, so overridden images/networks are also
	// checked against the worker's allowlists and boolean-flag restrictions.
	if err := wp.ValidateComposeConfig(configMap); err != nil {
		return nil, err
	}

	// Error-severity lint findings (structural problems the checks above don't
	// catch, plus the same policy breaches ValidateComposeConfig already
	// rejects) block the deploy too — a lint report that says "error" and a
	// deploy that proceeds anyway would make the Review step's checks
	// meaningless. Warnings and infos stay advisory.
	lintReport := lint.Run(configMap, lint.Context{
		Policy:   wp,
		EnvKeys:  lint.EnvKeysFromPairs(envVars),
		RepoRoot: root,
	})
	if lintReport.HasErrors() {
		var msgs []string
		for _, f := range lintReport.Findings {
			if f.Severity == lint.SeverityError {
				msgs = append(msgs, f.Message)
			}
		}
		return nil, fmt.Errorf("compose file failed static checks: %s", strings.Join(msgs, "; "))
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
			ConfigFiles:  trackedConfigs,
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
		ConfigFiles:  trackedConfigs,
	}, nil
}

// ErrUnknownOverrideService is wrapped into the error returned by ApplyServiceOverrides
// when a persisted override targets a service that no longer exists in the compose
// config — e.g. the service was renamed or removed in a later Git commit. Callers on
// unattended reconcile paths use errors.Is against this to decide whether to clear the
// stale override and self-heal instead of failing the sync indefinitely.
var ErrUnknownOverrideService = errors.New("render override targets unknown service")

// ApplyServiceOverrides mutates services in place, replacing image/ports/networks for
// each named service per the given overrides. It returns an error if an override
// targets a service that doesn't exist in the compose config. Exported so the
// render-overrides API route can run the exact same mutation against a resolved
// compose config before persisting, and reject policy-violating overrides (blocked
// images/networks/:latest) up front instead of only failing at the next reconcile.
//
// Any override network name not already declared in configMap's top-level "networks"
// block is added there as external:true — an override can only reasonably point at a
// network that already exists on the host (e.g. a shared proxy network), and compose
// rejects a service referencing an undeclared network, so leaving it undeclared would
// turn a valid-looking override into a deploy-time failure.
func ApplyServiceOverrides(configMap map[string]interface{}, overrides map[string]ServiceOverride) error {
	services, _ := configMap["services"].(map[string]interface{})
	declaredNetworks, _ := configMap["networks"].(map[string]interface{})
	networksAdded := false

	for serviceName, override := range overrides {
		svcRaw, ok := services[serviceName]
		if !ok {
			return fmt.Errorf("%w: %q", ErrUnknownOverrideService, serviceName)
		}
		svc, ok := svcRaw.(map[string]interface{})
		if !ok {
			return fmt.Errorf("render override targets invalid service %q", serviceName)
		}

		if override.Image != "" {
			svc["image"] = override.Image
		}
		if override.Ports != nil {
			ports := make([]interface{}, len(override.Ports))
			for i, p := range override.Ports {
				ports[i] = p
			}
			svc["ports"] = ports
		}
		if override.Networks != nil {
			networks := make([]interface{}, len(override.Networks))
			for i, n := range override.Networks {
				networks[i] = n
				if _, exists := declaredNetworks[n]; !exists {
					if declaredNetworks == nil {
						declaredNetworks = map[string]interface{}{}
					}
					declaredNetworks[n] = map[string]interface{}{"external": true}
					networksAdded = true
				}
			}
			svc["networks"] = networks
		}

		services[serviceName] = svc
	}

	configMap["services"] = services
	if networksAdded {
		configMap["networks"] = declaredNetworks
	}
	return nil
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

// configAnnotationPrefix marks a service annotation as a config mount
// directive: `dev.wireops.config.<name>: <repo-relative source>:<in-container
// target>`. <name> is just an arbitrary label distinguishing multiple
// annotations on the same service — all resolution info (source, target)
// lives in the value, so there is no wireops.yaml involvement at all.
// Mirrors the existing `customization.image.slug` convention
// (internal/depgraph/graph.go) of expressing per-service intent as a
// compose annotation read back server-side.
const configAnnotationPrefix = "dev.wireops.config."

// injectConfigContent synthesizes the compose config's native `configs:`
// element — the top-level content block plus each service's own `configs:`
// list entry — entirely from dev.wireops.config.<name> annotations on the
// service. The user's raw compose file never needs to declare `configs:`
// itself. Source may name a file or a directory (recursively expanded into
// one synthesized config entry per file, mounted under the declared target).
//
// It returns the resolved files for tracking, and errors on a malformed
// annotation value or an unresolvable/oversized source.
//
// Because content is embedded directly into configMap before the checksum
// is computed (unlike env vars, which stay as literal `${VAR}` placeholders
// under --no-interpolate), a config content-only change is already captured
// by the structural checksum — no separate HMAC fold is needed here.
func injectConfigContent(configMap map[string]interface{}, services map[string]interface{}, workDir string) ([]configfiles.TrackedFile, error) {
	directives, err := collectConfigDirectives(services)
	if err != nil {
		return nil, err
	}
	if len(directives) == 0 {
		return nil, nil
	}

	sources := make([]configfiles.Source, len(directives))
	for i, d := range directives {
		sources[i] = configfiles.Source{Name: d.key, Path: d.Source}
	}
	resolved, err := configfiles.Resolve(workDir, sources)
	if err != nil {
		return nil, err
	}
	byKey := make(map[string][]configfiles.ResolvedFile, len(directives))
	for _, rf := range resolved {
		byKey[rf.Name] = append(byKey[rf.Name], rf)
	}

	topConfigs, _ := configMap["configs"].(map[string]interface{})
	if topConfigs == nil {
		topConfigs = map[string]interface{}{}
	}

	var tracked []configfiles.TrackedFile
	for _, d := range directives {
		svc := services[d.ServiceName].(map[string]interface{})
		existing, _ := svc["configs"].([]interface{})

		for _, rf := range byKey[d.key] {
			target := d.Target
			trackName := d.Name
			if rf.RelPath != "" {
				target = strings.TrimRight(d.Target, "/") + "/" + rf.RelPath
				trackName = d.Name + "/" + rf.RelPath
			}
			configKey := sanitizeConfigKey(d.key, rf.RelPath)

			topConfigs[configKey] = map[string]interface{}{"content": string(rf.Content)}
			existing = append(existing, map[string]interface{}{"source": configKey, "target": target})
			tracked = append(tracked, configfiles.TrackedFile{
				Name:       trackName,
				SourcePath: rf.SourcePath,
				Target:     target,
				Checksum:   rf.Checksum,
			})
		}

		svc["configs"] = existing
		services[d.ServiceName] = svc
	}

	configMap["configs"] = topConfigs
	return tracked, nil
}

// configDirective is a single dev.wireops.config.<name> directive parsed off
// a service's annotations, plus a per-(service,name) key unique enough to
// key the configfiles.Resolve() call and the synthesized compose config
// name (two services independently annotating an unrelated source/target
// pair under the same friendly Name must not collide).
type configDirective struct {
	ServiceName string
	Name        string
	Source      string
	Target      string
	key         string
}

// collectConfigDirectives scans every service's annotations (and
// deploy.annotations) for dev.wireops.config.<name> directives, validating
// each value's "<source>:<target>" shape.
func collectConfigDirectives(services map[string]interface{}) ([]configDirective, error) {
	var directives []configDirective
	for serviceName, svcRaw := range services {
		svc, ok := svcRaw.(map[string]interface{})
		if !ok {
			continue
		}

		annotations := NormalizeToMap(svc["annotations"])
		merged := map[string]interface{}{}
		for k, v := range annotations {
			merged[k] = v
		}
		if deploy, ok := svc["deploy"].(map[string]interface{}); ok {
			for k, v := range NormalizeToMap(deploy["annotations"]) {
				merged[k] = v
			}
		}

		for k, v := range merged {
			if !strings.HasPrefix(k, configAnnotationPrefix) {
				continue
			}
			name := strings.TrimPrefix(k, configAnnotationPrefix)
			if err := safepath.ValidateConfigName(name); err != nil {
				return nil, fmt.Errorf("service %q: invalid config annotation name %q: %w", serviceName, name, err)
			}
			value, _ := v.(string)
			source, target, err := splitConfigMapping(value)
			if err != nil {
				return nil, fmt.Errorf("service %q: %s%s: %w", serviceName, configAnnotationPrefix, name, err)
			}
			directives = append(directives, configDirective{
				ServiceName: serviceName,
				Name:        name,
				Source:      source,
				Target:      target,
				key:         serviceName + "__" + name,
			})
		}
	}
	return directives, nil
}

// splitConfigMapping parses a "<source>:<target>" annotation value: source
// is a repo-relative file or directory, target is an absolute in-container
// path. Split on the first ':' — source paths must not contain one.
func splitConfigMapping(value string) (source, target string, err error) {
	idx := strings.Index(value, ":")
	if idx <= 0 || idx == len(value)-1 {
		return "", "", fmt.Errorf(`expected "<source>:<target>", got %q`, value)
	}
	source, target = value[:idx], value[idx+1:]
	if _, err := safepath.CleanRelativePath(source); err != nil {
		return "", "", fmt.Errorf("invalid source %q: %w", source, err)
	}
	if err := safepath.ValidateContainerMountPath(target); err != nil {
		return "", "", fmt.Errorf("invalid target %q: %w", target, err)
	}
	return source, target, nil
}

// sanitizeConfigKey turns a (service__name, relPath) pair into a compose
// config key — safe characters only, unique per resolved file.
func sanitizeConfigKey(key, relPath string) string {
	if relPath == "" {
		return key
	}
	return key + "-" + strings.NewReplacer("/", "-", " ", "_").Replace(relPath)
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
