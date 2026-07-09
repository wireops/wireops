package executor

import (
	"encoding/base64"
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

func TestPrepareComposeFileCleansWorkDirByDefault(t *testing.T) {
	tmpDir := t.TempDir()
	oldStackDir := stackDir
	stackDir = tmpDir
	defer func() { stackDir = oldStackDir }()
	t.Setenv("WORKER_KEEP_WORKDIR", "")

	composeB64 := base64.StdEncoding.EncodeToString([]byte("services:\n  app:\n    image: nginx\n"))
	workDir, composeFile, cleanup, err := prepareComposeFile("stack-1", "cmd-1", composeB64)
	if err != nil {
		t.Fatalf("prepareComposeFile failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workDir, composeFile)); err != nil {
		t.Fatalf("expected compose file to exist before cleanup: %v", err)
	}

	cleanup()

	if _, err := os.Stat(workDir); !os.IsNotExist(err) {
		t.Fatalf("expected workdir to be removed, got err=%v", err)
	}
}

func TestPrepareComposeFileCanKeepWorkDirForDebugging(t *testing.T) {
	tmpDir := t.TempDir()
	oldStackDir := stackDir
	stackDir = tmpDir
	defer func() { stackDir = oldStackDir }()
	t.Setenv("WORKER_KEEP_WORKDIR", "true")

	composeB64 := base64.StdEncoding.EncodeToString([]byte("services:\n  app:\n    image: nginx\n"))
	workDir, composeFile, cleanup, err := prepareComposeFile("stack-1", "cmd-1", composeB64)
	if err != nil {
		t.Fatalf("prepareComposeFile failed: %v", err)
	}

	cleanup()

	if _, err := os.Stat(filepath.Join(workDir, composeFile)); err != nil {
		t.Fatalf("expected compose file to be kept after cleanup: %v", err)
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

func TestIsAllowedPath(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "wireops-test-stackdir")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	oldStackDir := stackDir
	stackDir = tmpDir
	defer func() { stackDir = oldStackDir }()

	oldAllowedDirs := os.Getenv("WORKER_ALLOWED_IMPORT_DIRS")
	defer func() {
		if oldAllowedDirs == "" {
			os.Unsetenv("WORKER_ALLOWED_IMPORT_DIRS")
		} else {
			os.Setenv("WORKER_ALLOWED_IMPORT_DIRS", oldAllowedDirs)
		}
	}()

	safeFile := filepath.Join(tmpDir, "compose.yml")
	if err := os.WriteFile(safeFile, []byte("version: '3'"), 0600); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	passwdSymlink := filepath.Join(tmpDir, "evil-symlink.yml")
	if err := os.Symlink("/etc/passwd", passwdSymlink); err != nil {
		t.Fatalf("failed to create evil symlink: %v", err)
	}

	safeSymlink := filepath.Join(tmpDir, "safe-symlink.yml")
	if err := os.Symlink(safeFile, safeSymlink); err != nil {
		t.Fatalf("failed to create safe symlink: %v", err)
	}

	cases := []struct {
		name        string
		path        string
		allowedDirs string
		want        bool
	}{
		{
			name: "allow safe file inside stackDir",
			path: safeFile,
			want: true,
		},
		{
			name: "allow safe symlink inside stackDir pointing inside stackDir",
			path: safeSymlink,
			want: true,
		},
		{
			name: "block evil symlink pointing outside stackDir to sensitive root",
			path: passwdSymlink,
			want: false,
		},
		{
			name:        "block prefix collision (e.g. stackDir + evil)",
			path:        tmpDir + "-evil",
			allowedDirs: tmpDir,
			want:        false,
		},
		{
			name: "block sensitive root (/etc/passwd)",
			path: "/etc/passwd",
			want: false,
		},
		{
			name:        "allow extra allowed import dirs if configured",
			path:        "/tmp/some-external-file.yml",
			allowedDirs: "/tmp",
			want:        true,
		},
		{
			name:        "deny extra allowed import dirs if not matching allowed",
			path:        "/var/log/syslog.yml",
			allowedDirs: "/tmp",
			want:        false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.allowedDirs != "" {
				os.Setenv("WORKER_ALLOWED_IMPORT_DIRS", tc.allowedDirs)
			} else {
				os.Unsetenv("WORKER_ALLOWED_IMPORT_DIRS")
			}
			got := isAllowedPath(tc.path)
			if got != tc.want {
				t.Errorf("isAllowedPath(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}
