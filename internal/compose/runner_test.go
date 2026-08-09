package compose

import (
	"reflect"
	"strings"
	"testing"
)

func TestBuildUpArgs(t *testing.T) {
	cases := []struct {
		name          string
		composeFile   string
		removeOrphans bool
		forcePull     bool
		want          []string
	}{
		{
			name:        "NoFlags",
			composeFile: "docker-compose.yml",
			want:        []string{"compose", "-f", "docker-compose.yml", "up", "-d"},
		},
		{
			name:          "RemoveOrphansOnly",
			composeFile:   "docker-compose.yml",
			removeOrphans: true,
			want:          []string{"compose", "-f", "docker-compose.yml", "up", "-d", "--remove-orphans"},
		},
		{
			name:        "ForcePullOnly",
			composeFile: "docker-compose.yml",
			forcePull:   true,
			want:        []string{"compose", "-f", "docker-compose.yml", "up", "-d", "--pull", "always"},
		},
		{
			name:          "BothFlags",
			composeFile:   "compose.yml",
			removeOrphans: true,
			forcePull:     true,
			want:          []string{"compose", "-f", "compose.yml", "up", "-d", "--remove-orphans", "--pull", "always"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildUpArgs(tc.composeFile, tc.removeOrphans, tc.forcePull)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("buildUpArgs(%q, %v, %v) = %v, want %v", tc.composeFile, tc.removeOrphans, tc.forcePull, got, tc.want)
			}
		})
	}
}

func TestSafeEnv(t *testing.T) {
	t.Run("empty dockerConfigDir adds no DOCKER_CONFIG", func(t *testing.T) {
		env := safeEnv("")
		for _, kv := range env {
			if strings.HasPrefix(kv, "DOCKER_CONFIG=") {
				t.Fatalf("expected no DOCKER_CONFIG entry, found %q", kv)
			}
		}
	})

	t.Run("non-empty dockerConfigDir appends DOCKER_CONFIG", func(t *testing.T) {
		env := safeEnv("/tmp/registry-auth-dir")
		want := "DOCKER_CONFIG=/tmp/registry-auth-dir"
		found := false
		for _, kv := range env {
			if kv == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected env to contain %q, got %v", want, env)
		}
	})

	t.Run("non-empty dockerConfigDir overrides an inherited DOCKER_CONFIG", func(t *testing.T) {
		t.Setenv("DOCKER_CONFIG", "/inherited/from/worker/host")
		env := safeEnv("/tmp/registry-auth-dir")
		// safeEnv appends its own DOCKER_CONFIG after copying os.Environ(),
		// so the appended entry must be the last (and therefore winning, per
		// exec.Cmd.Env / os/exec semantics) DOCKER_CONFIG in the slice.
		lastIdx := -1
		for i, kv := range env {
			if strings.HasPrefix(kv, "DOCKER_CONFIG=") {
				lastIdx = i
			}
		}
		if lastIdx == -1 || env[lastIdx] != "DOCKER_CONFIG=/tmp/registry-auth-dir" {
			t.Fatalf("expected the last DOCKER_CONFIG entry to be the per-command dir, got %v", env)
		}
	})
}
