package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/pocketbase/pocketbase/tools/router"

	_ "github.com/wireops/wireops/pb_migrations"

	"github.com/wireops/wireops/internal/config"
	"github.com/wireops/wireops/internal/crypto"
	"github.com/wireops/wireops/internal/git"
	"github.com/wireops/wireops/internal/hooks"
	"github.com/wireops/wireops/internal/logstream"
	"github.com/wireops/wireops/internal/protocol"
	"github.com/wireops/wireops/internal/secrets"
	wiresync "github.com/wireops/wireops/internal/sync"
	"github.com/wireops/wireops/internal/testutil"
)

// requireDocker skips a test when the docker CLI is unavailable — only the
// preview route's end-to-end path shells out to `docker compose config`;
// everything else in this file is a pure function or a local-git-only route
// test, per the layering described on buildMigratePreview's doc comment.
func requireDocker(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker CLI not available")
	}
}

// --- pure function tests (no app, no I/O beyond t.TempDir) ---

func TestExtractComposeResourceSets(t *testing.T) {
	configMap := map[string]interface{}{
		"services": map[string]interface{}{
			"web": map[string]interface{}{},
			"api": map[string]interface{}{},
		},
		"volumes": map[string]interface{}{
			"data": map[string]interface{}{},
		},
		"networks": map[string]interface{}{
			"net1": map[string]interface{}{},
		},
	}
	services, volumes, networks := extractComposeResourceSets(configMap)
	if len(services) != 2 || services[0] != "api" || services[1] != "web" {
		t.Fatalf("unexpected services: %v", services)
	}
	if len(volumes) != 1 || volumes[0] != "data" {
		t.Fatalf("unexpected volumes: %v", volumes)
	}
	if len(networks) != 1 || networks[0] != "net1" {
		t.Fatalf("unexpected networks: %v", networks)
	}
}

func TestExtractComposeResourceSetsMissingKeys(t *testing.T) {
	services, volumes, networks := extractComposeResourceSets(map[string]interface{}{})
	if services != nil || volumes != nil || networks != nil {
		t.Fatalf("expected nil sets for an empty config map, got services=%v volumes=%v networks=%v", services, volumes, networks)
	}
}

func TestBuildMigratePreviewVolumeRemovedIsCritical(t *testing.T) {
	source := map[string]interface{}{
		"name":     "myapp",
		"services": map[string]interface{}{"web": map[string]interface{}{}},
		"volumes":  map[string]interface{}{"data": map[string]interface{}{}},
	}
	target := map[string]interface{}{
		"name":     "myapp",
		"services": map[string]interface{}{"web": map[string]interface{}{}},
	}

	preview := buildMigratePreview("source-repo", "target-repo", source, target, SopsCheck{Status: "none"})

	if len(preview.Volumes.Removed) != 1 || preview.Volumes.Removed[0] != "data" {
		t.Fatalf("expected volume 'data' to be reported removed, got %+v", preview.Volumes)
	}
	if !preview.ProjectName.Same {
		t.Fatalf("expected project names to match, got %+v", preview.ProjectName)
	}

	var found bool
	for _, w := range preview.Warnings {
		if w.Code == "volume_removed" && w.Severity == "critical" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a critical volume_removed warning, got %+v", preview.Warnings)
	}
}

func TestBuildMigratePreviewServiceRenameOrphanWarning(t *testing.T) {
	source := map[string]interface{}{
		"name":     "myapp",
		"services": map[string]interface{}{"web": map[string]interface{}{}},
	}
	target := map[string]interface{}{
		"name":     "myapp",
		"services": map[string]interface{}{"web-v2": map[string]interface{}{}},
	}

	preview := buildMigratePreview("source-repo", "target-repo", source, target, SopsCheck{Status: "none"})

	var removedWarn, addedInfo bool
	for _, w := range preview.Warnings {
		if w.Code == "service_removed" && w.Severity == "warn" {
			removedWarn = true
		}
		if w.Code == "service_added" && w.Severity == "info" {
			addedInfo = true
		}
	}
	if !removedWarn {
		t.Fatalf("expected a warn-severity service_removed warning, got %+v", preview.Warnings)
	}
	if !addedInfo {
		t.Fatalf("expected an info-severity service_added warning, got %+v", preview.Warnings)
	}
}

// TestBuildMigratePreviewVolumeAddedAndNetworkDiffWarnings covers the
// remaining info-severity migrateWarnings branches: a new named volume, a
// removed network, and a new network — none of which the other tests here
// happen to exercise.
func TestBuildMigratePreviewVolumeAddedAndNetworkDiffWarnings(t *testing.T) {
	source := map[string]interface{}{
		"name":     "myapp",
		"services": map[string]interface{}{},
		"networks": map[string]interface{}{"old-net": map[string]interface{}{}},
	}
	target := map[string]interface{}{
		"name":     "myapp",
		"services": map[string]interface{}{},
		"volumes":  map[string]interface{}{"new-data": map[string]interface{}{}},
		"networks": map[string]interface{}{"new-net": map[string]interface{}{}},
	}

	preview := buildMigratePreview("source-repo", "target-repo", source, target, SopsCheck{Status: "none"})

	var volumeAdded, networkRemoved, networkAdded bool
	for _, w := range preview.Warnings {
		switch w.Code {
		case "volume_added":
			volumeAdded = true
		case "network_removed":
			networkRemoved = true
		case "network_added":
			networkAdded = true
		}
	}
	if !volumeAdded {
		t.Fatalf("expected a volume_added warning, got %+v", preview.Warnings)
	}
	if !networkRemoved {
		t.Fatalf("expected a network_removed warning, got %+v", preview.Warnings)
	}
	if !networkAdded {
		t.Fatalf("expected a network_added warning, got %+v", preview.Warnings)
	}
}

func TestBuildMigratePreviewProjectNameChangedWarning(t *testing.T) {
	source := map[string]interface{}{"name": "old-name", "services": map[string]interface{}{}}
	target := map[string]interface{}{"name": "new-name", "services": map[string]interface{}{}}

	preview := buildMigratePreview("source-repo", "target-repo", source, target, SopsCheck{Status: "none"})

	if preview.ProjectName.Same {
		t.Fatal("expected project names to differ")
	}
	var found bool
	for _, w := range preview.Warnings {
		if w.Code == "project_name_changed" && w.Severity == "warn" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a project_name_changed warning, got %+v", preview.Warnings)
	}
}

func portMapConfig(name, publishedPort string) map[string]interface{} {
	return map[string]interface{}{
		"name": name,
		"services": map[string]interface{}{
			"web": map[string]interface{}{
				"ports": []interface{}{
					map[string]interface{}{"published": publishedPort, "target": float64(80), "protocol": "tcp"},
				},
			},
		},
	}
}

func TestExtractPublishedHostPorts(t *testing.T) {
	ports := extractPublishedHostPorts(portMapConfig("app", "8080"))
	if len(ports) != 1 || ports[0] != "8080/tcp" {
		t.Fatalf("expected [8080/tcp], got %v", ports)
	}
}

func TestExtractPublishedHostPortsNoServices(t *testing.T) {
	if ports := extractPublishedHostPorts(map[string]interface{}{}); ports != nil {
		t.Fatalf("expected nil for a config with no services, got %v", ports)
	}
}

// TestExtractPublishedHostPortsSkipsMalformedShapes exercises every
// defensive type-assertion branch: a `docker compose config` JSON result is
// trusted output, but the function still guards against a service, ports
// list, or port entry that isn't shaped as expected, and a port entry with
// no "published" host port (an internal-only container port).
func TestExtractPublishedHostPortsSkipsMalformedShapes(t *testing.T) {
	configMap := map[string]interface{}{
		"services": map[string]interface{}{
			"not-a-map":        "oops",
			"ports-not-a-list": map[string]interface{}{"ports": "oops"},
			"port-not-a-map":   map[string]interface{}{"ports": []interface{}{"oops"}},
			"no-published":     map[string]interface{}{"ports": []interface{}{map[string]interface{}{"target": float64(80), "protocol": "tcp"}}},
			"valid":            map[string]interface{}{"ports": []interface{}{map[string]interface{}{"published": "9090", "protocol": "tcp"}}},
		},
	}
	ports := extractPublishedHostPorts(configMap)
	if len(ports) != 1 || ports[0] != "9090/tcp" {
		t.Fatalf("expected only the one well-formed port entry, got %v", ports)
	}
}

