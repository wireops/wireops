package safepath

import (
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
