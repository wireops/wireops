// Package wireops parses the declarative wireops.yaml/wireops.yml stack
// config file (P1.3). Unlike internal/job's job.yaml, matching a candidate
// file is done by exact basename ("wireops.yaml" or "wireops.yml"), not by
// sniffing YAML content — the filename itself is the contract.
package wireops

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
}

const supportedVersion = "wireops.v1"

// ParseWireopsFile reads and validates a wireops.yaml from the cloned
// repository workspace. repoWorkspace is the base directory where repos are
// cloned (e.g. pb_data/repositories).
func ParseWireopsFile(repoWorkspace, repoID, filePath string) (*Definition, error) {
	clean, err := safepath.CleanRelativePath(filePath)
	if err != nil {
		return nil, fmt.Errorf("invalid wireops_file path: %w", err)
	}

	full := filepath.Join(repoWorkspace, repoID, clean)
	data, err := os.ReadFile(full)
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

	if err := def.validate(); err != nil {
		return nil, err
	}

	if def.Timeout != "" {
		d, _ := time.ParseDuration(def.Timeout) // already validated in validate()
		def.DeployTimeoutSeconds = int(d.Seconds())
	}

	if def.Sync != nil && def.Sync.Interval != "" {
		d, _ := time.ParseDuration(def.Sync.Interval) // already validated in validate()
		def.SyncIntervalSeconds = int(d.Seconds())
	}

	return &def, nil
}

// IsWireopsFile reports whether filename is exactly "wireops.yaml" or
// "wireops.yml" (case-sensitive, no leading dot).
func IsWireopsFile(filename string) bool {
	base := filepath.Base(filename)
	return base == "wireops.yaml" || base == "wireops.yml"
}

func (d *Definition) validate() error {
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
		if _, err := time.ParseDuration(d.Timeout); err != nil {
			errs = append(errs, fmt.Sprintf("timeout is invalid: %v", err))
		}
	}

	if d.Sync != nil && d.Sync.Interval != "" {
		if dur, err := time.ParseDuration(d.Sync.Interval); err != nil {
			errs = append(errs, fmt.Sprintf("sync.interval is invalid: %v", err))
		} else if dur <= 0 {
			errs = append(errs, "sync.interval must be positive")
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
