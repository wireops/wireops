package sync

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/wireops/wireops/internal/manifest"
)

func newScheduleTestStack(t *testing.T, timeoutSeconds, intervalSeconds int) *core.Record {
	t.Helper()
	col := core.NewBaseCollection("stacks_schedule_test")
	col.Fields.Add(&core.TextField{Name: "config_source"})
	col.Fields.Add(&core.NumberField{Name: "deploy_timeout_seconds"})
	col.Fields.Add(&core.NumberField{Name: "sync_interval_seconds"})
	rec := core.NewRecord(col)
	rec.Set("deploy_timeout_seconds", timeoutSeconds)
	rec.Set("sync_interval_seconds", intervalSeconds)
	return rec
}

func TestScheduleRefreshApplies(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   bool
	}{
		{name: "ComposeEmbedded", source: "compose_embedded", want: true},
		{name: "WireopsFile", source: "wireops_file", want: true},
		{name: "ManualStackKeepsItsOwnSchedule", source: "manual", want: false},
		{name: "EmptySourceIsNotFileManaged", source: "", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := scheduleRefreshApplies(tc.source); got != tc.want {
				t.Fatalf("scheduleRefreshApplies(%q) = %v, want %v", tc.source, got, tc.want)
			}
		})
	}
}

func TestScheduleChangesFor(t *testing.T) {
	cases := []struct {
		name            string
		storedTimeout   int
		storedInterval  int
		def             manifest.Definition
		wantChanges     map[string][2]int
		wantFinalValues [2]int
	}{
		{
			name:            "AppliesBothWhenGitDiverges",
			storedTimeout:   60,
			storedInterval:  10,
			def:             manifest.Definition{DeployTimeoutSeconds: 180, SyncIntervalSeconds: 45},
			wantChanges:     map[string][2]int{"deploy_timeout_seconds": {60, 180}, "sync_interval_seconds": {10, 45}},
			wantFinalValues: [2]int{180, 45},
		},
		{
			name:            "AppliesOnlyTheFieldThatDiffers",
			storedTimeout:   60,
			storedInterval:  45,
			def:             manifest.Definition{DeployTimeoutSeconds: 180, SyncIntervalSeconds: 45},
			wantChanges:     map[string][2]int{"deploy_timeout_seconds": {60, 180}},
			wantFinalValues: [2]int{180, 45},
		},
		{
			name:            "NoChangesWhenAlreadyConverged",
			storedTimeout:   180,
			storedInterval:  45,
			def:             manifest.Definition{DeployTimeoutSeconds: 180, SyncIntervalSeconds: 45},
			wantChanges:     map[string][2]int{},
			wantFinalValues: [2]int{180, 45},
		},
		{
			// Dropping `timeout:` from the file is a deliberate choice to
			// inherit the global default, so zero has to win over the stored
			// value rather than being treated as "nothing declared".
			name:            "ConvergesToZeroWhenTheKeyWasRemoved",
			storedTimeout:   60,
			storedInterval:  10,
			def:             manifest.Definition{},
			wantChanges:     map[string][2]int{"deploy_timeout_seconds": {60, 0}, "sync_interval_seconds": {10, 0}},
			wantFinalValues: [2]int{0, 0},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stack := newScheduleTestStack(t, tc.storedTimeout, tc.storedInterval)
			def := tc.def

			changes := scheduleChangesFor(stack, &def)

			if len(changes) != len(tc.wantChanges) {
				t.Fatalf("got %d changes (%v), want %d", len(changes), changes, len(tc.wantChanges))
			}
			for _, change := range changes {
				want, ok := tc.wantChanges[change.Field]
				if !ok {
					t.Fatalf("unexpected change for %q: %v", change.Field, change)
				}
				if change.From != want[0] || change.To != want[1] {
					t.Fatalf("%s = %d -> %d, want %d -> %d", change.Field, change.From, change.To, want[0], want[1])
				}
			}
			if got := stack.GetInt("deploy_timeout_seconds"); got != tc.wantFinalValues[0] {
				t.Fatalf("deploy_timeout_seconds = %d, want %d", got, tc.wantFinalValues[0])
			}
			if got := stack.GetInt("sync_interval_seconds"); got != tc.wantFinalValues[1] {
				t.Fatalf("sync_interval_seconds = %d, want %d", got, tc.wantFinalValues[1])
			}
		})
	}
}

func TestFormatScheduleChanges(t *testing.T) {
	got := formatScheduleChanges([]scheduleChange{
		{Field: "deploy_timeout_seconds", From: 60, To: 180},
		{Field: "sync_interval_seconds", From: 10, To: 45},
	})
	want := "deploy_timeout_seconds 60 -> 180, sync_interval_seconds 10 -> 45"
	if got != want {
		t.Fatalf("formatScheduleChanges() = %q, want %q", got, want)
	}
}

