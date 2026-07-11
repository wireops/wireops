package routes

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wireops/wireops/internal/wireops"
)

func TestWireopsValidationErrors(t *testing.T) {
	err := &wireops.ValidationError{Errors: []string{"name is required", "version is required"}}
	got := wireopsValidationErrors(err)
	if len(got) != 2 || got[0] != "name is required" || got[1] != "version is required" {
		t.Fatalf("expected wrapped validation errors, got: %v", got)
	}

	generic := os.ErrNotExist
	got = wireopsValidationErrors(generic)
	if len(got) != 1 || got[0] != generic.Error() {
		t.Fatalf("expected fallback to err.Error(), got: %v", got)
	}
}

func TestResolveWireopsComposeFileSingleMatch(t *testing.T) {
	repoDir := t.TempDir()
	writeFile(t, repoDir, "wireops.yaml", "version: wireops.v1\nname: api\n")
	writeFile(t, repoDir, "docker-compose.yml", "services:\n  web:\n    image: nginx\n")

	def := &wireops.Definition{}
	resolveWireopsComposeFile(repoDir, "wireops.yaml", def)

	if def.ResolutionError != "" {
		t.Fatalf("expected no resolution error, got: %s", def.ResolutionError)
	}
	if def.ResolvedComposePath != "." || def.ResolvedComposeFile != "docker-compose.yml" {
		t.Fatalf("expected resolved compose path='.' file='docker-compose.yml', got path=%q file=%q", def.ResolvedComposePath, def.ResolvedComposeFile)
	}
}

func TestResolveWireopsComposeFileNoMatch(t *testing.T) {
	repoDir := t.TempDir()
	writeFile(t, repoDir, "wireops.yaml", "version: wireops.v1\nname: api\n")

	def := &wireops.Definition{}
	resolveWireopsComposeFile(repoDir, "wireops.yaml", def)

	if def.ResolutionError == "" {
		t.Fatal("expected resolution error when no compose file is present")
	}
	if def.ResolvedComposeFile != "" {
		t.Fatalf("expected no resolved compose file, got: %s", def.ResolvedComposeFile)
	}
}

func TestResolveWireopsComposeFileAmbiguous(t *testing.T) {
	repoDir := t.TempDir()
	writeFile(t, repoDir, "wireops.yaml", "version: wireops.v1\nname: api\n")
	writeFile(t, repoDir, "docker-compose.yml", "services:\n  web:\n    image: nginx\n")
	writeFile(t, repoDir, "compose.yml", "services:\n  api:\n    image: alpine\n")

	def := &wireops.Definition{}
	resolveWireopsComposeFile(repoDir, "wireops.yaml", def)

	if def.ResolutionError == "" {
		t.Fatal("expected resolution error when multiple compose files are present")
	}
	if def.ResolvedComposeFile != "" {
		t.Fatalf("expected no resolved compose file when ambiguous, got: %s", def.ResolvedComposeFile)
	}
}

func TestResolveWireopsComposeFileNestedDir(t *testing.T) {
	repoDir := t.TempDir()
	apiDir := filepath.Join(repoDir, "apps", "api")
	if err := os.MkdirAll(apiDir, 0755); err != nil {
		t.Fatalf("failed to create nested dir: %v", err)
	}
	writeFile(t, apiDir, "wireops.yaml", "version: wireops.v1\nname: api\n")
	writeFile(t, apiDir, "compose.yml", "services:\n  web:\n    image: nginx\n")

	def := &wireops.Definition{}
	resolveWireopsComposeFile(repoDir, "apps/api/wireops.yaml", def)

	if def.ResolutionError != "" {
		t.Fatalf("expected no resolution error, got: %s", def.ResolutionError)
	}
	if def.ResolvedComposePath != filepath.Join("apps", "api") || def.ResolvedComposeFile != "compose.yml" {
		t.Fatalf("expected resolved path=apps/api file=compose.yml, got path=%q file=%q", def.ResolvedComposePath, def.ResolvedComposeFile)
	}
}

func boolPtr(b bool) *bool { return &b }

func TestResolveWireopsStackFieldsDefaults(t *testing.T) {
	removeOrphans, forcePull, waitRunningJobs, workerTags := resolveWireopsStackFields(&wireops.Definition{})

	if !removeOrphans {
		t.Error("expected remove_orphans to default to true")
	}
	if forcePull {
		t.Error("expected force_pull to default to false")
	}
	if waitRunningJobs != "never" {
		t.Errorf("expected wait_running_jobs to default to 'never', got %q", waitRunningJobs)
	}
	if workerTags == nil || len(workerTags) != 0 {
		t.Errorf("expected worker_tags to default to an empty non-nil slice, got %v", workerTags)
	}
}

func TestResolveWireopsStackFieldsExplicitValues(t *testing.T) {
	def := &wireops.Definition{
		Compose: &wireops.ComposeConfig{
			RemoveOrphans: boolPtr(false),
			ForcePull:     boolPtr(true),
		},
		Jobs: &wireops.JobsConfig{
			WaitRunning: boolPtr(true),
		},
		Worker: &wireops.WorkerConfig{
			Tags: []string{"prod", "amd64"},
		},
	}

	removeOrphans, forcePull, waitRunningJobs, workerTags := resolveWireopsStackFields(def)

	if removeOrphans {
		t.Error("expected explicit remove_orphans: false to be honored")
	}
	if !forcePull {
		t.Error("expected explicit force_pull: true to be honored")
	}
	if waitRunningJobs != "always" {
		t.Errorf("expected wait_running_jobs='always', got %q", waitRunningJobs)
	}
	if len(workerTags) != 2 || workerTags[0] != "prod" || workerTags[1] != "amd64" {
		t.Errorf("expected worker_tags=[prod amd64], got %v", workerTags)
	}
}

func TestResolveWireopsStackFieldsWaitRunningFalseIsNever(t *testing.T) {
	def := &wireops.Definition{Jobs: &wireops.JobsConfig{WaitRunning: boolPtr(false)}}
	_, _, waitRunningJobs, _ := resolveWireopsStackFields(def)
	if waitRunningJobs != "never" {
		t.Errorf("expected wait_running_jobs='never' for explicit false, got %q", waitRunningJobs)
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write %s: %v", name, err)
	}
}
