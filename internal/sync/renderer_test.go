package sync_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/wireops/wireops/internal/sync"
)

const (
	errCreateTestApp    = "failed to create test app: %v"
	errWriteComposeFile = "failed to write compose file: %v"
)

func TestRendererGenerateRevision(t *testing.T) {
	app, workDir, composePath := setupRendererTest(t)
	composeContent := `
name: test_stack
services:
  web:
    image: nginx:latest
    labels:
      user.label: "value"
`
	writeTestComposeFile(t, composePath, composeContent)

	// Create Stack and Repo records
	repo := createTestRepo(t, app, "Test Repo", "main")
	stack := createTestStack(t, app, repo.Id, "test_stack")

	renderer := sync.NewRenderer(app)
	ctx := context.Background()

	// 1. First Generation
	res1, err := renderer.GenerateRevision(ctx, stack, repo, workDir, "docker-compose.yml", nil, "commitA", false, "", "embedded", nil)
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
	res2, err := renderer.GenerateRevision(ctx, stack, repo, workDir, "docker-compose.yml", nil, "commitA", false, "", "embedded", nil)
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
	res3, err := renderer.GenerateRevision(ctx, stack, repo, workDir, "docker-compose.yml", nil, "commitA", true, "", "embedded", nil)
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
	writeTestComposeFile(t, composePath, composeContent2)

	res4, err := renderer.GenerateRevision(ctx, stack, repo, workDir, "docker-compose.yml", nil, "commitC", false, "", "embedded", nil)
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
	contentStr := readRenderedFile(t, renderer, stack.Id, res4.Version)
	if !contains(contentStr, `dev.wireops.managed: "true"`) {
		t.Errorf("missing dev.wireops.managed label")
	}
	if !contains(contentStr, `user.label: value`) {
		t.Errorf("missing original user label")
	}
	if !contains(contentStr, `annotations:`) {
		t.Errorf("missing annotations block")
	}
	if !contains(contentStr, `dev.wireops.version: "3"`) {
		t.Errorf("missing correct version in annotations")
	}
	if !contains(contentStr, `dev.wireops.repository.commit_sha: commitC`) {
		t.Errorf("missing correct commit in annotations")
	}
}