// newDefinitionTestStack builds a stack record carrying the fields
// parseStackDefinition reads to resolve its own workDir and compose filename
// now that it no longer takes them from the render phase.
func newDefinitionTestStack(t *testing.T, configSource, composePath, composeFile string) *core.Record {
	t.Helper()
	col := core.NewBaseCollection("stacks_definition_test")
	col.Fields.Add(&core.TextField{Name: "config_source"})
	col.Fields.Add(&core.TextField{Name: "compose_path"})
	col.Fields.Add(&core.TextField{Name: "compose_file"})
	col.Fields.Add(&core.TextField{Name: "wireops_file_path"})
	col.Fields.Add(&core.SelectField{Name: "status", Values: []string{"active", "syncing", "paused", "error", "pending", "degraded"}})
	col.Fields.Add(&core.NumberField{Name: "deploy_timeout_seconds"})
	col.Fields.Add(&core.NumberField{Name: "sync_interval_seconds"})

	rec := core.NewRecord(col)
	rec.Set("config_source", configSource)
	rec.Set("compose_path", composePath)
	rec.Set("compose_file", composeFile)
	rec.Set("status", "active")
	return rec
}

// composeWithTimeout is a minimal compose file carrying an x-wireops block,
// which is what a compose_embedded stack's definition lives in.
func composeWithTimeout(timeout string) []byte {
	return []byte("x-wireops:\n  version: wireops.v1\n  name: app\n  timeout: " + timeout +
		"\n  sync:\n    interval: 45s\nservices:\n  app:\n    image: nginx:1.27\n")
}

