package sync_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/wireops/wireops/internal/sync"
)

func TestRenderer_GenerateRevision(t *testing.T) {
	// Setup PocketBase test app
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	defer app.Cleanup()

	// Initialize tables needed
	createTestCollections(t, app)

	// Create a dummy workspace
	workDir := t.TempDir()
	composePath := filepath.Join(workDir, "docker-compose.yml")
	composeContent := `
name: test_stack
services:
  web:
    image: nginx:latest
    labels:
      user.label: "value"
`
	if err := os.WriteFile(composePath, []byte(composeContent), 0644); err != nil {
		t.Fatalf("failed to write compose file: %v", err)
	}

	// Create Stack and Repo records
	repo := createTestRepo(t, app, "Test Repo", "main")
	stack := createTestStack(t, app, repo.Id, "test_stack")

	renderer := sync.NewRenderer(app)
	ctx := context.Background()

	// 1. First Generation
	res1, err := renderer.GenerateRevision(ctx, stack, repo, workDir, "docker-compose.yml", nil, "commitA", false, "embedded")
	if err != nil {
		t.Fatalf("unexpected error on first render: %v", err)
	}
	if res1.Version != 1 {
		t.Errorf("expected version 1, got %d", res1.Version)
	}
	t.Logf("First generated checksum: %s", res1.Checksum)

	// Refresh stack to see updates
	stack, _ = app.FindRecordById("stacks", stack.Id)

	// 2. Second Generation, no changes (using SAME commitA to verify identity)
	time.Sleep(50 * time.Millisecond) // ensure time moves
	res2, err := renderer.GenerateRevision(ctx, stack, repo, workDir, "docker-compose.yml", nil, "commitA", false, "embedded")
	if err != nil {
		t.Fatalf("unexpected error on second render: %v", err)
	}
	if res2.Version != 1 {
		t.Errorf("expected version 1, got %d", res2.Version)
	}
	if res1.Checksum != res2.Checksum {
		t.Errorf("expected checksums to match when compose is identical (got %s != %s)", res1.Checksum, res2.Checksum)
	}

	// 3. Third Generation, force bump (still using commitA, but forced)
	res3, err := renderer.GenerateRevision(ctx, stack, repo, workDir, "docker-compose.yml", nil, "commitA", true, "embedded")
	if err != nil {
		t.Fatalf("unexpected error on forced render: %v", err)
	}
	if res3.Version != 2 {
		t.Errorf("expected version 2 on forced increment, got %d", res3.Version)
	}

	// Refresh stack again
	stack, _ = app.FindRecordById("stacks", stack.Id)

	// 4. Update compose file, should bump automatically
	composeContent2 := `
name: test_stack
services:
  web:
    image: nginx:alpine
    labels:
      user.label: "value"
`
	if err := os.WriteFile(composePath, []byte(composeContent2), 0644); err != nil {
		t.Fatalf("failed to update compose file: %v", err)
	}

	res4, err := renderer.GenerateRevision(ctx, stack, repo, workDir, "docker-compose.yml", nil, "commitC", false, "embedded")
	if err != nil {
		t.Fatalf("unexpected error on changed render: %v", err)
	}
	if res4.Version != 3 {
		t.Errorf("expected version 3 on changed file, got %d", res4.Version)
	}
	if res4.Checksum == res3.Checksum {
		t.Errorf("expected new checksum after file changes")
	}

	// Verify the file was written
	renderedFile := renderer.GetRevisionFilePath(stack.Id, res4.Version)
	content, err := os.ReadFile(renderedFile)
	if err != nil {
		t.Fatalf("failed to read written config: %v", err)
	}
	contentStr := string(content)
	if !contains(contentStr, `dev.wireops.managed: "true"`) {
		t.Errorf("missing dev.wireops.managed label")
	}
	if !contains(contentStr, `user.label: value`) {
		t.Errorf("missing original user label")
	}
	if !contains(contentStr, `dev.wireops.version: "3"`) {
		t.Errorf("missing correct version label")
	}
	if !contains(contentStr, `dev.wireops.repository.commit_sha: commitC`) {
		t.Errorf("missing correct commit label")
	}
}

