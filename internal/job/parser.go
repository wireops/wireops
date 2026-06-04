package job

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

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

type Resources struct {
	CPU     string `yaml:"cpu"     json:"cpu"`
	Memory  string `yaml:"memory"  json:"memory"`
	Timeout string `yaml:"timeout" json:"timeout"`
}

// Definition holds all fields parsed from a job.yaml file.
type Definition struct {
	Name        string    `yaml:"name"        json:"name"`
	Description string    `yaml:"description" json:"description"`
	Cron        string    `yaml:"cron"        json:"cron"`
	Tags        []string  `yaml:"tags"        json:"tags"`
	Mode        Mode      `yaml:"mode"        json:"mode"`
	Image       string    `yaml:"image"       json:"image"`
	Command     Command   `yaml:"command"     json:"command"`
	Volumes     []string  `yaml:"volumes"     json:"volumes"`
	Network     string    `yaml:"network"     json:"network"`
	Resources   Resources `yaml:"resources"   json:"resources"`
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

	dec := yaml.NewDecoder(bytes.NewReader(data))
	var def Definition
	if err := dec.Decode(&def); err != nil {
		return nil, fmt.Errorf("invalid job.yaml: %w", err)
	}

	// Reject multiple documents (separated by ---)
	var next yaml.Node
	if err := dec.Decode(&next); err == nil {
		return nil, fmt.Errorf("invalid job.yaml: multiple YAML documents (separated by '---') are not allowed")
	} else if err != io.EOF {
		return nil, fmt.Errorf("invalid job.yaml: multiple YAML documents or invalid trailing content found: %w", err)
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

// IsJobFile reports whether data looks like a job.yaml by requiring
// non-empty "title", "image", and "cron" fields at the top level.
func IsJobFile(data []byte) bool {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc struct {
		Name  string `yaml:"name"`
		Image string `yaml:"image"`
		Cron  string `yaml:"cron"`
	}
	if err := dec.Decode(&doc); err != nil {
		return false
	}
	if doc.Name == "" || doc.Image == "" || doc.Cron == "" {
		return false
	}

	// Reject multiple documents
	var next yaml.Node
	if err := dec.Decode(&next); err == nil {
		return false
	} else if err != io.EOF {
		return false
	}

	return true
}

func (d *Definition) validate() error {
	var errs []string

	if d.Name == "" {
		errs = append(errs, "name is required")
	}
	if d.Description == "" {
		errs = append(errs, "description is required")
	}
	if d.Image == "" {
		errs = append(errs, "image is required")
	}
	if d.Cron == "" {
		errs = append(errs, "cron is required")
	}
	if d.Mode != "" && d.Mode != ModeOnce && d.Mode != ModeOnceAll {
		errs = append(errs, fmt.Sprintf("mode must be 'once' or 'once_all', got %q", d.Mode))
	}

	// Validate mandatory resources block
	if d.Resources.CPU == "" {
		errs = append(errs, "resources.cpu is required")
	}
	if d.Resources.Memory == "" {
		errs = append(errs, "resources.memory is required")
	}
	if d.Resources.Timeout == "" {
		errs = append(errs, "resources.timeout is required")
	} else {
		// Validate timeout duration format
		if _, err := time.ParseDuration(d.Resources.Timeout); err != nil {
			errs = append(errs, fmt.Sprintf("resources.timeout is invalid: %v", err))
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
	return fmt.Sprintf("job.yaml: %s", strings.Join(v.Errors, ", "))
}