func TestRendererStripReservedLabels(t *testing.T) {
	app, workDir, composePath := setupRendererTest(t)
	// Service defines labels that use the reserved dev.wireops prefix —
	// these must be stripped and replaced by system-injected values.
	composeContent := `
name: sanitize_stack
services:
  web:
    image: nginx:latest
    labels:
      dev.wireops.managed: "hijack"
      dev.wireops.stack_id: "spoofed-id"
      user.safe.label: "keep-me"
    annotations:
      dev.wireops.checksum: "fake"
      user.note: "safe"
`
	writeTestComposeFile(t, composePath, composeContent)

	repo := createTestRepo(t, app, "Sanitize Repo", "main")
	stack := createTestStack(t, app, repo.Id, "sanitize_stack")

	renderer := sync.NewRenderer(app)
	ctx := context.Background()

	res, err := renderer.GenerateRevision(ctx, stack, repo, workDir, "docker-compose.yml", nil, "commitX", false, "", "embedded", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	contentStr := readRenderedFile(t, renderer, stack.Id, res.Version)

	// Identity labels must win
	if !contains(contentStr, `dev.wireops.managed: "true"`) {
		t.Errorf("expected dev.wireops.managed to be 'true', got something else:\n%s", contentStr)
	}
	if contains(contentStr, `dev.wireops.stack_id: spoofed-id`) {
		t.Errorf("spoofed dev.wireops.stack_id must be stripped")
	}

	// Annotations must also be scrubbed
	if contains(contentStr, `dev.wireops.checksum: fake`) {
		t.Errorf("spoofed dev.wireops.checksum in annotations must be stripped")
	}
	if !contains(contentStr, `user.note: safe`) {
		t.Errorf("expected user.note annotation to be preserved")
	}

	// Normal user label must be preserved.
	if !contains(contentStr, `user.safe.label: keep-me`) {
		t.Errorf("expected user.safe.label to be preserved:\n%s", contentStr)
	}
}

func TestRendererNoSecrets(t *testing.T) {
	app, workDir, composePath := setupRendererTest(t)
	composeContent := `
name: secret_stack
services:
  web:
    image: nginx:latest
    environment:
      - MY_SECRET=${MY_SECRET}
      - ANOTHER_VAR=${ANOTHER_VAR:-default_val}
`
	writeTestComposeFile(t, composePath, composeContent)

	repo := createTestRepo(t, app, "Secret Repo", "main")
	stack := createTestStack(t, app, repo.Id, "secret_stack")

	renderer := sync.NewRenderer(app)
	ctx := context.Background()

	// Env vars containing sensible data
	envVars := []string{
		"MY_SECRET=super_secret_value",
		"ANOTHER_VAR=my_override",
	}

	res, err := renderer.GenerateRevision(ctx, stack, repo, workDir, "docker-compose.yml", envVars, "commit123", false, "", "embedded", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	contentStr := readRenderedFile(t, renderer, stack.Id, res.Version)

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

func TestRendererInjectsConfigContentFromAnnotation(t *testing.T) {
	app, workDir, composePath := setupRendererTest(t)
	// User's raw compose file never declares `configs:` itself — just an
	// annotation mapping "<source path in repo>:<in-container target>".
	// No wireops.yaml involvement at all: source is resolved straight from
	// workDir (the repo checkout).
	composeContent := `
name: config_stack
services:
  web:
    image: nginx:latest
    annotations:
      dev.wireops.config.nginx-conf: files/nginx.conf:/etc/nginx/nginx.conf
`
	writeTestComposeFile(t, composePath, composeContent)
	writeSourceFile(t, workDir, "files/nginx.conf", "server {}\n")

	repo := createTestRepo(t, app, "Config Repo", "main")
	stack := createTestStack(t, app, repo.Id, "config_stack")

	renderer := sync.NewRenderer(app)
	ctx := context.Background()

	res, err := renderer.GenerateRevision(ctx, stack, repo, workDir, "docker-compose.yml", nil, "commitA", false, "", "embedded", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.ConfigFiles) != 1 || res.ConfigFiles[0].Target != "/etc/nginx/nginx.conf" {
		t.Fatalf("expected one tracked config with target /etc/nginx/nginx.conf, got %+v", res.ConfigFiles)
	}

	contentStr := readRenderedFile(t, renderer, stack.Id, res.Version)
	if !contains(contentStr, "server {}") {
		t.Errorf("expected resolved config content to be embedded, got:\n%s", contentStr)
	}
	if !contains(contentStr, "target: /etc/nginx/nginx.conf") {
		t.Errorf("expected synthesized service-level configs: entry, got:\n%s", contentStr)
	}
	if contains(contentStr, "dev.wireops.config.nginx-conf") {
		t.Errorf("expected directive annotation to be stripped from rendered output, got:\n%s", contentStr)
	}
}

func TestRendererWarnsOnUnescapedConfigInterpolation(t *testing.T) {
	app, workDir, composePath := setupRendererTest(t)
	composeContent := `
name: config_warn_stack
services:
  web:
    image: nginx:latest
    annotations:
      dev.wireops.config.nginx-conf: files/nginx.conf:/etc/nginx/nginx.conf
`
	writeTestComposeFile(t, composePath, composeContent)
	// $uri is meant for nginx's own template engine at runtime, not docker
	// compose — left bare like this, `docker compose config` interpolates it
	// away before the container ever starts.
	writeSourceFile(t, workDir, "files/nginx.conf", "location / { try_files $uri /index.html; }\n")

	repo := createTestRepo(t, app, "Config Warn Repo", "main")
	stack := createTestStack(t, app, repo.Id, "config_warn_stack")

	renderer := sync.NewRenderer(app)
	ctx := context.Background()

	res, err := renderer.GenerateRevision(ctx, stack, repo, workDir, "docker-compose.yml", nil, "commitA", false, "", "embedded", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var match string
	for _, w := range res.Warnings {
		if contains(w, "compose/unescaped-config-interpolation") {
			match = w
			break
		}
	}
	if match == "" {
		t.Fatalf("expected a compose/unescaped-config-interpolation warning, got %+v", res.Warnings)
	}
	if !contains(match, "uri") {
		t.Errorf("expected warning to mention the unescaped variable, got %q", match)
	}
}

func TestRendererNoWarningsWhenConfigContentHasNoInterpolation(t *testing.T) {
	app, workDir, composePath := setupRendererTest(t)
	composeContent := `
name: config_nowarn_stack
services:
  web:
    image: nginx:latest
    annotations:
      dev.wireops.config.nginx-conf: files/nginx.conf:/etc/nginx/nginx.conf
`
	writeTestComposeFile(t, composePath, composeContent)
	writeSourceFile(t, workDir, "files/nginx.conf", "server {}\n")

	repo := createTestRepo(t, app, "Config No Warn Repo", "main")
	stack := createTestStack(t, app, repo.Id, "config_nowarn_stack")

	renderer := sync.NewRenderer(app)
	ctx := context.Background()

	res, err := renderer.GenerateRevision(ctx, stack, repo, workDir, "docker-compose.yml", nil, "commitA", false, "", "embedded", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, w := range res.Warnings {
		if contains(w, "compose/unescaped-config-interpolation") {
			t.Fatalf("expected no unescaped-config-interpolation warning, got %+v", res.Warnings)
		}
	}
}

func TestRendererConfigAnnotationOnMultipleServices(t *testing.T) {
	app, workDir, composePath := setupRendererTest(t)
	// Both services reference the same source file but mount it at different
	// targets — content is shared, each service controls its own mount path.
	composeContent := `
name: config_multi_stack
services:
  web:
    image: nginx:latest
    annotations:
      dev.wireops.config.app-cert: files/cert.pem:/etc/ssl/web-cert.pem
  api:
    image: alpine:latest
    annotations:
      dev.wireops.config.app-cert: files/cert.pem:/etc/ssl/api-cert.pem
`
	writeTestComposeFile(t, composePath, composeContent)
	writeSourceFile(t, workDir, "files/cert.pem", "CERT")

	repo := createTestRepo(t, app, "Config Multi Repo", "main")
	stack := createTestStack(t, app, repo.Id, "config_multi_stack")

	renderer := sync.NewRenderer(app)
	ctx := context.Background()

	res, err := renderer.GenerateRevision(ctx, stack, repo, workDir, "docker-compose.yml", nil, "commitA", false, "", "embedded", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.ConfigFiles) != 2 {
		t.Fatalf("expected 2 tracked configs (one per service), got %d: %+v", len(res.ConfigFiles), res.ConfigFiles)
	}
	// web and api both annotate the friendly name "app-cert" for unrelated
	// mount points — TrackedFile.Name must be service-qualified (not the
	// bare annotation name) or the two rows collide on stack_config_files'
	// (stack, name) unique index and the tracking upsert fails outright.
	if res.ConfigFiles[0].Name == res.ConfigFiles[1].Name {
		t.Errorf("expected distinct tracked config names for web/api sharing the app-cert annotation, got both %q", res.ConfigFiles[0].Name)
	}

	contentStr := readRenderedFile(t, renderer, stack.Id, res.Version)
	if !contains(contentStr, "target: /etc/ssl/web-cert.pem") {
		t.Errorf("expected web target in rendered output, got:\n%s", contentStr)
	}
	if !contains(contentStr, "target: /etc/ssl/api-cert.pem") {
		t.Errorf("expected api target in rendered output, got:\n%s", contentStr)
	}
}

func TestRendererConfigAnnotationMissingSourceErrors(t *testing.T) {
	app, workDir, composePath := setupRendererTest(t)
	composeContent := `
name: config_missing_stack
services:
  web:
    image: nginx:latest
    annotations:
      dev.wireops.config.missing: files/does-not-exist.conf:/etc/nginx/nginx.conf
`
	writeTestComposeFile(t, composePath, composeContent)

	repo := createTestRepo(t, app, "Config Missing Repo", "main")
	stack := createTestStack(t, app, repo.Id, "config_missing_stack")

	renderer := sync.NewRenderer(app)
	ctx := context.Background()

	_, err := renderer.GenerateRevision(ctx, stack, repo, workDir, "docker-compose.yml", nil, "commitA", false, "", "embedded", nil)
	if err == nil {
		t.Fatal("expected error for annotation referencing a source file that doesn't exist")
	}
}

func TestRendererConfigAnnotationMalformedValueErrors(t *testing.T) {
	app, workDir, composePath := setupRendererTest(t)
	composeContent := `
name: config_malformed_stack
services:
  web:
    image: nginx:latest
    annotations:
      dev.wireops.config.bad: no-colon-here
`
	writeTestComposeFile(t, composePath, composeContent)

	repo := createTestRepo(t, app, "Config Malformed Repo", "main")
	stack := createTestStack(t, app, repo.Id, "config_malformed_stack")

	renderer := sync.NewRenderer(app)
	ctx := context.Background()

	_, err := renderer.GenerateRevision(ctx, stack, repo, workDir, "docker-compose.yml", nil, "commitA", false, "", "embedded", nil)
	if err == nil {
		t.Fatal("expected error for malformed annotation value")
	}
}

func TestRendererUnannotatedServiceIsIgnored(t *testing.T) {
	app, workDir, composePath := setupRendererTest(t)
	composeContent := `
name: config_unused_stack
services:
  web:
    image: nginx:latest
`
	writeTestComposeFile(t, composePath, composeContent)

	repo := createTestRepo(t, app, "Config Unused Repo", "main")
	stack := createTestStack(t, app, repo.Id, "config_unused_stack")

	renderer := sync.NewRenderer(app)
	ctx := context.Background()

	res, err := renderer.GenerateRevision(ctx, stack, repo, workDir, "docker-compose.yml", nil, "commitA", false, "", "embedded", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.ConfigFiles) != 0 {
		t.Errorf("expected no tracked configs, got %v", res.ConfigFiles)
	}
}

func TestRendererConfigAnnotationDirectoryExpandsRecursively(t *testing.T) {
	app, workDir, composePath := setupRendererTest(t)
	composeContent := `
name: config_dir_stack
services:
  web:
    image: nginx:latest
    annotations:
      dev.wireops.config.confd: files/confd:/etc/app/conf.d
`
	writeTestComposeFile(t, composePath, composeContent)
	writeSourceFile(t, workDir, "files/confd/a.conf", "A")
	writeSourceFile(t, workDir, "files/confd/nested/b.conf", "B")

	repo := createTestRepo(t, app, "Config Dir Repo", "main")
	stack := createTestStack(t, app, repo.Id, "config_dir_stack")

	renderer := sync.NewRenderer(app)
	ctx := context.Background()

	res, err := renderer.GenerateRevision(ctx, stack, repo, workDir, "docker-compose.yml", nil, "commitA", false, "", "embedded", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.ConfigFiles) != 2 {
		t.Fatalf("expected 2 tracked configs from directory expansion, got %d: %+v", len(res.ConfigFiles), res.ConfigFiles)
	}

	contentStr := readRenderedFile(t, renderer, stack.Id, res.Version)
	if !contains(contentStr, "target: /etc/app/conf.d/a.conf") {
		t.Errorf("expected a.conf target in rendered output, got:\n%s", contentStr)
	}
	if !contains(contentStr, "target: /etc/app/conf.d/nested/b.conf") {
		t.Errorf("expected nested/b.conf target in rendered output, got:\n%s", contentStr)
	}
	if !contains(contentStr, "content: A") {
		t.Errorf("expected a.conf content in rendered output, got:\n%s", contentStr)
	}
	if !contains(contentStr, "content: B") {
		t.Errorf("expected b.conf content in rendered output, got:\n%s", contentStr)
	}
}

func TestRendererConfigContentChangeTriggersVersionBump(t *testing.T) {
	app, workDir, composePath := setupRendererTest(t)
	composeContent := `
name: config_bump_stack
services:
  web:
    image: nginx:latest
    annotations:
      dev.wireops.config.nginx-conf: files/nginx.conf:/etc/nginx/nginx.conf
`
	writeTestComposeFile(t, composePath, composeContent)
	writeSourceFile(t, workDir, "files/nginx.conf", "v1")

	repo := createTestRepo(t, app, "Config Bump Repo", "main")
	stack := createTestStack(t, app, repo.Id, "config_bump_stack")

	renderer := sync.NewRenderer(app)
	ctx := context.Background()

	res1, err := renderer.GenerateRevision(ctx, stack, repo, workDir, "docker-compose.yml", nil, "commitA", false, "", "embedded", nil)
	if err != nil {
		t.Fatalf("unexpected error on first render: %v", err)
	}
	if res1.Version != 1 {
		t.Fatalf("expected version 1, got %d", res1.Version)
	}

	stack, _ = app.FindRecordById("stacks", stack.Id)
	writeSourceFile(t, workDir, "files/nginx.conf", "v2")

	res2, err := renderer.GenerateRevision(ctx, stack, repo, workDir, "docker-compose.yml", nil, "commitA", false, "", "embedded", nil)
	if err != nil {
		t.Fatalf("unexpected error on second render: %v", err)
	}
	if res2.Version != 2 {
		t.Errorf("expected config content change to bump version to 2, got %d", res2.Version)
	}
	if res2.Checksum == res1.Checksum {
		t.Errorf("expected checksum to change when config content changes")
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

func setupRendererTest(t *testing.T) (core.App, string, string) {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf(errCreateTestApp, err)
	}
	t.Cleanup(func() { app.Cleanup() })
	createTestCollections(t, app)
	workDir := t.TempDir()
	return app, workDir, filepath.Join(workDir, "docker-compose.yml")
}

func writeTestComposeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf(errWriteComposeFile, err)
	}
}

// writeSourceFile writes content to relPath under workDir, creating parent
// directories as needed — used to simulate a git-committed config source
// file that a dev.wireops.config.* annotation points at.
func writeSourceFile(t *testing.T, workDir, relPath, content string) {
	t.Helper()
	full := filepath.Join(workDir, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatalf("failed to create source file dir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}
}

func readRenderedFile(t *testing.T, renderer *sync.Renderer, stackId string, version int) string {
	t.Helper()
	renderedFile := renderer.GetRevisionFilePath(stackId, version)
	content, err := os.ReadFile(renderedFile)
	if err != nil {
		t.Fatalf("failed to read rendered file: %v", err)
	}
	return string(content)
}

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

	// Worker Policies
	policies := core.NewBaseCollection("worker_policies")
	policies.Fields.Add(&core.BoolField{Name: "enabled"})
	policies.Fields.Add(&core.JSONField{Name: "allowed_volumes"})
	policies.Fields.Add(&core.JSONField{Name: "allowed_networks"})
	policies.Fields.Add(&core.JSONField{Name: "allowed_images"})
	policies.Fields.Add(&core.BoolField{Name: "prevent_latest_images"})
	policies.Fields.Add(&core.BoolField{Name: "block_host_volumes"})
	policies.Fields.Add(&core.BoolField{Name: "block_privileged"})
	policies.Fields.Add(&core.BoolField{Name: "block_host_network"})
	policies.Fields.Add(&core.BoolField{Name: "block_host_pid"})
	policies.Fields.Add(&core.BoolField{Name: "block_host_ipc"})
	policies.Fields.Add(&core.BoolField{Name: "block_docker_socket"})
	policies.Fields.Add(&core.JSONField{Name: "allowed_cap_add"})
	policies.Fields.Add(&core.JSONField{Name: "allowed_devices"})
	policies.Fields.Add(&core.JSONField{Name: "allowed_security_opt"})
	policies.Fields.Add(&core.BoolField{Name: "allow_render_overrides"})
	if err := app.Save(policies); err != nil {
		t.Fatalf("failed to create worker_policies collection: %v", err)
	}

	// Workers
	workers := core.NewBaseCollection("workers")
	workers.Fields.Add(&core.TextField{Name: "hostname"})
	workers.Fields.Add(&core.TextField{Name: "fingerprint"})
	workers.Fields.Add(&core.BoolField{Name: "policy_inherit"})
	workers.Fields.Add(&core.JSONField{Name: "policy_flags"})
	workers.Fields.Add(&core.JSONField{Name: "policy_volumes"})
	workers.Fields.Add(&core.JSONField{Name: "policy_networks"})
	workers.Fields.Add(&core.JSONField{Name: "policy_images"})
	workers.Fields.Add(&core.JSONField{Name: "policy_cap_add"})
	workers.Fields.Add(&core.JSONField{Name: "policy_devices"})
	workers.Fields.Add(&core.JSONField{Name: "policy_security_opt"})
	if err := app.Save(workers); err != nil {
		t.Fatalf("failed to create workers collection: %v", err)
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

func TestRendererListStyleLabels(t *testing.T) {
	app, workDir, composePath := setupRendererTest(t)
	// Use list format for labels
	composeContent := `
name: list_labels_stack
services:
  web:
    image: nginx:latest
    labels:
      - "traefik.http.routers.web.rule=Host(` + "`" + `example.com` + "`" + `)"
      - "user.safe=value"
`
	writeTestComposeFile(t, composePath, composeContent)

	repo := createTestRepo(t, app, "List Labels Repo", "main")
	stack := createTestStack(t, app, repo.Id, "list_labels_stack")

	renderer := sync.NewRenderer(app)
	ctx := context.Background()

	res, err := renderer.GenerateRevision(ctx, stack, repo, workDir, "docker-compose.yml", nil, "commitL", false, "", "embedded", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	contentStr := readRenderedFile(t, renderer, stack.Id, res.Version)

	// Verify Traefik label is preserved (marshaled as map in the output)
	if !contains(contentStr, "traefik.http.routers.web.rule: Host(`example.com`)") {
		t.Errorf("traefik label lost or incorrectly marshaled:\n%s", contentStr)
	}
	if !contains(contentStr, "user.safe: value") {
		t.Errorf("user label lost:\n%s", contentStr)
	}
}

func TestRendererPolicyRejection(t *testing.T) {
	app, workDir, composePath := setupRendererTest(t)

	// Enable policy that blocks latest tags, host volumes, and restricts networks/images
	col, _ := app.FindCollectionByNameOrId("worker_policies")
	globalPolicy := core.NewRecord(col)
	globalPolicy.Set("enabled", true)
	globalPolicy.Set("prevent_latest_images", true)
	globalPolicy.Set("block_host_volumes", true)
	globalPolicy.Set("block_privileged", true)
	globalPolicy.Set("allowed_networks", `["safe_net"]`)
	globalPolicy.Set("allowed_images", `["nginx:alpine", "postgres:15"]`)
	if err := app.Save(globalPolicy); err != nil {
		t.Fatalf("failed to save global policy: %v", err)
	}

	tests := []struct {
		name    string
		compose string
	}{
		{
			name: "violates latest image",
			compose: `
name: policy_stack
services:
  web:
    image: nginx:latest
`,
		},
		{
			name: "violates allowed images",
			compose: `
name: policy_stack
services:
  web:
    image: mysql:8
`,
		},
		{
			name: "violates host volume",
			compose: `
name: policy_stack
services:
  web:
    image: nginx:alpine
    volumes:
      - /etc/shadow:/etc/shadow
`,
		},
		{
			name: "violates allowed networks",
			compose: `
name: policy_stack
services:
  web:
    image: nginx:alpine
    networks:
      - malicious_net
`,
		},
		{
			name: "violates privileged mode",
			compose: `
name: policy_stack
services:
  web:
    image: nginx:alpine
    privileged: true
`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			writeTestComposeFile(t, composePath, tc.compose)

			repo := createTestRepo(t, app, tc.name, "main")
			stack := createTestStack(t, app, repo.Id, "policy_stack")

			renderer := sync.NewRenderer(app)
			ctx := context.Background()

			// Generation should fail due to policy violation
			_, err := renderer.GenerateRevision(ctx, stack, repo, workDir, "docker-compose.yml", nil, "commitA", false, "", "embedded", nil)
			if err == nil {
				t.Fatalf("expected error due to policy violation, got nil")
			}

			if !contains(err.Error(), "policy violation") {
				t.Errorf("expected policy violation error, got: %v", err)
			}
		})
	}

	t.Run("worker specific override path", func(t *testing.T) {
		// Create a worker record to exercise the worker override path
		workersCol, _ := app.FindCollectionByNameOrId("workers")
		worker := core.NewRecord(workersCol)
		worker.Id = "workeroverride1"
		worker.Set("hostname", "worker-1")
		worker.Set("policy_inherit", true)
		if err := app.Save(worker); err != nil {
			t.Fatalf("failed to save worker: %v", err)
		}

		compose := `
name: policy_stack
services:
  web:
    image: nginx:latest
`
		writeTestComposeFile(t, composePath, compose)

		repo := createTestRepo(t, app, "worker-override-test", "main")
		stack := createTestStack(t, app, repo.Id, "policy_stack")

		renderer := sync.NewRenderer(app)
		ctx := context.Background()

		// Generation should fail due to policy violation on the specific worker path
		_, err := renderer.GenerateRevision(ctx, stack, repo, workDir, "docker-compose.yml", nil, "commitA", false, "workeroverride1", "embedded", nil)
		if err == nil {
			t.Fatalf("expected error due to policy violation, got nil")
		}

		if !contains(err.Error(), "policy violation") {
			t.Errorf("expected policy violation error, got: %v", err)
		}
	})
}

func TestRendererRenderOverridesBlockedByDefault(t *testing.T) {
	app, workDir, composePath := setupRendererTest(t)
	writeTestComposeFile(t, composePath, `
name: overrides_stack
services:
  web:
    image: nginx:alpine
`)

	repo := createTestRepo(t, app, "overrides-blocked", "main")
	stack := createTestStack(t, app, repo.Id, "overrides_stack")

	renderer := sync.NewRenderer(app)
	ctx := context.Background()

	overrides := map[string]sync.ServiceOverride{
		"web": {Image: "nginx:test"},
	}

	_, err := renderer.GenerateRevision(ctx, stack, repo, workDir, "docker-compose.yml", nil, "commitA", false, "", "embedded", overrides)
	if err == nil {
		t.Fatalf("expected error because render overrides are disabled by default, got nil")
	}
	if !contains(err.Error(), "disabled by the worker policy") {
		t.Errorf("expected worker-policy error, got: %v", err)
	}
}

// AllowRenderOverrides gates this capability on its own: disabling the rest of the
// worker policy's allowlist/Block* enforcement must not implicitly grant override
// capability too, since that would let anyone bypass image/network allowlists by
// disabling policy for an unrelated reason.
func TestRendererRenderOverridesStillBlockedWhenPolicyDisabled(t *testing.T) {
	app, workDir, composePath := setupRendererTest(t)
	writeTestComposeFile(t, composePath, `
name: overrides_stack
services:
  web:
    image: nginx:alpine
`)

	col, _ := app.FindCollectionByNameOrId("worker_policies")
	globalPolicy := core.NewRecord(col)
	globalPolicy.Set("enabled", false) // Disabled = true; allow_render_overrides left unset/false
	if err := app.Save(globalPolicy); err != nil {
		t.Fatalf("failed to save global policy: %v", err)
	}

	repo := createTestRepo(t, app, "overrides-policy-disabled", "main")
	stack := createTestStack(t, app, repo.Id, "overrides_stack")

	renderer := sync.NewRenderer(app)
	ctx := context.Background()

	overrides := map[string]sync.ServiceOverride{
		"web": {Image: "nginx:test"},
	}

	_, err := renderer.GenerateRevision(ctx, stack, repo, workDir, "docker-compose.yml", nil, "commitA", false, "", "embedded", overrides)
	if err == nil {
		t.Fatal("expected error: render overrides must stay blocked when AllowRenderOverrides is unset, even with the rest of policy disabled")
	}
	if !contains(err.Error(), "render-time overrides are disabled by the worker policy") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRendererRenderOverridesAppliedWhenAllowed(t *testing.T) {
	app, workDir, composePath := setupRendererTest(t)
	writeTestComposeFile(t, composePath, `
name: overrides_stack
services:
  web:
    image: nginx:alpine
    ports:
      - "8080:80"
`)

	col, _ := app.FindCollectionByNameOrId("worker_policies")
	globalPolicy := core.NewRecord(col)
	globalPolicy.Set("enabled", true)
	globalPolicy.Set("allow_render_overrides", true)
	if err := app.Save(globalPolicy); err != nil {
		t.Fatalf("failed to save global policy: %v", err)
	}

	repo := createTestRepo(t, app, "overrides-allowed", "main")
	stack := createTestStack(t, app, repo.Id, "overrides_stack")

	renderer := sync.NewRenderer(app)
	ctx := context.Background()

	overrides := map[string]sync.ServiceOverride{
		"web": {Image: "nginx:test", Ports: []string{"8081:80"}, Networks: []string{"proxy"}},
	}

	res, err := renderer.GenerateRevision(ctx, stack, repo, workDir, "docker-compose.yml", nil, "commitA", false, "", "embedded", overrides)
	if err != nil {
		t.Fatalf("unexpected error applying overrides: %v", err)
	}

	contentStr := readRenderedFile(t, renderer, stack.Id, res.Version)
	if !contains(contentStr, "nginx:test") {
		t.Errorf("expected overridden image in rendered compose, got:\n%s", contentStr)
	}
	if !contains(contentStr, "8081:80") {
		t.Errorf("expected overridden port in rendered compose, got:\n%s", contentStr)
	}
	if !contains(contentStr, "proxy") {
		t.Errorf("expected overridden network in rendered compose, got:\n%s", contentStr)
	}
	if !contains(contentStr, "external: true") {
		t.Errorf("expected override network 'proxy' to be declared external at the top level, got:\n%s", contentStr)
	}
}

// A network already declared at the top level (e.g. by the compose file itself) must
// not be clobbered by ApplyServiceOverrides — only names missing from the top-level
// networks block get an external:true entry added.
func TestRendererRenderOverridesReusesAlreadyDeclaredNetwork(t *testing.T) {
	app, workDir, composePath := setupRendererTest(t)
	writeTestComposeFile(t, composePath, `
name: overrides_stack
networks:
  proxy:
    driver: bridge
services:
  web:
    image: nginx:alpine
    networks:
      - proxy
`)

	col, _ := app.FindCollectionByNameOrId("worker_policies")
	globalPolicy := core.NewRecord(col)
	globalPolicy.Set("enabled", true)
	globalPolicy.Set("allow_render_overrides", true)
	if err := app.Save(globalPolicy); err != nil {
		t.Fatalf("failed to save global policy: %v", err)
	}

	repo := createTestRepo(t, app, "overrides-existing-network", "main")
	stack := createTestStack(t, app, repo.Id, "overrides_stack")

	renderer := sync.NewRenderer(app)
	ctx := context.Background()

	overrides := map[string]sync.ServiceOverride{
		"web": {Networks: []string{"proxy"}},
	}

	res, err := renderer.GenerateRevision(ctx, stack, repo, workDir, "docker-compose.yml", nil, "commitA", false, "", "embedded", overrides)
	if err != nil {
		t.Fatalf("unexpected error applying overrides: %v", err)
	}

	contentStr := readRenderedFile(t, renderer, stack.Id, res.Version)
	if contains(contentStr, "external: true") {
		t.Errorf("expected already-declared network 'proxy' to keep its own definition, not be marked external:\n%s", contentStr)
	}
	if !contains(contentStr, "driver: bridge") {
		t.Errorf("expected original network declaration to survive, got:\n%s", contentStr)
	}
}

func TestApplyServiceOverridesScale(t *testing.T) {
	scale := 3
	config := map[string]interface{}{
		"services": map[string]interface{}{
			"web": map[string]interface{}{
				"image": "nginx:alpine",
				"deploy": map[string]interface{}{
					"mode":     "replicated",
					"replicas": 1,
				},
			},
		},
	}

	if err := sync.ApplyServiceOverrides(config, map[string]sync.ServiceOverride{
		"web": {Scale: &scale},
	}); err != nil {
		t.Fatalf("apply scale override: %v", err)
	}

	web := config["services"].(map[string]interface{})["web"].(map[string]interface{})
	if got := web["scale"]; got != scale {
		t.Errorf("scale = %#v, want %d", got, scale)
	}
	deploy := web["deploy"].(map[string]interface{})
	if got := deploy["replicas"]; got != scale {
		t.Errorf("deploy.replicas = %#v, want %d", got, scale)
	}
	if got := deploy["mode"]; got != "replicated" {
		t.Errorf("deploy.mode = %#v, want replicated", got)
	}
}

func TestApplyServiceOverridesScaleAllowsZero(t *testing.T) {
	scale := 0
	config := map[string]interface{}{
		"services": map[string]interface{}{
			"worker": map[string]interface{}{"image": "busybox"},
		},
	}

	if err := sync.ApplyServiceOverrides(config, map[string]sync.ServiceOverride{
		"worker": {Scale: &scale},
	}); err != nil {
		t.Fatalf("apply zero scale override: %v", err)
	}

	worker := config["services"].(map[string]interface{})["worker"].(map[string]interface{})
	if got := worker["scale"]; got != 0 {
		t.Errorf("scale = %#v, want 0", got)
	}
}

func TestApplyServiceOverridesScaleRejectsUnsupportedServices(t *testing.T) {
	three := 3
	negative := -1
	tooLarge := 101
	cases := []struct {
		name    string
		service map[string]interface{}
		scale   *int
		want    string
	}{
		{
			name:    "custom container name",
			service: map[string]interface{}{"image": "nginx", "container_name": "web"},
			scale:   &three,
			want:    "container_name",
		},
		{
			name: "fixed published port",
			service: map[string]interface{}{
				"image": "nginx",
				"ports": []interface{}{map[string]interface{}{"published": "8080", "target": float64(80)}},
			},
			scale: &three,
			want:  "fixed host ports",
		},
		{
			name:    "negative scale",
			service: map[string]interface{}{"image": "nginx"},
			scale:   &negative,
			want:    "between 0 and 100",
		},
		{
			name:    "unbounded scale",
			service: map[string]interface{}{"image": "nginx"},
			scale:   &tooLarge,
			want:    "between 0 and 100",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			config := map[string]interface{}{
				"services": map[string]interface{}{"web": tc.service},
			}
			err := sync.ApplyServiceOverrides(config, map[string]sync.ServiceOverride{
				"web": {Scale: tc.scale},
			})
			if err == nil || !contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want message containing %q", err, tc.want)
			}
		})
	}
}

func TestRenderOverridesRequireContainerRecreate(t *testing.T) {
	scale := 2
	if sync.RenderOverridesRequireContainerRecreate(map[string]sync.ServiceOverride{"web": {Scale: &scale}}) {
		t.Fatal("scale-only override must not force container recreation")
	}
	if !sync.RenderOverridesRequireContainerRecreate(map[string]sync.ServiceOverride{"web": {Image: "nginx:test", Scale: &scale}}) {
		t.Fatal("image override must force container recreation")
	}
}

func TestRendererRenderOverridesUnknownServiceErrors(t *testing.T) {
	app, workDir, composePath := setupRendererTest(t)
	writeTestComposeFile(t, composePath, `
name: overrides_stack
services:
  web:
    image: nginx:alpine
`)

	col, _ := app.FindCollectionByNameOrId("worker_policies")
	globalPolicy := core.NewRecord(col)
	globalPolicy.Set("enabled", true)
	globalPolicy.Set("allow_render_overrides", true)
	if err := app.Save(globalPolicy); err != nil {
		t.Fatalf("failed to save global policy: %v", err)
	}

	repo := createTestRepo(t, app, "overrides-unknown-service", "main")
	stack := createTestStack(t, app, repo.Id, "overrides_stack")

	renderer := sync.NewRenderer(app)
	ctx := context.Background()

	overrides := map[string]sync.ServiceOverride{
		"does-not-exist": {Image: "nginx:test"},
	}

	_, err := renderer.GenerateRevision(ctx, stack, repo, workDir, "docker-compose.yml", nil, "commitA", false, "", "embedded", overrides)
	if err == nil {
		t.Fatalf("expected error for unknown service override, got nil")
	}
	if !contains(err.Error(), "unknown service") {
		t.Errorf("expected unknown-service error, got: %v", err)
	}
	// Reconciler.ReconcileStack/reconcileLocalStack use errors.Is against this sentinel
	// to decide whether to auto-clear a stale override and self-heal.
	if !errors.Is(err, sync.ErrUnknownOverrideService) {
		t.Errorf("expected error to wrap sync.ErrUnknownOverrideService, got: %v", err)
	}
}
