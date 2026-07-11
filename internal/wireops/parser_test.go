package wireops

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsWireopsFile(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "ExactYaml", input: "wireops.yaml", want: true},
		{name: "ExactYml", input: "wireops.yml", want: true},
		{name: "NestedPath", input: "apps/api/wireops.yaml", want: true},
		{name: "DotPrefixed", input: ".wireops.yml", want: false},
		{name: "Uppercase", input: "WIREOPS.YAML", want: false},
		{name: "WrongName", input: "notwireops.yaml", want: false},
		{name: "ComposeFile", input: "docker-compose.yml", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsWireopsFile(tc.input)
			if got != tc.want {
				t.Errorf("IsWireopsFile(%q) = %v, want %v", tc.input, got, tc.want)
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
			name: "Valid minimal",
			def:  Definition{Version: "wireops.v1", Name: "api"},
		},
		{
			name: "Valid with timeout",
			def:  Definition{Version: "wireops.v1", Name: "api", Timeout: "5m"},
		},
		{
			name:    "Missing version",
			def:     Definition{Name: "api"},
			wantErr: "version is required",
		},
		{
			name:    "Unsupported version",
			def:     Definition{Version: "wireops.v2", Name: "api"},
			wantErr: `unsupported version "wireops.v2"`,
		},
		{
			name:    "Missing name",
			def:     Definition{Version: "wireops.v1"},
			wantErr: "name is required",
		},
		{
			name:    "Invalid timeout",
			def:     Definition{Version: "wireops.v1", Name: "api", Timeout: "not-a-duration"},
			wantErr: "timeout is invalid",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.def.validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Errorf("expected no error, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("expected error to be *ValidationError, got %T: %v", err, err)
			}
			combined := strings.Join(ve.Errors, ", ")
			if !strings.Contains(combined, tc.wantErr) {
				t.Errorf("expected validation errors to contain %q, got: %v", tc.wantErr, combined)
			}
		})
	}
}

func TestParseWireopsFile(t *testing.T) {
	tmpDir := t.TempDir()

	validContent := `
version: wireops.v1
name: api
timeout: 5m
compose:
  remove_orphans: true
  force_pull: false
jobs:
  wait_running: true
worker:
  tags:
    - prod
    - amd64
`
	if err := os.WriteFile(filepath.Join(tmpDir, "wireops.yaml"), []byte(validContent), 0644); err != nil {
		t.Fatalf("failed to create valid test file: %v", err)
	}

	multipleContent := `
version: wireops.v1
name: api
---
version: wireops.v1
name: api2
`
	if err := os.WriteFile(filepath.Join(tmpDir, "multiple.yaml"), []byte(multipleContent), 0644); err != nil {
		t.Fatalf("failed to create multiple test file: %v", err)
	}

	invalidContent := `
name: api
`
	if err := os.WriteFile(filepath.Join(tmpDir, "invalid.yaml"), []byte(invalidContent), 0644); err != nil {
		t.Fatalf("failed to create invalid test file: %v", err)
	}

	def, err := ParseWireopsFile(tmpDir, "", "wireops.yaml")
	if err != nil {
		t.Fatalf("expected no error for valid wireops file, got: %v", err)
	}
	if def.Name != "api" {
		t.Errorf("expected name 'api', got: %q", def.Name)
	}
	if def.DeployTimeoutSeconds != 300 {
		t.Errorf("expected deploy_timeout_seconds=300, got: %d", def.DeployTimeoutSeconds)
	}
	if def.Compose == nil || def.Compose.RemoveOrphans == nil || !*def.Compose.RemoveOrphans {
		t.Errorf("expected compose.remove_orphans=true, got: %+v", def.Compose)
	}
	if def.Jobs == nil || def.Jobs.WaitRunning == nil || !*def.Jobs.WaitRunning {
		t.Errorf("expected jobs.wait_running=true, got: %+v", def.Jobs)
	}
	if def.Worker == nil || len(def.Worker.Tags) != 2 {
		t.Errorf("expected worker.tags with 2 entries, got: %+v", def.Worker)
	}

	if _, err := ParseWireopsFile(tmpDir, "", "multiple.yaml"); err == nil {
		t.Errorf("expected error for multiple YAML documents, got nil")
	} else if !strings.Contains(err.Error(), "multiple YAML documents (separated by '---') are not allowed") {
		t.Errorf("expected error message to contain multiple documents warning, got: %v", err)
	}

	if _, err := ParseWireopsFile(tmpDir, "", "invalid.yaml"); err == nil {
		t.Errorf("expected error for missing version, got nil")
	} else if !strings.Contains(err.Error(), "version is required") {
		t.Errorf("expected error to mention missing version, got: %v", err)
	}

	if _, err := ParseWireopsFile(tmpDir, "", "does-not-exist.yaml"); err == nil {
		t.Errorf("expected error for missing file, got nil")
	}
}