func TestRenderer_GenerateRevision_NoSecrets(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	defer app.Cleanup()

	createTestCollections(t, app)

	workDir := t.TempDir()
	composePath := filepath.Join(workDir, "docker-compose.yml")
	composeContent := `
name: secret_stack
services:
  web:
    image: nginx:latest
    environment:
      - MY_SECRET=${MY_SECRET}
      - ANOTHER_VAR=${ANOTHER_VAR:-default_val}
`
	if err := os.WriteFile(composePath, []byte(composeContent), 0644); err != nil {
		t.Fatalf("failed to write compose file: %v", err)
	}

	repo := createTestRepo(t, app, "Secret Repo", "main")
	stack := createTestStack(t, app, repo.Id, "secret_stack")

	renderer := sync.NewRenderer(app)
	ctx := context.Background()

	// Env vars containing sensible data
	envVars := []string{
		"MY_SECRET=super_secret_value",
		"ANOTHER_VAR=my_override",
	}

	res, err := renderer.GenerateRevision(ctx, stack, repo, workDir, "docker-compose.yml", envVars, "commit123", false, "embedded")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	renderedFile := renderer.GetRevisionFilePath(stack.Id, res.Version)
	content, err := os.ReadFile(renderedFile)
	if err != nil {
		t.Fatalf("failed to read written config: %v", err)
	}
	contentStr := string(content)

	if contains(contentStr, "super_secret_value") {
		t.Errorf("Security risk: secret value 'super_secret_value' was interpolated into the saved compose file!")
	}
	if contains(contentStr, "my_override") {
		t.Errorf("Environment variable override 'my_override' was interpolated into the saved compose file!")
	}

	// The docker compose config outputs environment variables as a list, so we check for list syntax
	if !contains(contentStr, `- MY_SECRET=${MY_SECRET}`) {
		t.Errorf("Expected MY_SECRET expression to be preserved. Output:\n%s", contentStr)
	}
	if !contains(contentStr, `- ANOTHER_VAR=${ANOTHER_VAR:-default_val}`) {
		t.Errorf("Expected ANOTHER_VAR expression to be preserved. Output:\n%s", contentStr)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && bytesContains([]byte(s), []byte(substr))
}

func bytesContains(s, substr []byte) bool {
	if len(substr) == 0 {
		return true
	}
	if len(s) < len(substr) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			if s[i+j] != substr[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// Helper methods to create tables and dummy data for tests
func createTestCollections(t *testing.T, app core.App) {
	// Simple implementations that create exactly what we need for the renderer

	// Repositories
	repos := core.NewBaseCollection("repositories")
	repos.Fields.Add(&core.TextField{Name: "name"})
	repos.Fields.Add(&core.TextField{Name: "git_url"})
	repos.Fields.Add(&core.TextField{Name: "branch"})
	if err := app.Save(repos); err != nil {
		t.Fatalf("failed to create repos collection: %v", err)
	}

	// Stacks
	stacks := core.NewBaseCollection("stacks")
	stacks.Fields.Add(&core.TextField{Name: "name"})
	stacks.Fields.Add(&core.RelationField{Name: "repository", CollectionId: repos.Id, MaxSelect: 1})
	stacks.Fields.Add(&core.NumberField{Name: "current_version"})
	stacks.Fields.Add(&core.TextField{Name: "desired_commit"})
	stacks.Fields.Add(&core.TextField{Name: "checksum"})
	if err := app.Save(stacks); err != nil {
		t.Fatalf("failed to create stacks collection: %v", err)
	}

	// Stack Revisions
	revs := core.NewBaseCollection("stack_revisions")
	revs.Fields.Add(&core.RelationField{Name: "stack", CollectionId: stacks.Id, MaxSelect: 1})
	revs.Fields.Add(&core.NumberField{Name: "version"})
	revs.Fields.Add(&core.TextField{Name: "commit_sha"})
	revs.Fields.Add(&core.TextField{Name: "checksum"})
	revs.Fields.Add(&core.TextField{Name: "compose_path"})
	if err := app.Save(revs); err != nil {
		t.Fatalf("failed to create stack revisions collection: %v", err)
	}
}

func createTestRepo(t *testing.T, app core.App, name, branch string) *core.Record {
	col, _ := app.FindCollectionByNameOrId("repositories")
	rec := core.NewRecord(col)
	rec.Set("name", name)
	rec.Set("branch", branch)
	rec.Set("git_url", "https://example.com/repo.git")
	if err := app.Save(rec); err != nil {
		t.Fatalf("failed to create repo: %v", err)
	}
	return rec
}

func createTestStack(t *testing.T, app core.App, repoId, name string) *core.Record {
	col, _ := app.FindCollectionByNameOrId("stacks")
	rec := core.NewRecord(col)
	rec.Set("name", name)
	rec.Set("repository", repoId)
	if err := app.Save(rec); err != nil {
		t.Fatalf("failed to create stack: %v", err)
	}
	return rec
}
