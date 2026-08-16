package safepath

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateBackupKey(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantErr   bool
		errSubstr string
	}{
		{name: "valid key", input: "wireops_backup_20260717.zip"},
		{name: "empty", input: "", wantErr: true, errSubstr: "cannot be empty"},
		{name: "traversal", input: "../../etc/passwd.zip", wantErr: true, errSubstr: "filename, not a path"},
		{name: "traversal encoded in middle", input: "foo/../../bar.zip", wantErr: true, errSubstr: "invalid characters"},
		{name: "absolute path", input: "/etc/passwd.zip", wantErr: true, errSubstr: "filename, not a path"},
		{name: "nested path", input: "sub/dir/backup.zip", wantErr: true, errSubstr: "filename, not a path"},
		{name: "backslash", input: "sub\\backup.zip", wantErr: true, errSubstr: "filename, not a path"},
		{name: "wrong extension", input: "backup.tar", wantErr: true, errSubstr: "must end in .zip"},
		{name: "no extension", input: "backup", wantErr: true, errSubstr: "must end in .zip"},
		{name: "just dot", input: ".", wantErr: true},
		{name: "just dotdot", input: "..", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateBackupKey(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ValidateBackupKey(%q) expected error, got nil", tt.input)
					return
				}
				if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("ValidateBackupKey(%q) error = %q, want to contain %q", tt.input, err.Error(), tt.errSubstr)
				}
			} else if err != nil {
				t.Errorf("ValidateBackupKey(%q) unexpected error: %v", tt.input, err)
			}
		})
	}
}

func TestCleanRelativePath(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		want      string
		wantErr   bool
		errSubstr string
	}{
		{
			name:    "simple relative file",
			input:   "job.yaml",
			want:    "job.yaml",
			wantErr: false,
		},
		{
			name:    "nested relative file",
			input:   "jobs/prod/job.yaml",
			want:    "jobs/prod/job.yaml",
			wantErr: false,
		},
		{
			name:    "relative path needing cleaning",
			input:   "jobs/nested/../job.yaml",
			want:    "jobs/job.yaml",
			wantErr: false,
		},
		{
			name:    "relative path with dot slash needing cleaning",
			input:   "./jobs/./job.yaml",
			want:    "jobs/job.yaml",
			wantErr: false,
		},
		{
			name:      "empty path error",
			input:     "",
			wantErr:   true,
			errSubstr: "cannot be empty",
		},
		{
			name:      "absolute path error (root)",
			input:     "/job.yaml",
			wantErr:   true,
			errSubstr: "path is absolute or escapes base directory",
		},
		{
			name:      "absolute path error (deep)",
			input:     "/var/log/job.yaml",
			wantErr:   true,
			errSubstr: "path is absolute or escapes base directory",
		},
		{
			name:      "traversal escape path error",
			input:     "../job.yaml",
			wantErr:   true,
			errSubstr: "path is absolute or escapes base directory",
		},
		{
			name:      "nested traversal escape path error",
			input:     "jobs/../../job.yaml",
			wantErr:   true,
			errSubstr: "path is absolute or escapes base directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CleanRelativePath(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("CleanRelativePath(%q) expected error, got nil", tt.input)
					return
				}
				if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("CleanRelativePath(%q) error = %q, want to contain %q", tt.input, err.Error(), tt.errSubstr)
				}
			} else {
				if err != nil {
					t.Errorf("CleanRelativePath(%q) unexpected error: %v", tt.input, err)
					return
				}
				if got != tt.want {
					t.Errorf("CleanRelativePath(%q) = %q, want %q", tt.input, got, tt.want)
				}
			}
		})
	}
}

