package sync

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
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

// stubApp overrides FindAllRecords on top of a real test app so Start()'s
// error paths can be exercised without the wireops collections existing.
type stubApp struct {
	core.App
	stacks   []*core.Record
	repos    []*core.Record
	stackErr error
	repoErr  error
}

func (a *stubApp) FindAllRecords(collectionModelOrIdentifier any, exprs ...dbx.Expression) ([]*core.Record, error) {
	switch collectionModelOrIdentifier {
	case "stacks":
		return a.stacks, a.stackErr
	case "repositories":
		return a.repos, a.repoErr
	}
	return a.App.FindAllRecords(collectionModelOrIdentifier, exprs...)
}

func newStubStack(id string) *core.Record {
	col := core.NewBaseCollection("stacks")
	col.Fields.Add(&core.NumberField{Name: "sync_interval_seconds"})
	col.Fields.Add(&core.TextField{Name: "status"})
	rec := core.NewRecord(col)
	rec.Id = id
	return rec
}

func newStubRepo(id string) *core.Record {
	col := core.NewBaseCollection("repositories")
	col.Fields.Add(&core.NumberField{Name: "sync_interval_seconds"})
	rec := core.NewRecord(col)
	rec.Id = id
	return rec
}

// TestStartLeavesNoJobsRunningWhenRepositoryLoadFails covers the startup error
// path: if the repositories query fails, Start must return before spawning any
// ticker goroutine. Starting the stack jobs first would leave them running with
// the caller holding an error and no handle to stop them.
func TestStartLeavesNoJobsRunningWhenRepositoryLoadFails(t *testing.T) {
	t.Setenv("SCAN_PERIOD", "3600")

	app := &stubApp{
		App:     newSchedulerTestApp(t),
		stacks:  []*core.Record{newStubStack("stack-a"), newStubStack("stack-b")},
		repoErr: errors.New("repositories query failed"),
	}
	s := NewScheduler(app, &fakeDispatcher{})
	defer s.Shutdown()

	if err := s.Start(); err == nil {
		t.Fatal("Start() = nil, want the repositories query error")
	}

	s.mu.Lock()
	stackJobs, repoJobs := len(s.jobs), len(s.repoJobs)
	s.mu.Unlock()
	if stackJobs != 0 || repoJobs != 0 {
		t.Fatalf("Start() left jobs running after failing: %d stack job(s), %d repo job(s)", stackJobs, repoJobs)
	}
}

// TestStartStartsBothJobSetsOnSuccess guards the reordering above against a
// regression that skips one of the two job sets.
func TestStartStartsBothJobSetsOnSuccess(t *testing.T) {
	t.Setenv("SCAN_PERIOD", "3600")

	paused := newStubStack("stack-paused")
	paused.Set("status", "paused")

	app := &stubApp{
		App:    newSchedulerTestApp(t),
		stacks: []*core.Record{newStubStack("stack-a"), paused},
		repos:  []*core.Record{newStubRepo("repo-a"), newStubRepo("repo-b")},
	}
	s := NewScheduler(app, &fakeDispatcher{})
	defer s.Shutdown()

	if err := s.Start(); err != nil {
		t.Fatalf("Start() = %v, want nil", err)
	}

	s.mu.Lock()
	stackJobs, repoJobs := len(s.jobs), len(s.repoJobs)
	s.mu.Unlock()
	if stackJobs != 1 {
		t.Errorf("got %d stack job(s), want 1 (the paused stack must be skipped)", stackJobs)
	}
	if repoJobs != 2 {
		t.Errorf("got %d repo job(s), want 2", repoJobs)
	}
}

// TestIntervalFromSecondsClampsOutOfRangeValues pins the boundary behaviour of
// the seconds -> time.Duration conversion. Anything above ~9.2e9 seconds
// overflows int64 nanoseconds and wraps to a non-positive duration, which
// panics time.NewTicker; the clamp is what keeps repo/stack job registration
// from taking the whole scheduler down.
func TestIntervalFromSecondsClampsOutOfRangeValues(t *testing.T) {
	t.Setenv("SCAN_PERIOD", "")

	tests := []struct {
		name    string
		seconds int
		want    time.Duration
	}{
		{"Zero falls back to SCAN_PERIOD", 0, 10 * time.Second},
		{"Negative falls back to SCAN_PERIOD", -1, 10 * time.Second},
		{"One second is honoured", 1, time.Second},
		{"Max is honoured exactly", maxSyncIntervalSeconds, maxSyncIntervalSeconds * time.Second},
		{"Just over max is clamped", maxSyncIntervalSeconds + 1, maxSyncIntervalSeconds * time.Second},
		{"Overflow value is clamped", math.MaxInt64 / int(time.Millisecond), maxSyncIntervalSeconds * time.Second},
		{"MaxInt64 is clamped", math.MaxInt64, maxSyncIntervalSeconds * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := intervalFromSeconds(tt.seconds, "test")
			if got != tt.want {
				t.Fatalf("intervalFromSeconds(%d) = %v, want %v", tt.seconds, got, tt.want)
			}
			if got <= 0 {
				t.Fatalf("intervalFromSeconds(%d) = %v, which would panic time.NewTicker", tt.seconds, got)
			}
		})
	}
}

// TestRegisterRepoSurvivesOverflowingInterval is the end-to-end version of the
// clamp: registering a repo whose stored interval overflows must start a ticker
// instead of panicking inside time.NewTicker.
func TestRegisterRepoSurvivesOverflowingInterval(t *testing.T) {
	app := newSchedulerTestApp(t)
	s := NewScheduler(app, &fakeDispatcher{})
	defer s.Shutdown()

	repo := newStubRepo("repo-overflow")
	repo.Set("sync_interval_seconds", math.MaxInt64)

	s.RegisterRepo(repo)

	s.mu.Lock()
	_, ok := s.repoJobs[repo.Id]
	s.mu.Unlock()
	if !ok {
		t.Fatal("RegisterRepo did not start a ticker for a repo with an overflowing interval")
	}
}
