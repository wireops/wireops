// Package manifest parses the declarative wireops.yaml/wireops.yml stack
// config file (P1.3). Unlike internal/job's job.yaml, matching a candidate
// file is done by exact basename ("wireops.yaml" or "wireops.yml"), not by
// sniffing YAML content — the filename itself is the contract.
//
// DEPRECATED: the standalone wireops.yaml/wireops.yml two-file layout is
// deprecated. Prefer embedding the same fields inline in the compose file
// under a top-level "x-wireops" key (see ExtensionKey / ParseComposeManifest).
// The standalone layout still parses and is fully supported, but new stacks
// should use x-wireops.
package manifest

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/wireops/wireops/internal/safepath"
)

// ComposeConfig holds docker-compose runtime flags.
type ComposeConfig struct {
	RemoveOrphans *bool `yaml:"remove_orphans" json:"remove_orphans"`
	ForcePull     *bool `yaml:"force_pull"     json:"force_pull"`
}

// JobsConfig controls whether deploy should wait for related jobs to finish.
type JobsConfig struct {
	WaitRunning *bool `yaml:"wait_running" json:"wait_running"`
}

// WorkerConfig declares which worker(s) this stack should target.
type WorkerConfig struct {
	Tags []string `yaml:"tags" json:"tags"`
}

// SyncConfig controls how often the stack's repository is polled for changes.
type SyncConfig struct {
	Interval string `yaml:"interval" json:"interval"`
}

// Definition holds all fields parsed from a wireops.yaml file. One file
// describes exactly one stack (no deployment list).
type Definition struct {
	Version              string         `yaml:"version" json:"version"`
	Name                 string         `yaml:"name"    json:"name"`
	Group                string         `yaml:"group"   json:"group,omitempty"`
	Timeout              string         `yaml:"timeout" json:"-"`
	DeployTimeoutSeconds int            `yaml:"-"       json:"deploy_timeout_seconds"`
	Compose              *ComposeConfig `yaml:"compose" json:"compose,omitempty"`
	Jobs                 *JobsConfig    `yaml:"jobs"    json:"jobs,omitempty"`
	Worker               *WorkerConfig  `yaml:"worker"  json:"worker,omitempty"`
	Sync                 *SyncConfig    `yaml:"sync"    json:"sync,omitempty"`

	// SyncIntervalSeconds is resolved from Sync.Interval. Zero means the
	// stack falls back to the global SCAN_PERIOD.
	SyncIntervalSeconds int `yaml:"-" json:"sync_interval_seconds,omitempty"`

	// Populated by the caller (routes layer) after locating the compose
	// file alongside this wireops.yaml. Never set by the parser itself.
	ResolvedComposePath string `yaml:"-" json:"resolved_compose_path,omitempty"`
	ResolvedComposeFile string `yaml:"-" json:"resolved_compose_file,omitempty"`
	ResolutionError     string `yaml:"-" json:"resolution_error,omitempty"`

	// Deprecated is set to true only when this Definition came from a
	// standalone wireops.yaml/wireops.yml file (ParseWireopsFile), the
	// deprecated two-file layout. It is never set for an embedded x-wireops
	// block (ParseComposeManifest), so the primary single-file path stays
	// clean. DeprecationNotice carries a short human-readable steer.
	Deprecated        bool   `yaml:"-" json:"deprecated,omitempty"`
	DeprecationNotice string `yaml:"-" json:"deprecation_notice,omitempty"`
}

// deprecationNotice is the steer shown for the standalone wireops.yaml layout.
const deprecationNotice = "The standalone wireops.yaml layout is deprecated. " +
	"Prefer embedding an 'x-wireops' block in the compose file (single-file stack). " +
	"Existing stacks keep working."

const supportedVersion = "wireops.v1"

// ParseWireopsFile reads and validates a wireops.yaml from the cloned
// repository workspace. repoWorkspace is the base directory where repos are
// cloned (e.g. pb_data/repositories).
func ParseWireopsFile(repoWorkspace, repoID, filePath string) (*Definition, error) {
	clean, err := safepath.CleanRelativePath(filePath)
	if err != nil {
		return nil, fmt.Errorf("invalid wireops_file path: %w", err)
	}
	// Enforce the filename contract documented on this package: the manifest
	// is matched by exact basename, in whichever subdirectory it lives. The
	// listing endpoint already filters candidates with IsWireopsFile, so this
	// only rejects hand-built API calls.
	if !IsWireopsFile(filepath.Base(clean)) {
		return nil, fmt.Errorf("invalid wireops_file path: must be wireops.yaml or wireops.yml, got %q", filePath)
	}

	base := filepath.Join(repoWorkspace, repoID)
	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve repository base path: %w", err)
	}
	fullAbs, err := filepath.Abs(filepath.Join(baseAbs, clean))
	if err != nil {
		return nil, fmt.Errorf("cannot resolve wireops file path %q: %w", filePath, err)
	}
	rel, err := filepath.Rel(baseAbs, fullAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("invalid wireops_file path: escapes repository directory: %q", filePath)
	}

	data, err := os.ReadFile(fullAbs)
	if err != nil {
		return nil, fmt.Errorf("cannot read wireops file %q: %w", filePath, err)
	}

	dec := yaml.NewDecoder(bytes.NewReader(data))
	var def Definition
	if err := dec.Decode(&def); err != nil {
		return nil, fmt.Errorf("invalid wireops.yaml: %w", err)
	}

	// Reject multiple documents (separated by ---)
	var next yaml.Node
	if err := dec.Decode(&next); err == nil {
		return nil, fmt.Errorf("invalid wireops.yaml: multiple YAML documents (separated by '---') are not allowed")
	} else if err != io.EOF {
		return nil, fmt.Errorf("invalid wireops.yaml: multiple YAML documents or invalid trailing content found: %w", err)
	}

	if err := finalizeDefinition(&def); err != nil {
		return nil, err
	}

	// Flag the deprecated standalone layout. Set here (not in the shared
	// finalizeDefinition) so an embedded x-wireops block never inherits it.
	def.Deprecated = true
	def.DeprecationNotice = deprecationNotice

	return &def, nil
}

