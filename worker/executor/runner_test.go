package executor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMatchPattern(t *testing.T) {
	cases := []struct {
		val     string
		pattern string
		want    bool
	}{
		{"postgres:14", "postgres:*", true},
		{"postgres:14", "mysql:*", false},
		{"ubuntu:latest", "*:latest", true},
		{"ubuntu:latest", "*", true},
		{"ubuntu:latest", "ubuntu:latest", true},
		{"my-registry.com/ubuntu:latest", "my-registry.com/*", true},
		{"my-registry.com/ubuntu:latest", "*ubuntu:*", true},
		{"my-registry.com/ubuntu:14.04", "*ubuntu:14*", true},
		{"my-registry.com/ubuntu:14.04", "*ubuntu:15*", false},
	}

	for _, tc := range cases {
		got := matchPattern(tc.val, tc.pattern)
		if got != tc.want {
			t.Errorf("matchPattern(%q, %q) = %v, want %v", tc.val, tc.pattern, got, tc.want)
		}
	}
}

// TestRunJobVolumePathTraversal verifies that validateVolumePaths (used inline
// by RunJob) rejects relative / traversal paths while accepting absolute paths
// and named volumes. Policy allowlist enforcement is the server's
// responsibility — the worker must not duplicate it with hardcoded deny-lists.
func TestRunJobVolumePathTraversal(t *testing.T) {
	cases := []struct {
		name    string
		volumes []string
		wantErr bool
	}{
		{
			name:    "absolute path accepted (policy enforced server-side)",
			volumes: []string{"/var/run/docker.sock:/var/run/docker.sock"},
			wantErr: false,
		},
		{
			name:    "named volume accepted",
			volumes: []string{"my_volume:/data"},
			wantErr: false,
		},
		{
			name:    "safe absolute path accepted",
			volumes: []string{"/mnt/data:/data"},
			wantErr: false,
		},
		{
			name:    "traversal in relative path rejected",
			volumes: []string{"../secrets:/data"},
			wantErr: true,
		},
		{
			name:    "relative path with slash rejected",
			volumes: []string{"my/relative/path:/data"},
			wantErr: true,
		},
		{
			name:    "empty volume skipped",
			volumes: []string{""},
			wantErr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateVolumePaths(tc.volumes)
			gotErr := err != nil
			if gotErr != tc.wantErr {
				t.Errorf("volumes=%v: err=%v, wantErr=%v", tc.volumes, err, tc.wantErr)
			}
		})
	}
}


func TestSafeEnv(t *testing.T) {
	// Set a custom environment variable to verify preservation
	os.Setenv("TEST_WIREOPS_VAR", "preserve-me")
	defer os.Unsetenv("TEST_WIREOPS_VAR")

	env := safeEnv()

	// Verify that TEST_WIREOPS_VAR is preserved
	foundPreserved := false
	pathCount := 0
	var pathVal string

	for _, kv := range env {
		if kv == "TEST_WIREOPS_VAR=preserve-me" {
			foundPreserved = true
		}
		if strings.HasPrefix(strings.ToUpper(kv), "PATH=") {
			pathCount++
			pathVal = kv
		}
	}

	if !foundPreserved {
		t.Error("expected custom env var to be preserved, but it was not found")
	}

	if pathCount != 1 {
		t.Errorf("expected PATH variable to appear exactly once, got count: %d", pathCount)
	}

	expectedPrefix := "PATH="
	if !strings.HasPrefix(strings.ToUpper(pathVal), expectedPrefix) {
		t.Errorf("expected PATH variable prefix, got: %q", pathVal)
	}

	pathDirs := strings.Split(pathVal[len(expectedPrefix):], string(filepath.ListSeparator))
	expectedDirs := []string{"/bin", "/sbin", "/usr/bin", "/usr/sbin", "/usr/local/bin"}

	if len(pathDirs) != len(expectedDirs) {
		t.Errorf("expected %d directories in PATH, got %d: %q", len(expectedDirs), len(pathDirs), pathDirs)
	}

	for i, d := range expectedDirs {
		if i < len(pathDirs) && pathDirs[i] != d {
			t.Errorf("expected path directory at index %d to be %q, got %q", i, d, pathDirs[i])
		}
	}
}
