package sync

import (
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

func newSchedulerTestStack(t *testing.T) *core.Record {
	t.Helper()
	col := core.NewBaseCollection("stacks_scheduler_test")
	col.Fields.Add(&core.NumberField{Name: "sync_interval_seconds"})
	return core.NewRecord(col)
}

func newSchedulerTestRepo(t *testing.T) *core.Record {
	t.Helper()
	col := core.NewBaseCollection("repositories_scheduler_test")
	col.Fields.Add(&core.NumberField{Name: "sync_interval_seconds"})
	return core.NewRecord(col)
}

func TestResolveSyncInterval(t *testing.T) {
	t.Setenv("SCAN_PERIOD", "")

	t.Run("PositiveOverrideUsed", func(t *testing.T) {
		stack := newSchedulerTestStack(t)
		stack.Set("sync_interval_seconds", 45)

		got := resolveSyncInterval(stack)
		if want := 45 * time.Second; got != want {
			t.Errorf("resolveSyncInterval() = %v, want %v", got, want)
		}
	})

	t.Run("ZeroFallsBackToGlobalScanPeriod", func(t *testing.T) {
		stack := newSchedulerTestStack(t)
		stack.Set("sync_interval_seconds", 0)

		got := resolveSyncInterval(stack)
		if want := 10 * time.Second; got != want {
			t.Errorf("resolveSyncInterval() = %v, want %v (default SCAN_PERIOD)", got, want)
		}
	})

	t.Run("NegativeFallsBackToGlobalScanPeriod", func(t *testing.T) {
		stack := newSchedulerTestStack(t)
		stack.Set("sync_interval_seconds", -5)

		got := resolveSyncInterval(stack)
		if want := 10 * time.Second; got != want {
			t.Errorf("resolveSyncInterval() = %v, want %v (default SCAN_PERIOD)", got, want)
		}
	})

	t.Run("FallbackHonorsCustomScanPeriodEnv", func(t *testing.T) {
		t.Setenv("SCAN_PERIOD", "30")

		stack := newSchedulerTestStack(t)

		got := resolveSyncInterval(stack)
		if want := 30 * time.Second; got != want {
			t.Errorf("resolveSyncInterval() = %v, want %v", got, want)
		}
	})
}

func TestResolveRepoSyncInterval(t *testing.T) {
	t.Setenv("SCAN_PERIOD", "")

	t.Run("PositiveOverrideUsed", func(t *testing.T) {
		repo := newSchedulerTestRepo(t)
		repo.Set("sync_interval_seconds", 90)

		got := resolveRepoSyncInterval(repo)
		if want := 90 * time.Second; got != want {
			t.Errorf("resolveRepoSyncInterval() = %v, want %v", got, want)
		}
	})

	t.Run("ZeroFallsBackToGlobalScanPeriod", func(t *testing.T) {
		repo := newSchedulerTestRepo(t)
		repo.Set("sync_interval_seconds", 0)

		got := resolveRepoSyncInterval(repo)
		if want := 10 * time.Second; got != want {
			t.Errorf("resolveRepoSyncInterval() = %v, want %v (default SCAN_PERIOD)", got, want)
		}
	})

	t.Run("FallbackHonorsCustomScanPeriodEnv", func(t *testing.T) {
		t.Setenv("SCAN_PERIOD", "25")

		repo := newSchedulerTestRepo(t)

		got := resolveRepoSyncInterval(repo)
		if want := 25 * time.Second; got != want {
			t.Errorf("resolveRepoSyncInterval() = %v, want %v", got, want)
		}
	})
}

func newSchedulerTestApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })
	return app
}

// TestRegisterRepoAndUnregisterRepoManageTickerLifecycle drives the repo-level
// ticker's lifecycle map directly: registering a repo must (re)start its job,
// and unregistering must remove it, mirroring the stack ticker's contract
// (RegisterStack/UnregisterStack) so stacks sharing a repository still get
// their fetches coordinated by a single ticker per repository.
func TestRegisterRepoAndUnregisterRepoManageTickerLifecycle(t *testing.T) {
	t.Setenv("SCAN_PERIOD", "3600") // keep the ticker from ever firing during the test

	app := newSchedulerTestApp(t)
	s := NewScheduler(app, &fakeDispatcher{})
	defer s.Shutdown()

	col := core.NewBaseCollection("repositories")
	col.Fields.Add(&core.NumberField{Name: "sync_interval_seconds"})
	repo := core.NewRecord(col)
	repo.Id = "repo-lifecycle-test"

	s.RegisterRepo(repo)
	s.mu.Lock()
	_, ok := s.repoJobs[repo.Id]
	s.mu.Unlock()
	if !ok {
		t.Fatal("RegisterRepo did not add a ticker job for the repo")
	}

	// Re-registering (e.g. after an interval change) must replace, not leak,
	// the previous job.
	s.RegisterRepo(repo)
	s.mu.Lock()
	_, stillOk := s.repoJobs[repo.Id]
	jobCount := len(s.repoJobs)
	s.mu.Unlock()
	if !stillOk || jobCount != 1 {
		t.Fatalf("expected exactly 1 job for repo after re-registering, got %d (present=%v)", jobCount, stillOk)
	}

	s.UnregisterRepo(repo.Id)
	s.mu.Lock()
	_, stillPresent := s.repoJobs[repo.Id]
	s.mu.Unlock()
	if stillPresent {
		t.Fatal("UnregisterRepo did not remove the repo's ticker job")
	}
}