// ExtensionKey is the top-level docker-compose extension field
// (https://docs.docker.com/reference/compose-file/extension/) that carries
// the wireops.yaml schema inline in the compose file itself, for stacks that
// want a single file instead of a compose file plus a separate wireops.yaml.
// Compose tooling ignores unknown "x-*" top-level keys, so this key is
// invisible to `docker compose up` — but `docker compose config` passes it
// through verbatim into the resolved output, so callers rendering that
// output for deployment (internal/sync/renderer.go) must strip it
// themselves; exported so they share this exact key rather than duplicating
// the string.
const ExtensionKey = "x-wireops"

// ParseComposeManifest extracts and validates a stack Definition embedded in
// a compose file's top-level "x-wireops" extension block. raw is the raw
// compose YAML (as committed to git, not the rendered/resolved config). It
// returns (nil, nil) if the compose file has no x-wireops block at all, so
// callers can distinguish "not using this feature" from a malformed block.
func ParseComposeManifest(raw []byte) (*Definition, error) {
	var root map[string]yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("invalid compose file: %w", err)
	}

	node, ok := root[ExtensionKey]
	if !ok {
		return nil, nil
	}

	var def Definition
	if err := node.Decode(&def); err != nil {
		return nil, fmt.Errorf("invalid %s block: %w", ExtensionKey, err)
	}

	if err := finalizeDefinition(&def); err != nil {
		return nil, err
	}

	return &def, nil
}

// HasEmbeddedXWireops reports whether raw (a candidate compose file's
// content) has a top-level "x-wireops" key at all, without validating its
// contents. Used to sniff candidate files during repository discovery
// (parallel to job.IsJobFile) — full validation happens later, when the
// caller actually resolves one specific file via ParseComposeManifest.
func HasEmbeddedXWireops(raw []byte) bool {
	var root map[string]yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return false
	}
	_, ok := root[ExtensionKey]
	return ok
}

// finalizeDefinition runs the validation and duration-resolution steps
// shared by every Definition source (standalone wireops.yaml or an embedded
// x-wireops compose block).
func finalizeDefinition(def *Definition) error {
	if err := def.Validate(); err != nil {
		return err
	}

	if def.Timeout != "" {
		d, _ := time.ParseDuration(def.Timeout) // already validated in Validate()
		def.DeployTimeoutSeconds = int(d.Seconds())
	}

	if def.Sync != nil && def.Sync.Interval != "" {
		d, _ := time.ParseDuration(def.Sync.Interval) // already validated in Validate()
		def.SyncIntervalSeconds = int(d.Seconds())
	}

	return nil
}

// IsWireopsFile reports whether filename is exactly "wireops.yaml" or
// "wireops.yml" (case-sensitive, no leading dot).
func IsWireopsFile(filename string) bool {
	base := filepath.Base(filename)
	return base == "wireops.yaml" || base == "wireops.yml"
}

// Validate checks the definition for required fields and well-formed
// duration strings. Exported so callers that build a Definition
// programmatically (e.g. the MCP scaffolding tools) can validate before
// marshaling, using the same rules ParseWireopsFile enforces.
func (d *Definition) Validate() error {
	var errs []string

	if d.Version == "" {
		errs = append(errs, "version is required")
	} else if d.Version != supportedVersion {
		errs = append(errs, fmt.Sprintf("unsupported version %q, expected %q", d.Version, supportedVersion))
	}

	if d.Name == "" {
		errs = append(errs, "name is required")
	}

	if d.Timeout != "" {
		if dur, err := time.ParseDuration(d.Timeout); err != nil {
			errs = append(errs, fmt.Sprintf("timeout is invalid: %v", err))
		} else if dur < time.Second {
			errs = append(errs, "timeout must be at least 1s")
		}
	}

	if d.Sync != nil && d.Sync.Interval != "" {
		if dur, err := time.ParseDuration(d.Sync.Interval); err != nil {
			errs = append(errs, fmt.Sprintf("sync.interval is invalid: %v", err))
		} else if dur < time.Second {
			errs = append(errs, "sync.interval must be at least 1s")
		}
	}

	if len(errs) > 0 {
		return &ValidationError{Errors: errs}
	}

	return nil
}

type ValidationError struct {
	Errors []string
}

func (v *ValidationError) Error() string {
	return fmt.Sprintf("wireops.yaml: %s", strings.Join(v.Errors, ", "))
}