func TestValidateConfigName(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantErr   bool
		errSubstr string
	}{
		{name: "valid", input: "nginx-conf"},
		{name: "valid with underscore", input: "app_settings"},
		{name: "empty", input: "", wantErr: true, errSubstr: "cannot be empty"},
		{name: "slash", input: "foo/bar", wantErr: true, errSubstr: "path separators"},
		{name: "backslash", input: "foo\\bar", wantErr: true, errSubstr: "path separators"},
		{name: "dot", input: ".", wantErr: true, errSubstr: "start with"},
		{name: "dotdot", input: "..", wantErr: true, errSubstr: "start with"},
		{name: "leading dot", input: ".hidden", wantErr: true, errSubstr: "start with"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConfigName(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ValidateConfigName(%q) expected error, got nil", tt.input)
					return
				}
				if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("ValidateConfigName(%q) error = %q, want to contain %q", tt.input, err.Error(), tt.errSubstr)
				}
			} else if err != nil {
				t.Errorf("ValidateConfigName(%q) unexpected error: %v", tt.input, err)
			}
		})
	}
}

func TestValidateContainerMountPath(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantErr   bool
		errSubstr string
	}{
		{name: "valid absolute", input: "/etc/nginx/nginx.conf"},
		{name: "valid root child", input: "/config"},
		{name: "empty", input: "", wantErr: true, errSubstr: "cannot be empty"},
		{name: "relative", input: "etc/nginx.conf", wantErr: true, errSubstr: "must be absolute"},
		{name: "traversal", input: "/etc/../../secret", wantErr: true, errSubstr: "not a clean absolute path"},
		{name: "root itself", input: "/", wantErr: true, errSubstr: "not a clean absolute path"},
		{name: "trailing slash", input: "/etc/nginx/", wantErr: true, errSubstr: "not a clean absolute path"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateContainerMountPath(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ValidateContainerMountPath(%q) expected error, got nil", tt.input)
					return
				}
				if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("ValidateContainerMountPath(%q) error = %q, want to contain %q", tt.input, err.Error(), tt.errSubstr)
				}
			} else if err != nil {
				t.Errorf("ValidateContainerMountPath(%q) unexpected error: %v", tt.input, err)
			}
		})
	}
}

func TestOpenRootFindsFileInsideRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "target.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}

	r, rel, err := OpenRoot(root, root)
	if err != nil {
		t.Fatalf("OpenRoot: %v", err)
	}
	defer r.Close()

	data, err := r.ReadFile(filepath.Join(rel, "target.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "ok" {
		t.Fatalf("content = %q, want %q", data, "ok")
	}
}

func TestOpenRootDefaultsRootToDir(t *testing.T) {
	dir := t.TempDir()
	r, rel, err := OpenRoot("", dir)
	if err != nil {
		t.Fatalf("OpenRoot: %v", err)
	}
	defer r.Close()
	if rel != "." {
		t.Fatalf("rel = %q, want %q", rel, ".")
	}
}

func TestOpenRootRejectsEscapingDir(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	if _, _, err := OpenRoot(root, outside); err == nil {
		t.Fatal("expected an error when dir is outside root")
	}
}

// TestOpenRootRejectsSymlinkedIntermediateDirectory is the core guarantee
// this function exists for: a symlinked *intermediate* path component (not
// just a symlinked final entry) must not let a caller read outside root —
// os.Root refuses to follow it, unlike a plain os.Open/os.Lstat on a joined
// path string.
func TestOpenRootRejectsSymlinkedIntermediateDirectory(t *testing.T) {
	root := t.TempDir()
	outsideDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(outsideDir, "secret.txt"), []byte("nope"), 0o644); err != nil {
		t.Fatalf("write outside target: %v", err)
	}
	linkDir := filepath.Join(root, "escape")
	if err := os.Symlink(outsideDir, linkDir); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	r, rel, err := OpenRoot(root, linkDir)
	if err != nil {
		// Rejected up front (path-string resolution) — also acceptable.
		return
	}
	defer r.Close()
	if _, err := r.ReadFile(filepath.Join(rel, "secret.txt")); err == nil {
		t.Fatal("expected reading through a symlinked intermediate directory to fail")
	}
}

func TestOpenRootReturnsNotExistForMissingRoot(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	_, _, err := OpenRoot(missing, missing)
	if err == nil {
		t.Fatal("expected an error for a nonexistent root")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected errors.Is(err, fs.ErrNotExist), got: %v", err)
	}
}