// TestExtractPublishedHostPortsDefaultsMissingProtocolToTCP covers a port
// entry with no "protocol" field — `docker compose config` always includes
// one in practice, but the function still defaults defensively.
func TestExtractPublishedHostPortsDefaultsMissingProtocolToTCP(t *testing.T) {
	configMap := map[string]interface{}{
		"services": map[string]interface{}{
			"web": map[string]interface{}{
				"ports": []interface{}{map[string]interface{}{"published": "8080"}},
			},
		},
	}
	ports := extractPublishedHostPorts(configMap)
	if len(ports) != 1 || ports[0] != "8080/tcp" {
		t.Fatalf("expected [8080/tcp], got %v", ports)
	}
}

func TestBuildMigratePreviewPortConflictOnlyFlaggedWhenProjectNameDiffers(t *testing.T) {
	// Same project name, same host port: docker compose recreates within
	// the same project, releasing the port before rebinding — no conflict.
	sameProject := buildMigratePreview("s", "t", portMapConfig("myapp", "8080"), portMapConfig("myapp", "8080"), SopsCheck{Status: "none"})
	for _, w := range sameProject.Warnings {
		if w.Code == "port_conflict" {
			t.Fatalf("expected no port_conflict warning when the project name is unchanged, got %+v", sameProject.Warnings)
		}
	}

	// Different project name, same host port: old project keeps running
	// untouched, so the new project's `up` hits a real bind conflict.
	diffProject := buildMigratePreview("s", "t", portMapConfig("old-name", "8080"), portMapConfig("new-name", "8080"), SopsCheck{Status: "none"})
	var found bool
	for _, w := range diffProject.Warnings {
		if w.Code == "port_conflict" {
			found = true
			if w.Severity != "critical" {
				t.Fatalf("expected port_conflict to be critical, got %q", w.Severity)
			}
			if !strings.Contains(w.Message, "8080/tcp") {
				t.Fatalf("expected message to name the conflicting port, got: %s", w.Message)
			}
		}
	}
	if !found {
		t.Fatalf("expected a port_conflict warning when the project name changes and ports collide, got %+v", diffProject.Warnings)
	}
}

func TestBuildMigratePreviewNoPortConflictWhenPortsDiffer(t *testing.T) {
	preview := buildMigratePreview("s", "t", portMapConfig("old-name", "8080"), portMapConfig("new-name", "9090"), SopsCheck{Status: "none"})
	for _, w := range preview.Warnings {
		if w.Code == "port_conflict" {
			t.Fatalf("expected no port_conflict warning when host ports differ, got %+v", preview.Warnings)
		}
	}
}

func TestBuildMigratePreviewSopsWarnings(t *testing.T) {
	empty := map[string]interface{}{"name": "app", "services": map[string]interface{}{}}

	undecryptable := buildMigratePreview("s", "t", empty, empty, SopsCheck{Status: "undecryptable", TargetAgePublicKey: "age1abc"})
	var undecryptableFound bool
	for _, w := range undecryptable.Warnings {
		if w.Code == "sops_undecryptable" && strings.Contains(w.Message, "age1abc") {
			undecryptableFound = true
		}
	}
	if !undecryptableFound {
		t.Fatalf("expected sops_undecryptable warning naming the target pubkey, got %+v", undecryptable.Warnings)
	}

	sourceHad := buildMigratePreview("s", "t", empty, empty, SopsCheck{Status: "source_had_secrets"})
	var sourceHadFound bool
	for _, w := range sourceHad.Warnings {
		if w.Code == "sops_source_had_secrets" {
			sourceHadFound = true
		}
	}
	if !sourceHadFound {
		t.Fatalf("expected sops_source_had_secrets warning, got %+v", sourceHad.Warnings)
	}

	ok := buildMigratePreview("s", "t", empty, empty, SopsCheck{Status: "ok"})
	for _, w := range ok.Warnings {
		if strings.HasPrefix(w.Code, "sops_") {
			t.Fatalf("expected no sops warning for status=ok, got %+v", ok.Warnings)
		}
	}
}

// TestBuildMigratePreviewSopsUndecryptableWithoutPubKeyOmitsParenthetical
// covers SopsCheck.TargetAgePublicKey empty on an undecryptable result —
// migrateWarnings' pubkey-in-message branch is otherwise never taken in the
// other sops tests, which all set a key.
func TestBuildMigratePreviewSopsUndecryptableWithoutPubKeyOmitsParenthetical(t *testing.T) {
	empty := map[string]interface{}{"name": "app", "services": map[string]interface{}{}}
	preview := buildMigratePreview("s", "t", empty, empty, SopsCheck{Status: "undecryptable"})

	var found bool
	for _, w := range preview.Warnings {
		if w.Code == "sops_undecryptable" {
			found = true
			if strings.Contains(w.Message, "target age public key") {
				t.Fatalf("expected no pubkey parenthetical when TargetAgePublicKey is empty, got: %s", w.Message)
			}
		}
	}
	if !found {
		t.Fatal("expected a sops_undecryptable warning")
	}
}

