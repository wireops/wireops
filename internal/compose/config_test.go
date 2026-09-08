package compose

import "testing"

func TestIsComposeFile(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name: "ValidComposeFile",
			input: `
services:
  web:
    image: nginx
  db:
    image: postgres
`,
			want: true,
		},
		{
			name: "EmptyServicesMap",
			input: `
services: {}
`,
			want: false,
		},
		{
			name: "MissingServicesKey",
			input: `
version: "3"
volumes:
  data: {}
`,
			want: false,
		},
		{
			name: "JobYAMLInput",
			input: `
title: My Job
image: alpine
cron: "0 * * * *"
command: echo hello
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
			got := IsComposeFile([]byte(tc.input))
			if got != tc.want {
				t.Errorf("IsComposeFile(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

func TestComposeCandidateError(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		wantError bool
	}{
		{
			name: "ValidComposeFile",
			input: `
services:
  web:
    image: nginx
`,
			wantError: false,
		},
		{
			name:      "MalformedYAML",
			input:     `{not: valid: yaml:`,
			wantError: true,
		},
		{
			name: "MissingServicesKey",
			input: `
version: "3"
volumes:
  data: {}
`,
			wantError: true,
		},
		{
			name: "EmptyServicesMap",
			input: `
services: {}
`,
			wantError: true,
		},
		{
			name: "ServicesTypeMismatch",
			input: `
services: []
`,
			wantError: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ComposeCandidateError([]byte(tc.input))
			if tc.wantError && err == nil {
				t.Errorf("ComposeCandidateError(%q) = nil, want error", tc.name)
			}
			if !tc.wantError && err != nil {
				t.Errorf("ComposeCandidateError(%q) = %v, want nil", tc.name, err)
			}
		})
	}
}

func TestInitServiceNames(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  map[string]bool
	}{
		{
			name: "MapFormLabels",
			input: `
services:
  web:
    image: nginx
  migrate:
    image: myapp
    labels:
      customization.init: "true"
`,
			want: map[string]bool{"migrate": true},
		},
		{
			name: "ListFormLabels",
			input: `
services:
  web:
    image: nginx
  migrate:
    image: myapp
    labels:
      - "customization.init=true"
`,
			want: map[string]bool{"migrate": true},
		},
		{
			name: "FalsyValueIsNotInit",
			input: `
services:
  migrate:
    image: myapp
    labels:
      customization.init: "false"
`,
			want: map[string]bool{},
		},
		{
			name: "NoLabelsDefined",
			input: `
services:
  web:
    image: nginx
`,
			want: map[string]bool{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := InitServiceNames([]byte(tc.input))
			if err != nil {
				t.Fatalf("InitServiceNames(%q) error = %v", tc.name, err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("InitServiceNames(%q) = %v, want %v", tc.name, got, tc.want)
			}
			for name := range tc.want {
				if !got[name] {
					t.Errorf("InitServiceNames(%q) missing expected init service %q, got %v", tc.name, name, got)
				}
			}
		})
	}
}
