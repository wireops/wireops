package job

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsJobFile(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name: "ValidJobFile",
			input: `
name: Cleanup Task
description: Removes old files
image: alpine
cron: "0 2 * * *"
command: rm -rf /tmp/old
`,
			want: true,
		},
		{
			name: "MissingCron",
			input: `
name: No Cron Job
description: Missing cron
image: alpine
command: echo hello
`,
			want: false,
		},
		{
			name: "MissingImage",
			input: `
name: No Image Job
description: Missing image
cron: "0 * * * *"
command: echo hello
`,
			want: false,
		},
		{
			name: "MissingName",
			input: `
description: Missing name
image: alpine
cron: "0 * * * *"
command: echo hello
`,
			want: false,
		},
		{
			name: "ComposeYAMLInput",
			input: `
services:
  web:
    image: nginx
  db:
    image: postgres
`,
			want: false,
		},
		{
			name:  "InvalidYAML",
			input: `{not: valid: yaml:`,
			want:  false,
		},
		{
			name:  "EmptyInput",
			input: ``,
			want:  false,
		},
		{
			name: "MultipleYAMLDocuments",
			input: `
name: First Job
description: Removes old files
image: alpine
cron: "0 2 * * *"
command: rm -rf /tmp/old
---
name: Second Job
description: Removes old files
image: alpine
cron: "0 2 * * *"
command: rm -rf /tmp/old
`,
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsJobFile([]byte(tc.input))
			if got != tc.want {
				t.Errorf("IsJobFile(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

func TestDefinitionValidate(t *testing.T) {
	cases := []struct {
		name    string
		def     Definition
		wantErr string
	}{
		{
			name: "Valid definition",
			def: Definition{
				Name:        "Test Job",
				Description: "A test job",
				Image:       "alpine:latest",
				Cron:        "*/5 * * * *",
				Resources: Resources{
					CPU:     "0.5",
					Memory:  "512m",
					Timeout: "10m",
				},
			},
			wantErr: "",
		},
		{
			name: "Missing name",
			def: Definition{
				Description: "A test job",
				Image:       "alpine:latest",
				Cron:        "*/5 * * * *",
				Resources: Resources{
					CPU:     "0.5",
					Memory:  "512m",
					Timeout: "10m",
				},
			},
			wantErr: "name is required",
		},
		{
			name: "Missing description",
			def: Definition{
				Name: "Test Job",
				Image: "alpine:latest",
				Cron:  "*/5 * * * *",
				Resources: Resources{
					CPU:     "0.5",
					Memory:  "512m",
					Timeout: "10m",
				},
			},
			wantErr: "description is required",
		},
		{
			name: "Missing image",
			def: Definition{
				Name:        "Test Job",
				Description: "A test job",
				Cron:        "*/5 * * * *",
				Resources: Resources{
					CPU:     "0.5",
					Memory:  "512m",
					Timeout: "10m",
				},
			},
			wantErr: "image is required",
		},
		{
			name: "Missing cron",
			def: Definition{
				Name:        "Test Job",
				Description: "A test job",
				Image:       "alpine:latest",
				Resources: Resources{
					CPU:     "0.5",
					Memory:  "512m",
					Timeout: "10m",
				},
			},
			wantErr: "cron is required",
		},
		{
			name: "Missing CPU",
			def: Definition{
				Name:        "Test Job",
				Description: "A test job",
				Image:       "alpine:latest",
				Cron:        "*/5 * * * *",
				Resources: Resources{
					Memory:  "512m",
					Timeout: "10m",
				},
			},
			wantErr: "resources.cpu is required",
		},
		{
			name: "Missing Memory",
			def: Definition{
				Name:        "Test Job",
				Description: "A test job",
				Image:       "alpine:latest",
				Cron:        "*/5 * * * *",
				Resources: Resources{
					CPU:     "0.5",
					Timeout: "10m",
				},
			},
			wantErr: "resources.memory is required",
		},
		{
			name: "Missing Timeout",
			def: Definition{
				Name:        "Test Job",
				Description: "A test job",
				Image:       "alpine:latest",
				Cron:        "*/5 * * * *",
				Resources: Resources{
					CPU:    "0.5",
					Memory: "512m",
				},
			},
			wantErr: "resources.timeout is required",
		},
		{
			name: "Invalid Timeout Format",
			def: Definition{
				Name:        "Test Job",
				Description: "A test job",
				Image:       "alpine:latest",
				Cron:        "*/5 * * * *",
				Resources: Resources{
					CPU:     "0.5",
					Memory:  "512m",
					Timeout: "invalid-duration",
				},
			},
			wantErr: "resources.timeout is invalid",
		},
		{
			name: "Multiple missing fields",
			def: Definition{
				Name: "",
				Cron:  "",
				Resources: Resources{
					CPU:    "",
					Memory: "",
				},
			},
			wantErr: "name is required, description is required, image is required, cron is required, resources.cpu is required, resources.memory is required, resources.timeout is required",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.def.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Errorf("expected no error, got: %v", err)
				}
			} else {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				var ve *ValidationError
				if !errors.As(err, &ve) {
					t.Fatalf("expected error to be *ValidationError, got %T: %v", err, err)
				}
				combined := strings.Join(ve.Errors, ", ")
				if !containsString(combined, tc.wantErr) {
					t.Errorf("expected validation errors to contain %q, got: %v", tc.wantErr, combined)
				}
			}
		})
	}
}

func TestParseJobFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a valid job file
	validContent := `
name: Cleanup Task
description: Removes old files
image: alpine
cron: "0 2 * * *"
resources:
  cpu: "0.5"
  memory: 512m
  timeout: 10m
command: rm -rf /tmp/old
`
	err := os.WriteFile(filepath.Join(tmpDir, "valid.yaml"), []byte(validContent), 0644)
	if err != nil {
		t.Fatalf("failed to create valid test file: %v", err)
	}

	// Create a job file with multiple documents
	multipleContent := `
name: First Task
description: Removes old files
image: alpine
cron: "0 2 * * *"
resources:
  cpu: "0.5"
  memory: 512m
  timeout: 10m
---
name: Second Task
description: Removes old files
image: alpine
cron: "0 2 * * *"
resources:
  cpu: "0.5"
  memory: 512m
  timeout: 10m
`
	err = os.WriteFile(filepath.Join(tmpDir, "multiple.yaml"), []byte(multipleContent), 0644)
	if err != nil {
		t.Fatalf("failed to create multiple test file: %v", err)
	}

	// Test valid
	def, err := ParseJobFile(tmpDir, "", "valid.yaml")
	if err != nil {
		t.Errorf("expected no error for valid job file, got: %v", err)
	}
	if def == nil || def.Name != "Cleanup Task" {
		t.Errorf("expected parsed definition name to be 'Cleanup Task', got: %v", def)
	}

	// Test multiple documents error
	_, err = ParseJobFile(tmpDir, "", "multiple.yaml")
	if err == nil {
		t.Errorf("expected error for multiple YAML documents, got nil")
	} else if !strings.Contains(err.Error(), "multiple YAML documents (separated by '---') are not allowed") {
		t.Errorf("expected error message to contain multiple documents warning, got: %v", err)
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || s[0:len(substr)] == substr || s[len(s)-len(substr):] == substr || stringsContains(s, substr))
}

func stringsContains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
