package job

import "testing"

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
