package executor

import "testing"

func TestMountExposesDockerSocket(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want bool
	}{
		{"exact var run socket", "/var/run/docker.sock", true},
		{"exact run socket", "/run/docker.sock", true},
		{"ancestor var run", "/var/run", true},
		{"ancestor run", "/run", true},
		{"root mount", "/", true},
		{"unrelated mount", "/var/lib/data", false},
		{"unrelated run subpath", "/run/secrets", false},
		{"trailing slash normalized", "/var/run/", true},
		{"whitespace trimmed", "  /run/docker.sock  ", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := mountExposesDockerSocket(tc.src); got != tc.want {
				t.Fatalf("mountExposesDockerSocket(%q) = %v, want %v", tc.src, got, tc.want)
			}
		})
	}
}