func writeComposeFile(t *testing.T, dir, name string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// TestParseStackDefinitionResolvesComposeFileFromTheStackRecord covers the
// half of issue #178 item 1 that made the fix possible: the refresh now runs
// before the render phase, so parseStackDefinition has to derive workDir and
// the compose filename from the stack record itself. The signature no longer
// accepts them, which is what stops the call site from drifting back below
// the skip gates it has to run ahead of.
//
// The last two cases are the reason this does not reuse resolveComposeFile:
// an unreadable definition must come back as a plain error, leaving the stack
// record untouched, rather than marking it errored on every cron tick.
func TestParseStackDefinitionResolvesComposeFileFromTheStackRecord(t *testing.T) {
	const repoID = "repo-1"

	cases := []struct {
		name        string
		composePath string
		composeFile string
		// writeTo is the directory, relative to the repo root, the compose
		// file is actually written to ("" = the repo root itself).
		writeTo     string
		writeAs     string
		wantErr     bool
		wantTimeout int
	}{
		{
			name:        "DefaultsToDockerComposeYmlAtTheRepoRoot",
			writeAs:     "docker-compose.yml",
			wantTimeout: 180,
		},
		{
			name:        "HonoursComposePathSubdirectory",
			composePath: "stacks/app",
			writeTo:     "stacks/app",
			writeAs:     "docker-compose.yml",
			wantTimeout: 180,
		},
		{
			name:        "HonoursCustomComposeFileName",
			composeFile: "compose.yaml",
			writeAs:     "compose.yaml",
			wantTimeout: 180,
		},
		{
			name:    "MissingComposeFileIsAPlainError",
			writeAs: "",
			wantErr: true,
		},
		{
			name:    "ComposeFileWithoutAnXWireopsBlockIsAnError",
			writeAs: "docker-compose.yml",
			wantErr: true,
		},
		{
			name:        "TraversingComposeFileNameIsRejected",
			composeFile: "../../etc/passwd.yml",
			writeAs:     "docker-compose.yml",
			wantErr:     true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			workspace := t.TempDir()
			t.Setenv("REPOS_WORKSPACE", workspace)
			repoRoot := filepath.Join(workspace, repoID)
			if err := os.MkdirAll(repoRoot, 0o755); err != nil {
				t.Fatalf("MkdirAll(%s): %v", repoRoot, err)
			}

			if tc.writeAs != "" {
				content := composeWithTimeout("3m")
				if tc.name == "ComposeFileWithoutAnXWireopsBlockIsAnError" {
					content = []byte("services:\n  app:\n    image: nginx:1.27\n")
				}
				writeComposeFile(t, filepath.Join(repoRoot, tc.writeTo), tc.writeAs, content)
			}

			stack := newDefinitionTestStack(t, "compose_embedded", tc.composePath, tc.composeFile)
			r := &Reconciler{}

			def, err := r.parseStackDefinition(stack, "compose_embedded", repoID)
			if tc.wantErr {
				if err == nil {
					t.Fatal("parseStackDefinition succeeded, want an error")
				}
				// The whole point of not reusing resolveComposeFile: the
				// failure must not touch the stack record.
				if got := stack.GetString("status"); got != "active" {
					t.Fatalf("stack status = %q after a failed parse, want it left at %q", got, "active")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseStackDefinition: %v", err)
			}
			if def.DeployTimeoutSeconds != tc.wantTimeout {
				t.Fatalf("DeployTimeoutSeconds = %d, want %d", def.DeployTimeoutSeconds, tc.wantTimeout)
			}
			if def.SyncIntervalSeconds != 45 {
				t.Fatalf("SyncIntervalSeconds = %d, want 45", def.SyncIntervalSeconds)
			}
		})
	}
}

// TestRefreshScheduleFromDefinitionAppliesCommittedTimeout is the end-to-end
// counterpart: a stack stored at the global default picks up the `timeout:`
// its compose file carries in git, and the save goes through the
// stackdef.WithBypass path (the immutability hook itself is covered in
// internal/hooks and internal/stackdef).
func TestRefreshScheduleFromDefinitionAppliesCommittedTimeout(t *testing.T) {
	const repoID = "repo-1"

	workspace := t.TempDir()
	t.Setenv("REPOS_WORKSPACE", workspace)
	writeComposeFile(t, filepath.Join(workspace, repoID), "docker-compose.yml", composeWithTimeout("3m"))

	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	stacks := core.NewBaseCollection("stacks")
	stacks.Fields.Add(&core.TextField{Name: "name"})
	stacks.Fields.Add(&core.TextField{Name: "config_source"})
	stacks.Fields.Add(&core.TextField{Name: "compose_path"})
	stacks.Fields.Add(&core.TextField{Name: "compose_file"})
	stacks.Fields.Add(&core.NumberField{Name: "deploy_timeout_seconds"})
	stacks.Fields.Add(&core.NumberField{Name: "sync_interval_seconds"})
	if err := app.Save(stacks); err != nil {
		t.Fatalf("failed to create stacks collection: %v", err)
	}

	stack := core.NewRecord(stacks)
	stack.Set("name", "app")
	stack.Set("config_source", "compose_embedded")
	stack.Set("deploy_timeout_seconds", 60)
	stack.Set("sync_interval_seconds", 0)
	if err := app.Save(stack); err != nil {
		t.Fatalf("failed to create stack: %v", err)
	}

	r := &Reconciler{app: app}
	changes, err := r.refreshScheduleFromDefinition(stack, repoID)
	if err != nil {
		t.Fatalf("refreshScheduleFromDefinition: %v", err)
	}
	if len(changes) != 2 {
		t.Fatalf("got %d changes (%v), want 2", len(changes), formatScheduleChanges(changes))
	}

	reloaded, err := app.FindRecordById("stacks", stack.Id)
	if err != nil {
		t.Fatalf("reload stack: %v", err)
	}
	if got := reloaded.GetInt("deploy_timeout_seconds"); got != 180 {
		t.Fatalf("persisted deploy_timeout_seconds = %d, want 180", got)
	}
	if got := reloaded.GetInt("sync_interval_seconds"); got != 45 {
		t.Fatalf("persisted sync_interval_seconds = %d, want 45", got)
	}

	// Converged: a second pass has nothing left to apply.
	again, err := r.refreshScheduleFromDefinition(reloaded, repoID)
	if err != nil {
		t.Fatalf("second refreshScheduleFromDefinition: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("second pass applied %v, want no changes", formatScheduleChanges(again))
	}
}

// TestRefreshScheduleUnderRepoLockWaitsForTheRepoWriteLock pins the lock the
// new call site needs. It reads the shared checkout (workspace/<repoID>),
// which every git fetch rewrites, and it now runs ahead of the render phase's
// own read lock — so it has to take that lock itself.
func TestRefreshScheduleUnderRepoLockWaitsForTheRepoWriteLock(t *testing.T) {
	const repoID = "repo-1"

	workspace := t.TempDir()
	t.Setenv("REPOS_WORKSPACE", workspace)

	r := &Reconciler{}
	// A stack whose config_source is not file-managed returns before any IO,
	// so this exercises the lock and nothing else.
	stack := newDefinitionTestStack(t, "manual", "", "")

	repoMu := r.repoMutex(repoID)
	repoMu.Lock()

	done := make(chan struct{})
	go func() {
		r.refreshScheduleUnderRepoLock(stack, repoID, "stack-1")
		close(done)
	}()

	select {
	case <-done:
		repoMu.Unlock()
		t.Fatal("refreshScheduleUnderRepoLock completed while the repo write lock was held")
	case <-time.After(100 * time.Millisecond):
	}

	repoMu.Unlock()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("refreshScheduleUnderRepoLock did not complete after the repo write lock was released")
	}

	// Released again, not held until the render phase: an RWMutex gives
	// writers priority, so a still-held read lock would deadlock the
	// reconcile's own RLock further down.
	if !repoMu.TryLock() {
		t.Fatal("repo lock still held after refreshScheduleUnderRepoLock returned")
	}
	repoMu.Unlock()
}