func TestResolveMigrateDestComposePathsManual(t *testing.T) {
	composePath, composeFile, err := resolveMigrateDestComposePaths("/unused", "repo-id", false, migrateRequestBody{
		ComposePath: "apps/api",
		ComposeFile: "compose.yml",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if composePath != "apps/api" || composeFile != "compose.yml" {
		t.Fatalf("expected apps/api + compose.yml, got %q + %q", composePath, composeFile)
	}
}

func TestResolveMigrateDestComposePathsManualDefaultsComposeFile(t *testing.T) {
	composePath, composeFile, err := resolveMigrateDestComposePaths("/unused", "repo-id", false, migrateRequestBody{
		ComposePath: ".",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if composePath != "." || composeFile != "docker-compose.yml" {
		t.Fatalf("expected '.' + docker-compose.yml, got %q + %q", composePath, composeFile)
	}
}

func TestResolveMigrateDestComposePathsManualRequiresPathAndFile(t *testing.T) {
	_, _, err := resolveMigrateDestComposePaths("/unused", "repo-id", false, migrateRequestBody{})
	if err == nil {
		t.Fatal("expected an error when both compose_path and compose_file are empty for a manual stack")
	}
}

func TestResolveMigrateDestComposePathsWireopsManagedRequiresWireopsFile(t *testing.T) {
	_, _, err := resolveMigrateDestComposePaths("/unused", "repo-id", true, migrateRequestBody{})
	if err == nil {
		t.Fatal("expected an error when wireops_file is missing for a wireops-managed stack")
	}
}

func TestResolveMigrateDestComposePathsWireopsManagedDerivesPaths(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("REPOS_WORKSPACE", workspace)
	repoDir := filepath.Join(workspace, "dest-repo")
	apiDir := filepath.Join(repoDir, "apps", "api")
	if err := os.MkdirAll(apiDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, apiDir, "wireops.yaml", "version: wireops.v1\nname: api\n")
	writeFile(t, apiDir, "docker-compose.yml", "services:\n  web:\n    image: nginx\n")

	composePath, composeFile, err := resolveMigrateDestComposePaths(repoDir, "dest-repo", true, migrateRequestBody{
		WireopsFile: "apps/api/wireops.yaml",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if composePath != filepath.Join("apps", "api") || composeFile != "docker-compose.yml" {
		t.Fatalf("expected apps/api + docker-compose.yml, got %q + %q", composePath, composeFile)
	}
}

// TestResolveMigrateDestComposePathsWireopsManagedParseError covers the
// manifest.ParseWireopsFile failure branch (a malformed wireops.yaml on the
// target) — distinct from ResolutionError below, which is a valid
// wireops.yaml that just can't find its compose file.
func TestResolveMigrateDestComposePathsWireopsManagedParseError(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("REPOS_WORKSPACE", workspace)
	repoDir := filepath.Join(workspace, "dest-repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, repoDir, "wireops.yaml", "not: valid: wireops:\n")

	_, _, err := resolveMigrateDestComposePaths(repoDir, "dest-repo", true, migrateRequestBody{
		WireopsFile: "wireops.yaml",
	})
	if err == nil {
		t.Fatal("expected an error for a malformed wireops.yaml")
	}
}

// TestResolveMigrateDestComposePathsWireopsManagedResolutionError covers a
// syntactically valid wireops.yaml whose compose file can't be resolved
// (none present) — surfaced as def.ResolutionError rather than a parse err.
func TestResolveMigrateDestComposePathsWireopsManagedResolutionError(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("REPOS_WORKSPACE", workspace)
	repoDir := filepath.Join(workspace, "dest-repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, repoDir, "wireops.yaml", "version: wireops.v1\nname: api\n")

	_, _, err := resolveMigrateDestComposePaths(repoDir, "dest-repo", true, migrateRequestBody{
		WireopsFile: "wireops.yaml",
	})
	if err == nil {
		t.Fatal("expected an error when no compose file can be resolved next to the wireops.yaml")
	}
}

func TestComposeWorkDir(t *testing.T) {
	if got := composeWorkDir("/repo", ""); got != "/repo" {
		t.Fatalf("expected /repo for an empty compose_path, got %q", got)
	}
	if got := composeWorkDir("/repo", "."); got != "/repo" {
		t.Fatalf("expected /repo for compose_path='.', got %q", got)
	}
	if got := composeWorkDir("/repo", "apps/api"); got != filepath.Join("/repo", "apps", "api") {
		t.Fatalf("expected /repo/apps/api, got %q", got)
	}
}

// --- resolveSopsCheck: filesystem + crypto only, no docker, no app ---

func TestResolveSopsCheckNone(t *testing.T) {
	destWorkDir := t.TempDir()
	sourceWorkDir := t.TempDir()

	got := resolveSopsCheck(context.Background(), nil, destWorkDir, destWorkDir, sourceWorkDir, sourceWorkDir)
	if got.Status != "none" {
		t.Fatalf("expected status=none, got %+v", got)
	}
}

func TestResolveSopsCheckSourceHadSecrets(t *testing.T) {
	destWorkDir := t.TempDir()
	sourceWorkDir := t.TempDir()
	writeFile(t, sourceWorkDir, "secrets.yaml", "DB_PASS: whatever\n")

	got := resolveSopsCheck(context.Background(), nil, destWorkDir, destWorkDir, sourceWorkDir, sourceWorkDir)
	if got.Status != "source_had_secrets" {
		t.Fatalf("expected status=source_had_secrets, got %+v", got)
	}
}

func newSopsCheckTestRepoRecord(t *testing.T, app core.App, encryptedKey, publicKey string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("repositories")
	if err != nil {
		t.Fatalf("find repositories collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("name", "dest-repo")
	rec.Set("git_url", "https://example.com/repo.git")
	rec.Set("sops_age_key", encryptedKey)
	rec.Set("sops_age_public_key", publicKey)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save repo: %v", err)
	}
	return rec
}

func TestResolveSopsCheckOkAndUndecryptable(t *testing.T) {
	t.Setenv("SECRET_KEY", testSecretBackendKey)
	app := newSetupTestApp(t)

	correctPriv, correctPub, err := secrets.GenerateAgeKeypair()
	if err != nil {
		t.Fatalf("GenerateAgeKeypair: %v", err)
	}
	_, wrongPub, err := secrets.GenerateAgeKeypair()
	if err != nil {
		t.Fatalf("GenerateAgeKeypair: %v", err)
	}
	encryptedCorrectPriv, err := crypto.Encrypt([]byte(correctPriv), crypto.NormalizeSecretKey(testSecretBackendKey))
	if err != nil {
		t.Fatalf("crypto.Encrypt: %v", err)
	}

	// ok: secrets.yaml encrypted for the destination's own key.
	okRepo := newSopsCheckTestRepoRecord(t, app, encryptedCorrectPriv, correctPub)
	okDir := t.TempDir()
	okEncrypted := testutil.EncryptForAge(t, correctPub, []byte("DB_PASS: hunter2\n"))
	if err := os.WriteFile(filepath.Join(okDir, "secrets.yaml"), okEncrypted, 0o644); err != nil {
		t.Fatalf("write secrets.yaml: %v", err)
	}
	okSourceDir := t.TempDir()
	got := resolveSopsCheck(context.Background(), okRepo, okDir, okDir, okSourceDir, okSourceDir)
	if got.Status != "ok" {
		t.Fatalf("expected status=ok, got %+v", got)
	}

	// undecryptable: secrets.yaml encrypted for a *different* key than the
	// destination repository's own age key — the expected post-migration state.
	badDir := t.TempDir()
	badEncrypted := testutil.EncryptForAge(t, wrongPub, []byte("DB_PASS: hunter2\n"))
	if err := os.WriteFile(filepath.Join(badDir, "secrets.yaml"), badEncrypted, 0o644); err != nil {
		t.Fatalf("write secrets.yaml: %v", err)
	}
	badSourceDir := t.TempDir()
	got = resolveSopsCheck(context.Background(), okRepo, badDir, badDir, badSourceDir, badSourceDir)
	if got.Status != "undecryptable" || got.TargetAgePublicKey != correctPub {
		t.Fatalf("expected status=undecryptable with pubkey=%s, got %+v", correctPub, got)
	}
}

// TestResolveSopsCheckMissingAgeKey covers the destination repo having a
// secrets.yaml but no sops_age_key configured at all (e.g. the repository
// predates SOPS support, or the key generation hook never ran) — distinct
// from "wrong key": here there is no key to even attempt a decrypt with.
func TestResolveSopsCheckMissingAgeKey(t *testing.T) {
	t.Setenv("SECRET_KEY", testSecretBackendKey)
	app := newSetupTestApp(t)

	_, pub, err := secrets.GenerateAgeKeypair()
	if err != nil {
		t.Fatalf("GenerateAgeKeypair: %v", err)
	}
	repo := newSopsCheckTestRepoRecord(t, app, "", "")

	destDir := t.TempDir()
	encrypted := testutil.EncryptForAge(t, pub, []byte("DB_PASS: hunter2\n"))
	if err := os.WriteFile(filepath.Join(destDir, "secrets.yaml"), encrypted, 0o644); err != nil {
		t.Fatalf("write secrets.yaml: %v", err)
	}

	sourceDir := t.TempDir()
	got := resolveSopsCheck(context.Background(), repo, destDir, destDir, sourceDir, sourceDir)
	if got.Status != "undecryptable" {
		t.Fatalf("expected status=undecryptable when the repo has no age key, got %+v", got)
	}
}

// TestResolveSopsCheckCorruptedAgeKey covers crypto.Decrypt itself failing
// (e.g. the stored ciphertext predates a SECRET_KEY rotation) — distinct
// from "no key configured" (empty string) and "wrong key" (valid ciphertext,
// wrong recipient): here the stored value isn't even valid base64/AES-GCM.
func TestResolveSopsCheckCorruptedAgeKey(t *testing.T) {
	t.Setenv("SECRET_KEY", testSecretBackendKey)
	app := newSetupTestApp(t)

	_, pub, err := secrets.GenerateAgeKeypair()
	if err != nil {
		t.Fatalf("GenerateAgeKeypair: %v", err)
	}
	repo := newSopsCheckTestRepoRecord(t, app, "not-valid-base64-ciphertext!!!", pub)

	destDir := t.TempDir()
	encrypted := testutil.EncryptForAge(t, pub, []byte("DB_PASS: hunter2\n"))
	if err := os.WriteFile(filepath.Join(destDir, "secrets.yaml"), encrypted, 0o644); err != nil {
		t.Fatalf("write secrets.yaml: %v", err)
	}

	sourceDir := t.TempDir()
	got := resolveSopsCheck(context.Background(), repo, destDir, destDir, sourceDir, sourceDir)
	if got.Status != "undecryptable" {
		t.Fatalf("expected status=undecryptable for a corrupted stored age key, got %+v", got)
	}
}

// --- sync.Scheduler.IsSyncing test-helper local git fixtures ---

// localGitFixture initializes a real git repo at dir on branch "master" with
// the given files, so tests can point a repositories.git_url at a plain
// filesystem path and exercise the real CloneOrFetchContext path with no
// network access.
func localGitFixture(t *testing.T, dir string, files map[string]string) string {
	t.Helper()
	repo, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	for rel, content := range files {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
		if _, err := wt.Add(rel); err != nil {
			t.Fatalf("git add %s: %v", rel, err)
		}
	}
	hash, err := wt.Commit("initial", &gogit.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@example.com"},
	})
	if err != nil {
		t.Fatalf("git commit: %v", err)
	}
	return hash.String()
}

func setupMigrateTestApp(t *testing.T, withHooks bool) (*tests.TestApp, http.Handler, *wiresync.Scheduler) {
	t.Helper()
	return setupMigrateTestAppWithWorker(t, withHooks, nil)
}

// setupMigrateTestAppWithWorker is setupMigrateTestApp for tests that also
// need rr.workerSvc wired (the coordinated-teardown path dispatches a
// TeardownCommand through it) — a nil workerSvc is fine for every route path
// that never reaches that dispatch. Callers that need to inspect the
// dispatcher afterward should keep their own reference to what they pass in
// rather than type-asserting a returned value back.
func setupMigrateTestAppWithWorker(t *testing.T, withHooks bool, workerSvc wiresync.WorkerDispatcher) (*tests.TestApp, http.Handler, *wiresync.Scheduler) {
	t.Helper()
	t.Setenv("SECRET_KEY", testSecretBackendKey)
	workspace := t.TempDir()
	t.Setenv("REPOS_WORKSPACE", workspace)

	app := newSetupTestApp(t)
	admin := createTestUser(t, app, "migrate-admin@example.com", "Password1!", "admin")

	scheduler := wiresync.NewScheduler(app, workerSvc)
	if withHooks {
		hooks.Register(app, scheduler, nil, logstream.New())
	}

	r := router.NewRouter(func(w http.ResponseWriter, req *http.Request) (*core.RequestEvent, router.EventCleanupFunc) {
		return &core.RequestEvent{
			App:   app,
			Event: router.Event{Response: w, Request: req},
			Auth:  admin,
		}, nil
	})

	rr := routeRegistrar{r: r, app: app, scheduler: scheduler, workerSvc: workerSvc}
	rr.registerMigratePreviewRoute()
	rr.registerMigrateRoute()

	mux, err := r.BuildMux()
	if err != nil {
		t.Fatalf("build mux: %v", err)
	}
	return app, mux, scheduler
}

// waitUntilNotSyncing polls scheduler.IsSyncing for stackID: with hooks
// registered, OnRecordAfterCreateSuccess("stacks") auto-triggers a
// background sync on every stack fixture this file creates, which briefly
// holds the same per-stack mutex the migrate route's 409 guard checks. Tests
// that register real hooks and then call /migrate must wait that
// auto-triggered sync out first, or they race it.
func waitUntilNotSyncing(t *testing.T, scheduler *wiresync.Scheduler, stackID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !scheduler.IsSyncing(stackID) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("stack %s still syncing after deadline", stackID)
}

func createMigrateTestRepo(t *testing.T, app core.App, name, gitURL string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("repositories")
	if err != nil {
		t.Fatalf("find repositories collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("name", name)
	rec.Set("git_url", gitURL)
	rec.Set("branch", "master")
	if err := app.Save(rec); err != nil {
		t.Fatalf("save repo: %v", err)
	}
	return rec
}

func createMigrateTestStack(t *testing.T, app core.App, repoID string, extra map[string]any) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("stacks")
	if err != nil {
		t.Fatalf("find stacks collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("name", "migrate-test-stack")
	rec.Set("repository", repoID)
	rec.Set("compose_path", ".")
	rec.Set("compose_file", "docker-compose.yml")
	for k, v := range extra {
		rec.Set(k, v)
	}
	if err := app.Save(rec); err != nil {
		t.Fatalf("save stack: %v", err)
	}
	return rec
}

func migrateReq(repo string, extra map[string]any) map[string]any {
	body := map[string]any{"repository": repo}
	for k, v := range extra {
		body[k] = v
	}
	return body
}

// --- /migrate/preview: request-validation paths (no docker needed) ---

func TestMigratePreviewSameRepositoryRejected(t *testing.T) {
	app, mux, _ := setupMigrateTestApp(t, false)
	repo := createMigrateTestRepo(t, app, "repo-a", "https://example.com/a.git")
	stack := createMigrateTestStack(t, app, repo.Id, nil)

	body, _ := json.Marshal(migrateReq(repo.Id, nil))
	req := httptest.NewRequest(http.MethodPost, "/api/custom/stacks/"+stack.Id+"/migrate/preview", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMigratePreviewTargetRepositoryNotFound(t *testing.T) {
	app, mux, _ := setupMigrateTestApp(t, false)
	repo := createMigrateTestRepo(t, app, "repo-a", "https://example.com/a.git")
	stack := createMigrateTestStack(t, app, repo.Id, nil)

	body, _ := json.Marshal(migrateReq("does-not-exist", nil))
	req := httptest.NewRequest(http.MethodPost, "/api/custom/stacks/"+stack.Id+"/migrate/preview", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMigratePreviewInvalidBody(t *testing.T) {
	app, mux, _ := setupMigrateTestApp(t, false)
	repo := createMigrateTestRepo(t, app, "repo-a", "https://example.com/a.git")
	stack := createMigrateTestStack(t, app, repo.Id, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/custom/stacks/"+stack.Id+"/migrate/preview", strings.NewReader("{"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMigratePreviewMissingRepositoryField(t *testing.T) {
	app, mux, _ := setupMigrateTestApp(t, false)
	repo := createMigrateTestRepo(t, app, "repo-a", "https://example.com/a.git")
	stack := createMigrateTestStack(t, app, repo.Id, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/custom/stacks/"+stack.Id+"/migrate/preview", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMigratePreviewStackNotFound(t *testing.T) {
	_, mux, _ := setupMigrateTestApp(t, false)
	req := httptest.NewRequest(http.MethodPost, "/api/custom/stacks/does-not-exist/migrate/preview", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestMigratePreviewWireopsManagedResolutionErrorReturns422 covers the
// resolveMigrateDestComposePaths error branch inside the preview handler: a
// wireops-managed stack pointed at a target wireops.yaml with no compose
// file next to it — a request-shaped failure (422), never a compose diff.
func TestMigratePreviewWireopsManagedResolutionErrorReturns422(t *testing.T) {
	app, mux, _ := setupMigrateTestApp(t, false)

	sourceRepo := createMigrateTestRepo(t, app, "source-repo", "https://example.com/source.git")

	targetDir := t.TempDir()
	localGitFixture(t, targetDir, map[string]string{"wireops.yaml": "version: wireops.v1\nname: api\n"})
	targetRepo := createMigrateTestRepo(t, app, "target-repo", targetDir)

	stack := createMigrateTestStack(t, app, sourceRepo.Id, map[string]any{"config_source": "wireops_file"})

	body, _ := json.Marshal(migrateReq(targetRepo.Id, map[string]any{"wireops_file": "wireops.yaml"}))
	req := httptest.NewRequest(http.MethodPost, "/api/custom/stacks/"+stack.Id+"/migrate/preview", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMigratePreviewRejectsUnsafeComposePath(t *testing.T) {
	app, mux, _ := setupMigrateTestApp(t, false)
	sourceRepo := createMigrateTestRepo(t, app, "source-repo", "https://example.com/source.git")
	targetDir := t.TempDir()
	localGitFixture(t, targetDir, map[string]string{"docker-compose.yml": "services:\n  web:\n    image: nginx\n"})
	targetRepo := createMigrateTestRepo(t, app, "target-repo", targetDir)
	stack := createMigrateTestStack(t, app, sourceRepo.Id, nil)

	body, _ := json.Marshal(migrateReq(targetRepo.Id, map[string]any{"compose_path": "../evil", "compose_file": "docker-compose.yml"}))
	req := httptest.NewRequest(http.MethodPost, "/api/custom/stacks/"+stack.Id+"/migrate/preview", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMigratePreviewRejectsUnsafeComposeFile(t *testing.T) {
	app, mux, _ := setupMigrateTestApp(t, false)
	sourceRepo := createMigrateTestRepo(t, app, "source-repo", "https://example.com/source.git")
	targetDir := t.TempDir()
	localGitFixture(t, targetDir, map[string]string{"docker-compose.yml": "services:\n  web:\n    image: nginx\n"})
	targetRepo := createMigrateTestRepo(t, app, "target-repo", targetDir)
	stack := createMigrateTestStack(t, app, sourceRepo.Id, nil)

	body, _ := json.Marshal(migrateReq(targetRepo.Id, map[string]any{"compose_path": ".", "compose_file": "notes.txt"}))
	req := httptest.NewRequest(http.MethodPost, "/api/custom/stacks/"+stack.Id+"/migrate/preview", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestMigratePreviewTargetRepoCloneFailure covers repoFilesSetupByID's
// error branch — an unreachable target git_url — surfaced as a 500 since
// it's an infrastructure lookup failure, not a request-shape problem.
func TestMigratePreviewTargetRepoCloneFailure(t *testing.T) {
	app, mux, _ := setupMigrateTestApp(t, false)
	sourceRepo := createMigrateTestRepo(t, app, "source-repo", "https://example.com/source.git")
	targetRepo := createMigrateTestRepo(t, app, "target-repo", filepath.Join(t.TempDir(), "does-not-exist"))
	stack := createMigrateTestStack(t, app, sourceRepo.Id, nil)

	body, _ := json.Marshal(migrateReq(targetRepo.Id, map[string]any{"compose_path": ".", "compose_file": "docker-compose.yml"}))
	req := httptest.NewRequest(http.MethodPost, "/api/custom/stacks/"+stack.Id+"/migrate/preview", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestMigratePreviewReportsRemovedVolumeAsCritical exercises the full
// preview handler end to end: two local git fixtures (source stack repo,
// target repo) with a named volume dropped on the target, cloned for real
// via CloneOrFetchContext and diffed via a real `docker compose config`.
func TestMigratePreviewReportsRemovedVolumeAsCritical(t *testing.T) {
	requireDocker(t)
	app, mux, _ := setupMigrateTestApp(t, false)

	sourceDir := t.TempDir()
	localGitFixture(t, sourceDir, map[string]string{
		"docker-compose.yml": "name: myapp\nservices:\n  web:\n    image: nginx:latest\n    volumes:\n      - data:/data\nvolumes:\n  data:\n",
	})
	sourceRepo := createMigrateTestRepo(t, app, "source-repo", sourceDir)
	// The preview's source-side fallback (no rendered revision yet) reads a
	// live `docker compose config` from the source repo's already-cloned
	// workspace, mirroring a stack the scheduler's own ticker has already
	// fetched at least once — clone it here to reproduce that precondition.
	if _, err := git.CloneOrFetch(sourceRepo.Id, sourceDir, "master", nil, config.GetReposWorkspace()); err != nil {
		t.Fatalf("clone source repo: %v", err)
	}

	targetDir := t.TempDir()
	localGitFixture(t, targetDir, map[string]string{
		"docker-compose.yml": "name: myapp\nservices:\n  web:\n    image: nginx:latest\n",
	})
	targetRepo := createMigrateTestRepo(t, app, "target-repo", targetDir)

	stack := createMigrateTestStack(t, app, sourceRepo.Id, nil)

	body, _ := json.Marshal(migrateReq(targetRepo.Id, map[string]any{
		"compose_path": ".",
		"compose_file": "docker-compose.yml",
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/custom/stacks/"+stack.Id+"/migrate/preview", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var preview MigratePreview
	if err := json.Unmarshal(rec.Body.Bytes(), &preview); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(preview.Volumes.Removed) != 1 || preview.Volumes.Removed[0] != "data" {
		t.Fatalf("expected volume 'data' reported removed, got %+v", preview.Volumes)
	}
	var criticalFound bool
	for _, w := range preview.Warnings {
		if w.Code == "volume_removed" && w.Severity == "critical" {
			criticalFound = true
		}
	}
	if !criticalFound {
		t.Fatalf("expected a critical volume_removed warning, got %+v", preview.Warnings)
	}
	if preview.Sops.Status != "none" {
		t.Fatalf("expected sops status=none, got %+v", preview.Sops)
	}
}

// --- /migrate: request-validation paths (no docker, no git needed) ---

func TestMigrateRequiresConfirm(t *testing.T) {
	app, mux, _ := setupMigrateTestApp(t, false)
	repoA := createMigrateTestRepo(t, app, "repo-a", "https://example.com/a.git")
	repoB := createMigrateTestRepo(t, app, "repo-b", "https://example.com/b.git")
	stack := createMigrateTestStack(t, app, repoA.Id, nil)

	body, _ := json.Marshal(migrateReq(repoB.Id, map[string]any{"compose_path": ".", "compose_file": "docker-compose.yml"}))
	req := httptest.NewRequest(http.MethodPost, "/api/custom/stacks/"+stack.Id+"/migrate", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 without confirm=true, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMigrateSameRepositoryRejected(t *testing.T) {
	app, mux, _ := setupMigrateTestApp(t, false)
	repo := createMigrateTestRepo(t, app, "repo-a", "https://example.com/a.git")
	stack := createMigrateTestStack(t, app, repo.Id, nil)

	body, _ := json.Marshal(migrateReq(repo.Id, map[string]any{"confirm": true}))
	req := httptest.NewRequest(http.MethodPost, "/api/custom/stacks/"+stack.Id+"/migrate", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMigrateTargetRepositoryNotFound(t *testing.T) {
	app, mux, _ := setupMigrateTestApp(t, false)
	repo := createMigrateTestRepo(t, app, "repo-a", "https://example.com/a.git")
	stack := createMigrateTestStack(t, app, repo.Id, nil)

	body, _ := json.Marshal(migrateReq("does-not-exist", map[string]any{"confirm": true}))
	req := httptest.NewRequest(http.MethodPost, "/api/custom/stacks/"+stack.Id+"/migrate", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMigrateInvalidBody(t *testing.T) {
	app, mux, _ := setupMigrateTestApp(t, false)
	repo := createMigrateTestRepo(t, app, "repo-a", "https://example.com/a.git")
	stack := createMigrateTestStack(t, app, repo.Id, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/custom/stacks/"+stack.Id+"/migrate", strings.NewReader("{"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMigrateMissingRepositoryField(t *testing.T) {
	app, mux, _ := setupMigrateTestApp(t, false)
	repo := createMigrateTestRepo(t, app, "repo-a", "https://example.com/a.git")
	stack := createMigrateTestStack(t, app, repo.Id, nil)

	body, _ := json.Marshal(map[string]any{"confirm": true})
	req := httptest.NewRequest(http.MethodPost, "/api/custom/stacks/"+stack.Id+"/migrate", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMigrateStackNotFound(t *testing.T) {
	_, mux, _ := setupMigrateTestApp(t, false)
	body, _ := json.Marshal(map[string]any{"repository": "repo-b", "confirm": true})
	req := httptest.NewRequest(http.MethodPost, "/api/custom/stacks/does-not-exist/migrate", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestMigrateWireopsManagedResolutionErrorReturns422 is the /migrate
// counterpart of the preview test above: same failure (no compose file next
// to the target wireops.yaml), surfaced through the mutation route instead —
// must not mutate the record.
func TestMigrateWireopsManagedResolutionErrorReturns422(t *testing.T) {
	app, mux, _ := setupMigrateTestApp(t, false)

	sourceRepo := createMigrateTestRepo(t, app, "source-repo", "https://example.com/source.git")

	targetDir := t.TempDir()
	localGitFixture(t, targetDir, map[string]string{"wireops.yaml": "version: wireops.v1\nname: api\n"})
	targetRepo := createMigrateTestRepo(t, app, "target-repo", targetDir)

	stack := createMigrateTestStack(t, app, sourceRepo.Id, map[string]any{"config_source": "wireops_file"})

	body, _ := json.Marshal(migrateReq(targetRepo.Id, map[string]any{"wireops_file": "wireops.yaml", "confirm": true}))
	req := httptest.NewRequest(http.MethodPost, "/api/custom/stacks/"+stack.Id+"/migrate", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
	}

	updated, err := app.FindRecordById("stacks", stack.Id)
	if err != nil {
		t.Fatalf("reload stack: %v", err)
	}
	if updated.GetString("repository") != sourceRepo.Id {
		t.Fatalf("expected repository unchanged after a 422, got %s", updated.GetString("repository"))
	}
}

func TestMigrateRejectsUnsafeComposePath(t *testing.T) {
	app, mux, _ := setupMigrateTestApp(t, false)
	sourceRepo := createMigrateTestRepo(t, app, "source-repo", "https://example.com/source.git")
	targetDir := t.TempDir()
	localGitFixture(t, targetDir, map[string]string{"docker-compose.yml": "services:\n  web:\n    image: nginx\n"})
	targetRepo := createMigrateTestRepo(t, app, "target-repo", targetDir)
	stack := createMigrateTestStack(t, app, sourceRepo.Id, nil)

	body, _ := json.Marshal(migrateReq(targetRepo.Id, map[string]any{"compose_path": "../evil", "compose_file": "docker-compose.yml", "confirm": true}))
	req := httptest.NewRequest(http.MethodPost, "/api/custom/stacks/"+stack.Id+"/migrate", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMigrateRejectsUnsafeComposeFile(t *testing.T) {
	app, mux, _ := setupMigrateTestApp(t, false)
	sourceRepo := createMigrateTestRepo(t, app, "source-repo", "https://example.com/source.git")
	targetDir := t.TempDir()
	localGitFixture(t, targetDir, map[string]string{"docker-compose.yml": "services:\n  web:\n    image: nginx\n"})
	targetRepo := createMigrateTestRepo(t, app, "target-repo", targetDir)
	stack := createMigrateTestStack(t, app, sourceRepo.Id, nil)

	body, _ := json.Marshal(migrateReq(targetRepo.Id, map[string]any{"compose_path": ".", "compose_file": "notes.txt", "confirm": true}))
	req := httptest.NewRequest(http.MethodPost, "/api/custom/stacks/"+stack.Id+"/migrate", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMigrateTargetRepoCloneFailure(t *testing.T) {
	app, mux, _ := setupMigrateTestApp(t, false)
	sourceRepo := createMigrateTestRepo(t, app, "source-repo", "https://example.com/source.git")
	targetRepo := createMigrateTestRepo(t, app, "target-repo", filepath.Join(t.TempDir(), "does-not-exist"))
	stack := createMigrateTestStack(t, app, sourceRepo.Id, nil)

	body, _ := json.Marshal(migrateReq(targetRepo.Id, map[string]any{"compose_path": ".", "compose_file": "docker-compose.yml", "confirm": true}))
	req := httptest.NewRequest(http.MethodPost, "/api/custom/stacks/"+stack.Id+"/migrate", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestMigrateRejectedWhileSyncing is the deterministic HTTP-layer
// counterpart to internal/sync's TestReconcilerIsSyncingReflectsLockState:
// LockStackForTest holds the exact mutex the route's IsSyncing guard
// checks, so this exercises the 409 branch without racing a real
// background reconcile to observe "currently syncing".
func TestMigrateRejectedWhileSyncing(t *testing.T) {
	app, mux, scheduler := setupMigrateTestApp(t, false)
	sourceRepo := createMigrateTestRepo(t, app, "source-repo", "https://example.com/source.git")
	targetRepo := createMigrateTestRepo(t, app, "target-repo", "https://example.com/target.git")
	stack := createMigrateTestStack(t, app, sourceRepo.Id, nil)

	unlock := scheduler.LockStackForTest(stack.Id)
	defer unlock()

	body, _ := json.Marshal(migrateReq(targetRepo.Id, map[string]any{"compose_path": ".", "compose_file": "docker-compose.yml", "confirm": true}))
	req := httptest.NewRequest(http.MethodPost, "/api/custom/stacks/"+stack.Id+"/migrate", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}

	updated, err := app.FindRecordById("stacks", stack.Id)
	if err != nil {
		t.Fatalf("reload stack: %v", err)
	}
	if updated.GetString("repository") != sourceRepo.Id {
		t.Fatalf("expected repository unchanged when rejected as syncing, got %s", updated.GetString("repository"))
	}
}

// TestMigrateRepointsRepositoryAndResetsState covers the mutation's core
// contract: repository/compose_path/compose_file move to the target and
// desired_commit/checksum reset for a clean next-reconcile diff, while
// identity fields (name, current_version) and env vars are left untouched.
// Uses a manual (non wireops-managed) stack, so the mutation path never
// shells out to docker — only a local-filesystem git clone of the target.
func TestMigrateRepointsRepositoryAndResetsState(t *testing.T) {
	app, mux, _ := setupMigrateTestApp(t, false)

	sourceDir := t.TempDir()
	localGitFixture(t, sourceDir, map[string]string{"docker-compose.yml": "services:\n  web:\n    image: nginx\n"})
	sourceRepo := createMigrateTestRepo(t, app, "source-repo", sourceDir)

	targetDir := t.TempDir()
	localGitFixture(t, targetDir, map[string]string{"apps/api/docker-compose.yml": "services:\n  web:\n    image: nginx\n"})
	targetRepo := createMigrateTestRepo(t, app, "target-repo", targetDir)

	stack := createMigrateTestStack(t, app, sourceRepo.Id, map[string]any{
		"current_version": 3,
		"desired_commit":  "deadbeef",
		"checksum":        "old-checksum",
		"status":          "active",
	})

	envCol, err := app.FindCollectionByNameOrId("stack_env_vars")
	if err != nil {
		t.Fatalf("find stack_env_vars collection: %v", err)
	}
	envVar := core.NewRecord(envCol)
	envVar.Set("stack", stack.Id)
	envVar.Set("key", "FOO")
	envVar.Set("value", "bar")
	if err := app.Save(envVar); err != nil {
		t.Fatalf("save env var: %v", err)
	}

	body, _ := json.Marshal(migrateReq(targetRepo.Id, map[string]any{
		"compose_path": "apps/api",
		"compose_file": "docker-compose.yml",
		"confirm":      true,
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/custom/stacks/"+stack.Id+"/migrate", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}

	updated, err := app.FindRecordById("stacks", stack.Id)
	if err != nil {
		t.Fatalf("reload stack: %v", err)
	}
	if updated.GetString("repository") != targetRepo.Id {
		t.Fatalf("expected repository=%s, got %s", targetRepo.Id, updated.GetString("repository"))
	}
	if updated.GetString("compose_path") != "apps/api" {
		t.Fatalf("expected compose_path=apps/api, got %s", updated.GetString("compose_path"))
	}
	if updated.GetString("compose_file") != "docker-compose.yml" {
		t.Fatalf("expected compose_file=docker-compose.yml, got %s", updated.GetString("compose_file"))
	}
	if updated.GetString("desired_commit") != "" {
		t.Fatalf("expected desired_commit cleared, got %q", updated.GetString("desired_commit"))
	}
	if updated.GetString("checksum") != "" {
		t.Fatalf("expected checksum cleared, got %q", updated.GetString("checksum"))
	}
	if updated.GetInt("current_version") != 3 {
		t.Fatalf("expected current_version preserved at 3, got %d", updated.GetInt("current_version"))
	}
	if updated.GetString("name") != "migrate-test-stack" {
		t.Fatalf("expected name preserved, got %q", updated.GetString("name"))
	}

	envVars, err := app.FindAllRecords("stack_env_vars", dbx.HashExp{"stack": stack.Id})
	if err != nil || len(envVars) != 1 {
		t.Fatalf("expected env var to survive migration, got %d records (err=%v)", len(envVars), err)
	}
}

func TestMigrateWritesAuditLog(t *testing.T) {
	app, mux, _ := setupMigrateTestApp(t, false)

	sourceDir := t.TempDir()
	localGitFixture(t, sourceDir, map[string]string{"docker-compose.yml": "services:\n  web:\n    image: nginx\n"})
	sourceRepo := createMigrateTestRepo(t, app, "source-repo", sourceDir)

	targetDir := t.TempDir()
	localGitFixture(t, targetDir, map[string]string{"docker-compose.yml": "services:\n  web:\n    image: nginx\n"})
	targetRepo := createMigrateTestRepo(t, app, "target-repo", targetDir)

	stack := createMigrateTestStack(t, app, sourceRepo.Id, nil)

	body, _ := json.Marshal(migrateReq(targetRepo.Id, map[string]any{
		"compose_path": ".",
		"compose_file": "docker-compose.yml",
		"confirm":      true,
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/custom/stacks/"+stack.Id+"/migrate", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}

	logs, err := app.FindAllRecords("audit_logs", dbx.HashExp{"action": "stack.migrate", "resource_id": stack.Id})
	if err != nil || len(logs) == 0 {
		t.Fatalf("expected a stack.migrate audit log, got %d records (err=%v)", len(logs), err)
	}
}

// TestMigrateWireopsManagedChangesPathsBypassesImmutability is the §10.1
// regression test: without WithMigrationBypass, a wireops-managed stack
// whose target repository resolves a different compose_path would be
// rejected by validateWireopsFieldsImmutable. This registers the real
// hooks (hooks.Register) so the immutability hook actually runs.
func TestMigrateWireopsManagedChangesPathsBypassesImmutability(t *testing.T) {
	app, mux, scheduler := setupMigrateTestApp(t, true)

	sourceDir := t.TempDir()
	localGitFixture(t, sourceDir, map[string]string{"docker-compose.yml": "services:\n  web:\n    image: nginx\n"})
	sourceRepo := createMigrateTestRepo(t, app, "source-repo", sourceDir)

	targetDir := t.TempDir()
	localGitFixture(t, targetDir, map[string]string{
		"apps/api/wireops.yaml":       "version: wireops.v1\nname: api\n",
		"apps/api/docker-compose.yml": "services:\n  web:\n    image: nginx\n",
	})
	targetRepo := createMigrateTestRepo(t, app, "target-repo", targetDir)

	workersCol, err := app.FindCollectionByNameOrId("workers")
	if err != nil {
		t.Fatalf("find workers collection: %v", err)
	}
	worker := core.NewRecord(workersCol)
	worker.Set("hostname", "worker-1")
	worker.Set("fingerprint", "fp-1")
	worker.Set("status", "ACTIVE")
	if err := app.Save(worker); err != nil {
		t.Fatalf("save worker: %v", err)
	}

	// hooks.Register wires an auto-sync on stack create that briefly holds
	// the same per-stack mutex the migrate route's 409 guard checks;
	// status=paused makes ReconcileStack return immediately after grabbing
	// that mutex instead of racing a real clone+render+dispatch attempt
	// against the migrate request sent right below.
	//
	// A LockStackForTest-based approach was tried here instead of the sleep
	// (holding the lock across stack creation so the auto-sync goroutine's
	// own TryLock would fail immediately) and does NOT work: Save() returning
	// says nothing about whether the goroutine it spawned has run yet, so
	// releasing right after Save() can free the lock before that goroutine
	// ever attempts to acquire it — it then grabs the now-free lock for real
	// during the HTTP request below and the migrate route correctly (if
	// confusingly for the test) 409s. There's no signal from production code
	// to synchronize against, so the sleep is the least-bad option here.
	stack := createMigrateTestStack(t, app, sourceRepo.Id, map[string]any{
		"config_source":     "wireops_file",
		"wireops_file_path": "wireops.yaml",
		"worker":            worker.Id,
		"status":            "paused",
	})
	// The auto-sync goroutine is only queued (not yet running) the instant
	// app.Save returns above, so an immediate IsSyncing() poll can race it
	// and see "not syncing" before it ever grabs the mutex. Give it a beat
	// to actually start (it then releases almost instantly on the paused
	// check) before polling it back to idle.
	time.Sleep(100 * time.Millisecond)
	waitUntilNotSyncing(t, scheduler, stack.Id)

	body, _ := json.Marshal(migrateReq(targetRepo.Id, map[string]any{
		"wireops_file": "apps/api/wireops.yaml",
		"confirm":      true,
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/custom/stacks/"+stack.Id+"/migrate", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 (bypass should let the path change through), got %d: %s", rec.Code, rec.Body.String())
	}

	updated, err := app.FindRecordById("stacks", stack.Id)
	if err != nil {
		t.Fatalf("reload stack: %v", err)
	}
	if updated.GetString("compose_path") != filepath.Join("apps", "api") {
		t.Fatalf("expected compose_path=apps/api, got %q", updated.GetString("compose_path"))
	}
	if updated.GetString("wireops_file_path") != "apps/api/wireops.yaml" {
		t.Fatalf("expected wireops_file_path updated, got %q", updated.GetString("wireops_file_path"))
	}

	// The bypass must not outlive the migrate Save: a follow-up edit to a
	// wireops-managed field through the normal path must still be rejected.
	updated.Set("compose_path", "somewhere-else")
	if err := app.Save(updated); err == nil {
		t.Fatal("expected a later unrelated edit to compose_path to be rejected once the migration bypass is cleared")
	}
}

// --- Coordinated teardown (Fase 4: teardown_old_project) ---

type recordingWorkerDispatcher struct {
	connected bool
	result    protocol.CommandResult
	err       error
	// entered, if non-nil, is closed the instant Dispatch is called — lets a
	// concurrency test wait deterministically for "the first request has
	// reached its critical section" instead of guessing with a sleep.
	entered chan struct{}
	// blockUntil, if non-nil, makes Dispatch wait for a receive on it before
	// returning — used to widen the migrate route's critical section in
	// concurrency tests (e.g. TestMigrateConcurrentRequestsAreSerialized).
	blockUntil chan struct{}

	mu    sync.Mutex
	calls []protocol.TeardownCommand
}

func (d *recordingWorkerDispatcher) Dispatch(_ context.Context, _ string, cmd interface{}) (protocol.CommandResult, error) {
	if d.entered != nil {
		close(d.entered)
	}
	if d.blockUntil != nil {
		<-d.blockUntil
	}
	d.mu.Lock()
	if tc, ok := cmd.(protocol.TeardownCommand); ok {
		d.calls = append(d.calls, tc)
	}
	d.mu.Unlock()
	return d.result, d.err
}

func (d *recordingWorkerDispatcher) callCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.calls)
}

func (d *recordingWorkerDispatcher) IsConnected(_ string) bool { return d.connected }

func createMigrateTestWorker(t *testing.T, app core.App) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("workers")
	if err != nil {
		t.Fatalf("find workers collection: %v", err)
	}
	worker := core.NewRecord(col)
	worker.Set("hostname", "worker-1")
	worker.Set("fingerprint", "fp-1")
	worker.Set("status", "ACTIVE")
	if err := app.Save(worker); err != nil {
		t.Fatalf("save worker: %v", err)
	}
	return worker
}

// seedRenderedRevision points STACKS_STORAGE_PATH at a fresh temp dir and
// writes a fake v1.yml under it for stackID — the on-disk precondition
// dispatchTeardownForMigration reads before it dispatches anything, mirroring
// what a real reconcile leaves behind.
func seedRenderedRevision(t *testing.T, stackID string, content string) {
	t.Helper()
	storage := t.TempDir()
	t.Setenv("STACKS_STORAGE_PATH", storage)
	dir := filepath.Join(storage, stackID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir revision dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "v1.yml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write revision file: %v", err)
	}
}

func TestDispatchTeardownForMigrationSkipsWhenNeverSynced(t *testing.T) {
	dispatcher := &recordingWorkerDispatcher{connected: true}
	app, _, _ := setupMigrateTestAppWithWorker(t, false, dispatcher)

	repo := createMigrateTestRepo(t, app, "repo-a", "https://example.com/a.git")
	worker := createMigrateTestWorker(t, app)
	stack := createMigrateTestStack(t, app, repo.Id, map[string]any{"worker": worker.Id})

	rr := routeRegistrar{app: app, workerSvc: dispatcher}
	output, err := rr.dispatchTeardownForMigration(context.Background(), stack)
	if err != nil {
		t.Fatalf("expected no error for a never-synced stack, got: %v", err)
	}
	if output != "" {
		t.Fatalf("expected empty output, got %q", output)
	}
	if len(dispatcher.calls) != 0 {
		t.Fatalf("expected no teardown dispatch for a never-synced stack, got %d calls", len(dispatcher.calls))
	}
}

// TestDispatchTeardownForMigrationMissingRevisionFileErrors covers a stack
// whose current_version is set but whose rendered revision file is gone
// from disk (e.g. STACKS_STORAGE_PATH wiped/misconfigured) — must error out
// rather than silently skip, since skipping there would proceed straight to
// mutating the record without ever tearing the old project down.
func TestDispatchTeardownForMigrationMissingRevisionFileErrors(t *testing.T) {
	dispatcher := &recordingWorkerDispatcher{connected: true}
	app, _, _ := setupMigrateTestAppWithWorker(t, false, dispatcher)
	t.Setenv("STACKS_STORAGE_PATH", t.TempDir()) // empty — no v1.yml written

	repo := createMigrateTestRepo(t, app, "repo-a", "https://example.com/a.git")
	worker := createMigrateTestWorker(t, app)
	stack := createMigrateTestStack(t, app, repo.Id, map[string]any{"worker": worker.Id, "current_version": 1})

	rr := routeRegistrar{app: app, workerSvc: dispatcher}
	_, err := rr.dispatchTeardownForMigration(context.Background(), stack)
	if err == nil {
		t.Fatal("expected an error when the rendered revision file is missing")
	}
	if len(dispatcher.calls) != 0 {
		t.Fatalf("expected no teardown dispatch when the revision file can't be read, got %d calls", len(dispatcher.calls))
	}
}

// TestDispatchTeardownForMigrationDispatchTransportError covers the
// transport-level dispatchErr branch (worker unreachable mid-call) —
// distinct from result.Error (TestMigrateTeardownFailureAbortsMutation),
// which is the worker successfully responding with a failure.
func TestDispatchTeardownForMigrationDispatchTransportError(t *testing.T) {
	dispatcher := &recordingWorkerDispatcher{connected: true, err: fmt.Errorf("connection reset")}
	app, _, _ := setupMigrateTestAppWithWorker(t, false, dispatcher)

	repo := createMigrateTestRepo(t, app, "repo-a", "https://example.com/a.git")
	worker := createMigrateTestWorker(t, app)
	stack := createMigrateTestStack(t, app, repo.Id, map[string]any{"worker": worker.Id, "current_version": 1})
	seedRenderedRevision(t, stack.Id, "name: myapp\nservices:\n  web:\n    image: nginx\n")

	rr := routeRegistrar{app: app, workerSvc: dispatcher}
	_, err := rr.dispatchTeardownForMigration(context.Background(), stack)
	if err == nil {
		t.Fatal("expected an error when the dispatch itself fails")
	}
	if !strings.Contains(err.Error(), "connection reset") {
		t.Fatalf("expected the transport error to be wrapped, got: %v", err)
	}
}

// TestResolveCurrentStackConfigMapReadsRenderedRevision covers the
// current_version>0 branch reading straight off disk (no docker) — the
// other route tests only exercise the current_version==0 fallback that
// shells out to `docker compose config`.
func TestResolveCurrentStackConfigMapReadsRenderedRevision(t *testing.T) {
	app, _, _ := setupMigrateTestApp(t, false)
	repo := createMigrateTestRepo(t, app, "repo-a", "https://example.com/a.git")
	stack := createMigrateTestStack(t, app, repo.Id, map[string]any{"current_version": 1})
	seedRenderedRevision(t, stack.Id, "name: myapp\nservices:\n  web:\n    image: nginx\n")

	rr := routeRegistrar{app: app}
	configMap, err := rr.resolveCurrentStackConfigMap(context.Background(), stack, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if configMap["name"] != "myapp" {
		t.Fatalf("expected name=myapp from the rendered revision, got %+v", configMap)
	}
}

func TestMigrateTeardownDispatchesBeforeMutationAndRecordsAudit(t *testing.T) {
	dispatcher := &recordingWorkerDispatcher{connected: true, result: protocol.CommandResult{Output: "removed 2 containers"}}
	app, mux, _ := setupMigrateTestAppWithWorker(t, false, dispatcher)

	sourceDir := t.TempDir()
	localGitFixture(t, sourceDir, map[string]string{"docker-compose.yml": "services:\n  web:\n    image: nginx\n"})
	sourceRepo := createMigrateTestRepo(t, app, "source-repo", sourceDir)

	targetDir := t.TempDir()
	localGitFixture(t, targetDir, map[string]string{"docker-compose.yml": "services:\n  web:\n    image: nginx\n"})
	targetRepo := createMigrateTestRepo(t, app, "target-repo", targetDir)

	worker := createMigrateTestWorker(t, app)
	stack := createMigrateTestStack(t, app, sourceRepo.Id, map[string]any{
		"worker":          worker.Id,
		"current_version": 1,
	})
	seedRenderedRevision(t, stack.Id, "name: myapp\nservices:\n  web:\n    image: nginx\n")

	body, _ := json.Marshal(migrateReq(targetRepo.Id, map[string]any{
		"compose_path":         ".",
		"compose_file":         "docker-compose.yml",
		"confirm":              true,
		"teardown_old_project": true,
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/custom/stacks/"+stack.Id+"/migrate", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}

	if len(dispatcher.calls) != 1 {
		t.Fatalf("expected exactly one teardown dispatch, got %d", len(dispatcher.calls))
	}
	if dispatcher.calls[0].StackID != stack.Id {
		t.Fatalf("expected teardown for stack %s, got %s", stack.Id, dispatcher.calls[0].StackID)
	}

	updated, err := app.FindRecordById("stacks", stack.Id)
	if err != nil {
		t.Fatalf("reload stack: %v", err)
	}
	if updated.GetString("repository") != targetRepo.Id {
		t.Fatalf("expected repository migrated to %s, got %s", targetRepo.Id, updated.GetString("repository"))
	}

	logs, err := app.FindAllRecords("audit_logs", dbx.HashExp{"action": "stack.migrate", "resource_id": stack.Id})
	if err != nil || len(logs) == 0 {
		t.Fatalf("expected a stack.migrate audit log, got %d records (err=%v)", len(logs), err)
	}
}

func TestMigrateTeardownFailureAbortsMutation(t *testing.T) {
	dispatcher := &recordingWorkerDispatcher{connected: true, result: protocol.CommandResult{Error: "compose down failed"}}
	app, mux, _ := setupMigrateTestAppWithWorker(t, false, dispatcher)

	sourceDir := t.TempDir()
	localGitFixture(t, sourceDir, map[string]string{"docker-compose.yml": "services:\n  web:\n    image: nginx\n"})
	sourceRepo := createMigrateTestRepo(t, app, "source-repo", sourceDir)

	targetDir := t.TempDir()
	localGitFixture(t, targetDir, map[string]string{"docker-compose.yml": "services:\n  web:\n    image: nginx\n"})
	targetRepo := createMigrateTestRepo(t, app, "target-repo", targetDir)

	worker := createMigrateTestWorker(t, app)
	stack := createMigrateTestStack(t, app, sourceRepo.Id, map[string]any{
		"worker":          worker.Id,
		"current_version": 1,
	})
	seedRenderedRevision(t, stack.Id, "name: myapp\nservices:\n  web:\n    image: nginx\n")

	body, _ := json.Marshal(migrateReq(targetRepo.Id, map[string]any{
		"compose_path":         ".",
		"compose_file":         "docker-compose.yml",
		"confirm":              true,
		"teardown_old_project": true,
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/custom/stacks/"+stack.Id+"/migrate", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when teardown fails, got %d: %s", rec.Code, rec.Body.String())
	}

	updated, err := app.FindRecordById("stacks", stack.Id)
	if err != nil {
		t.Fatalf("reload stack: %v", err)
	}
	if updated.GetString("repository") != sourceRepo.Id {
		t.Fatalf("expected repository unchanged after a failed teardown, got %s", updated.GetString("repository"))
	}

	logs, err := app.FindAllRecords("audit_logs", dbx.HashExp{"action": "stack.migrate", "resource_id": stack.Id})
	if err != nil {
		t.Fatalf("query audit logs: %v", err)
	}
	if len(logs) != 0 {
		t.Fatalf("expected no stack.migrate audit log after an aborted migration, got %d", len(logs))
	}
}

func TestMigrateTeardownRequiresWorkerOnline(t *testing.T) {
	dispatcher := &recordingWorkerDispatcher{connected: false}
	app, mux, _ := setupMigrateTestAppWithWorker(t, false, dispatcher)

	sourceDir := t.TempDir()
	localGitFixture(t, sourceDir, map[string]string{"docker-compose.yml": "services:\n  web:\n    image: nginx\n"})
	sourceRepo := createMigrateTestRepo(t, app, "source-repo", sourceDir)

	targetDir := t.TempDir()
	localGitFixture(t, targetDir, map[string]string{"docker-compose.yml": "services:\n  web:\n    image: nginx\n"})
	targetRepo := createMigrateTestRepo(t, app, "target-repo", targetDir)

	worker := createMigrateTestWorker(t, app)
	stack := createMigrateTestStack(t, app, sourceRepo.Id, map[string]any{
		"worker":          worker.Id,
		"current_version": 1,
	})
	seedRenderedRevision(t, stack.Id, "name: myapp\nservices:\n  web:\n    image: nginx\n")

	body, _ := json.Marshal(migrateReq(targetRepo.Id, map[string]any{
		"compose_path":         ".",
		"compose_file":         "docker-compose.yml",
		"confirm":              true,
		"teardown_old_project": true,
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/custom/stacks/"+stack.Id+"/migrate", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when the worker is offline, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(dispatcher.calls) != 0 {
		t.Fatalf("expected no teardown dispatch when the worker is offline, got %d", len(dispatcher.calls))
	}

	updated, err := app.FindRecordById("stacks", stack.Id)
	if err != nil {
		t.Fatalf("reload stack: %v", err)
	}
	if updated.GetString("repository") != sourceRepo.Id {
		t.Fatalf("expected repository unchanged when the worker is offline, got %s", updated.GetString("repository"))
	}
}

// TestMigrateConcurrentRequestsAreSerialized is the end-to-end proof for the
// TOCTOU fix: TryLockStack must be held across the whole teardown+mutation
// critical section, not just probed once at the top of the handler (the old
// IsSyncing-only guard). A dispatcher that blocks inside the teardown
// dispatch widens that section long enough for a second concurrent request
// to observe the lock still held and get rejected — deterministically, via
// the entered/blockUntil channels, no sleeps.
func TestMigrateConcurrentRequestsAreSerialized(t *testing.T) {
	entered := make(chan struct{})
	blockUntil := make(chan struct{})
	dispatcher := &recordingWorkerDispatcher{connected: true, entered: entered, blockUntil: blockUntil}
	app, mux, _ := setupMigrateTestAppWithWorker(t, false, dispatcher)

	sourceDir := t.TempDir()
	localGitFixture(t, sourceDir, map[string]string{"docker-compose.yml": "services:\n  web:\n    image: nginx\n"})
	sourceRepo := createMigrateTestRepo(t, app, "source-repo", sourceDir)

	targetDir := t.TempDir()
	localGitFixture(t, targetDir, map[string]string{"docker-compose.yml": "services:\n  web:\n    image: nginx\n"})
	targetRepo := createMigrateTestRepo(t, app, "target-repo", targetDir)

	worker := createMigrateTestWorker(t, app)
	stack := createMigrateTestStack(t, app, sourceRepo.Id, map[string]any{"worker": worker.Id, "current_version": 1})
	seedRenderedRevision(t, stack.Id, "name: myapp\nservices:\n  web:\n    image: nginx\n")

	body, _ := json.Marshal(migrateReq(targetRepo.Id, map[string]any{
		"compose_path":         ".",
		"compose_file":         "docker-compose.yml",
		"confirm":              true,
		"teardown_old_project": true,
	}))
	doRequest := func() int {
		req := httptest.NewRequest(http.MethodPost, "/api/custom/stacks/"+stack.Id+"/migrate", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code
	}

	var wg sync.WaitGroup
	var firstCode int
	wg.Add(1)
	go func() {
		defer wg.Done()
		firstCode = doRequest()
	}()

	<-entered // the first request is now inside dispatchTeardownForMigration, holding the lock
	secondCode := doRequest()
	close(blockUntil) // let the first request finish
	wg.Wait()

	if secondCode != http.StatusConflict {
		t.Fatalf("expected the second concurrent request to get 409, got %d", secondCode)
	}
	if firstCode != http.StatusAccepted {
		t.Fatalf("expected the first request to get 202, got %d", firstCode)
	}
	if dispatcher.callCount() != 1 {
		t.Fatalf("expected exactly one teardown dispatch, got %d", dispatcher.callCount())
	}
}
