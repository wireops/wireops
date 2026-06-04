package job

import (
	"errors"
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
title: Cleanup Task
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
title: No Cron Job
description: Missing cron
image: alpine
command: echo hello
`,
			want: false,
		},
		{
			name: "MissingImage",
			input: `
title: No Image Job
description: Missing image
cron: "0 * * * *"
command: echo hello
`,
			want: false,
		},
		{
			name: "MissingTitle",
			input: `
description: Missing title
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
				Title:       "Test Job",
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
			name: "Missing title",
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
			wantErr: "title is required",
		},
		{
			name: "Missing description",
			def: Definition{
				Title: "Test Job",
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
				Title:       "Test Job",
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
				Title:       "Test Job",
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
				Title:       "Test Job",
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
				Title:       "Test Job",
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
				Title:       "Test Job",
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
				Title:       "Test Job",
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
				Title: "",
				Cron:  "",
				Resources: Resources{
					CPU:    "",
					Memory: "",
				},
			},
			wantErr: "title is required, description is required, image is required, cron is required, resources.cpu is required, resources.memory is required, resources.timeout is required",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.def.validate()
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

