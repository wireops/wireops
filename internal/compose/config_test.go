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
