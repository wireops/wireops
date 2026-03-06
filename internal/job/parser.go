// Package job provides parsing for job.yaml files committed to repositories.
// The job.yaml is the single source of truth for all job configuration.
package job

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Mode controls how many agents receive a job dispatch per cron tick.
type Mode string

const (
	// ModeOnce dispatches to exactly one matching agent (round-robin).
	ModeOnce Mode = "once"
	// ModeOnceAll dispatches concurrently to every matching agent.
	ModeOnceAll Mode = "once_all"
)

// Definition holds all fields parsed from a job.yaml file.
type Definition struct {
	Title       string   `yaml:"title"       json:"title"`
	Description string   `yaml:"description" json:"description"`
	Cron        string   `yaml:"cron"        json:"cron"`
	Tags        []string `yaml:"tags"        json:"tags"`
	Mode        Mode     `yaml:"mode"        json:"mode"`
	Image       string   `yaml:"image"       json:"image"`
	Command     Command  `yaml:"command"     json:"command"`
	Remove      bool     `yaml:"remove"      json:"remove"`
	Volumes     []string `yaml:"volumes"     json:"volumes"`
	Network     string   `yaml:"network"     json:"network"`
}

// Command accepts both a single string and a string array in YAML.
type Command []string

func (c *Command) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		*c = strings.Fields(value.Value)
		return nil
	}
	var list []string
	if err := value.Decode(&list); err != nil {
		return err
	}
	*c = list
	return nil
}

// ParseJobFile reads and validates a job.yaml from the cloned repository workspace.
// repoWorkspace is the base directory where repos are cloned (e.g. pb_data/repositories).
func ParseJobFile(repoWorkspace, repoID, filePath string) (*Definition, error) {
	// Prevent path traversal
	clean := filepath.Clean(filePath)
	if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return nil, fmt.Errorf("invalid job_file path: %q", filePath)
	}

	full := filepath.Join(repoWorkspace, repoID, clean)
	data, err := os.ReadFile(full)
	if err != nil {
		return nil, fmt.Errorf("cannot read job file %q: %w", filePath, err)
	}

	var def Definition
	if err := yaml.Unmarshal(data, &def); err != nil {
		return nil, fmt.Errorf("invalid job.yaml: %w", err)
	}

	if err := def.validate(); err != nil {
		return nil, err
	}

	// Default mode
	if def.Mode == "" {
		def.Mode = ModeOnce
	}

	return &def, nil
}

func (d *Definition) validate() error {
	if d.Title == "" {
		return fmt.Errorf("job.yaml: title is required")
	}
	if d.Description == "" {
		return fmt.Errorf("job.yaml: description is required")
	}
	if d.Image == "" {
		return fmt.Errorf("job.yaml: image is required")
	}
	if d.Cron == "" {
		return fmt.Errorf("job.yaml: cron is required")
	}
	if d.Mode != "" && d.Mode != ModeOnce && d.Mode != ModeOnceAll {
		return fmt.Errorf("job.yaml: mode must be 'once' or 'once_all', got %q", d.Mode)
	}
	return nil
}
