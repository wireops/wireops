package sync

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"github.com/wireops/wireops/internal/config"
	"github.com/wireops/wireops/internal/contextutil"
	"github.com/wireops/wireops/internal/notify"
)

type Scheduler struct {
	mu         sync.Mutex
	jobs       map[string]context.CancelFunc // keyed by stack ID
	repoJobs   map[string]context.CancelFunc // keyed by repository ID
	reconciler *Reconciler
	app        core.App

	// rootCtx / rootCancel are used for a global graceful shutdown.
	// Shutdown() cancels rootCtx, causing all goroutines to stop.
	rootCtx    context.Context
	rootCancel context.CancelFunc

	syncSemaphore chan struct{} // global limit for concurrent reconciles/syncs
}

func NewScheduler(app core.App, dispatcher WorkerDispatcher) *Scheduler {
	notifier := notify.New(app)
	rootCtx, rootCancel := context.WithCancel(context.Background())

	limit := 5
	if limitStr := os.Getenv("MAX_CONCURRENT_SYNCS"); limitStr != "" {
		if val, err := strconv.Atoi(limitStr); err == nil && val > 0 {
			limit = val
		}
	}

	return &Scheduler{
		jobs:          make(map[string]context.CancelFunc),
		repoJobs:      make(map[string]context.CancelFunc),
		reconciler:    NewReconciler(app, notifier, dispatcher),
		app:           app,
		rootCtx:       rootCtx,
		rootCancel:    rootCancel,
		syncSemaphore: make(chan struct{}, limit),
	}
}

// Shutdown cancels the root context, signalling all background goroutines to stop.
// Should be called when the application is terminating.
func (s *Scheduler) Shutdown() {
	log.Printf("[scheduler] shutdown: cancelling all background jobs")
	s.rootCancel()
}

// safeRun executes fn, recovering from any panics. Errors and panics that occur
// after the context is done are suppressed, as they are expected during shutdown.
func (s *Scheduler) safeRun(ctx context.Context, label string, fn func() error) {
	defer func() {
		if rec := recover(); rec != nil {
			if ctx.Err() != nil {
				log.Printf("[scheduler] %s interrupted by shutdown", label)
			} else {
				log.Printf("[scheduler] panic in %s: %v", label, rec)
			}
		}
	}()
	if ctx.Err() != nil {
		return
	}
	if err := fn(); err != nil && ctx.Err() == nil {
		log.Printf("[scheduler] error in %s: %v", label, err)
	}
}

// Start boots every cron ticker. Both record sets are loaded up front so a
// failure to read either one returns before any goroutine is spawned —
// starting stack jobs first would leave them running with no way for the
// caller to stop them after the error.
func (s *Scheduler) Start() error {
	stacks, err := s.app.FindAllRecords("stacks")
	if err != nil {
		return err
	}
	repos, err := s.app.FindAllRecords("repositories")
	if err != nil {
		return err
	}

	for _, stack := range stacks {
		if stack.GetString("status") != "paused" {
			s.startJob(stack)
		}
	}
	for _, repo := range repos {
		s.startRepoJob(repo)
	}
	return nil
}

func (s *Scheduler) RegisterStack(stack *core.Record) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cancel, ok := s.jobs[stack.Id]; ok {
		cancel()
	}
	if stack.GetString("status") == "paused" {
		delete(s.jobs, stack.Id)
		return
	}
	s.startJobLocked(stack)
}

func (s *Scheduler) UnregisterStack(stackID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cancel, ok := s.jobs[stackID]; ok {
		cancel()
		delete(s.jobs, stackID)
	}
}

// RegisterRepo (re)starts the repo-level fetch ticker for repo, replacing
// any previously running one (e.g. after a sync_interval_seconds change).
func (s *Scheduler) RegisterRepo(repo *core.Record) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cancel, ok := s.repoJobs[repo.Id]; ok {
		cancel()
	}
	s.startRepoJobLocked(repo)
}

func (s *Scheduler) UnregisterRepo(repoID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cancel, ok := s.repoJobs[repoID]; ok {
		cancel()
		delete(s.repoJobs, repoID)
	}
}

// RemoveRepoWorkspace stops the repository's fetch ticker and then deletes its
// on-disk working tree. Cancelling the ticker only stops future ticks — a
// fetch already running keeps writing into that directory — so the removal
// itself waits on the reconciler's per-repo lock. Every caller that deletes or
// replaces workspace/<repoID> should go through here rather than calling
// os.RemoveAll directly.
func (s *Scheduler) RemoveRepoWorkspace(repoID string) error {
	s.UnregisterRepo(repoID)
	return s.reconciler.RemoveRepoWorkspace(repoID)
}

func (s *Scheduler) TriggerSync(stackID, trigger string, queueTotal int, userID string) {
	ctx := contextutil.WithUserID(s.rootCtx, userID)
	go s.safeRun(ctx, fmt.Sprintf("sync[%s] trigger=%s", stackID, trigger), func() error {
		select {
		case s.syncSemaphore <- struct{}{}:
			defer func() { <-s.syncSemaphore }()
		case <-ctx.Done():
			return ctx.Err()
		}
		return s.reconciler.ReconcileStack(ctx, stackID, trigger, queueTotal)
	})
}

func (s *Scheduler) TriggerRollback(stackID, commitSHA string, userID string) {
	ctx := contextutil.WithUserID(s.rootCtx, userID)
	go s.safeRun(ctx, fmt.Sprintf("rollback[%s]", stackID), func() error {
		select {
		case s.syncSemaphore <- struct{}{}:
			defer func() { <-s.syncSemaphore }()
		case <-ctx.Done():
			return ctx.Err()
		}
		return s.reconciler.RollbackStack(ctx, stackID, commitSHA)
	})
}

// TriggerForceRedeploy runs a force redeploy with the given recreate options.
// Any render_overrides persisted on the stack record are reapplied automatically
// by ForceRedeployStack, same as every other reconcile path.
func (s *Scheduler) TriggerForceRedeploy(stackID string, recreateContainers, recreateVolumes, recreateNetworks, pauseAfter bool, userID string) {
	ctx := contextutil.WithUserID(s.rootCtx, userID)
	go s.safeRun(ctx, fmt.Sprintf("force-redeploy[%s]", stackID), func() error {
		select {
		case s.syncSemaphore <- struct{}{}:
			defer func() { <-s.syncSemaphore }()
		case <-ctx.Done():
			return ctx.Err()
		}
		return s.reconciler.ForceRedeployStack(ctx, stackID, recreateContainers, recreateVolumes, recreateNetworks, pauseAfter)
	})
}

// LoadStackEnvVars resolves the stack's effective env vars (repo .env + secret-backed
// values), for callers outside the sync package that need to reproduce the same
// resolution the renderer uses (e.g. the render-overrides diff endpoint).
func (s *Scheduler) LoadStackEnvVars(ctx context.Context, stackID string) ([]string, error) {
	return s.reconciler.loadEnvVars(ctx, stackID)
}

func (s *Scheduler) TriggerTransfer(stackID, targetWorkerID string, userID string) {
	ctx := contextutil.WithUserID(s.rootCtx, userID)
	go s.safeRun(ctx, fmt.Sprintf("transfer[%s]", stackID), func() error {
		select {
		case s.syncSemaphore <- struct{}{}:
			defer func() { <-s.syncSemaphore }()
		case <-ctx.Done():
			return ctx.Err()
		}
		return s.reconciler.TransferStack(ctx, stackID, targetWorkerID)
	})
}

// TriggerPendingReconciles finds any pending reconnects for the given worker and triggers
// them, keeping only the most recent event per stack.
func (s *Scheduler) TriggerPendingReconciles(workerID string) {
	ctx := s.rootCtx
	go s.safeRun(ctx, fmt.Sprintf("pending-reconciles[worker=%s]", workerID), func() error {
		type pendingEvent struct {
			Record  *core.Record
			Trigger string
			Created time.Time
		}

		records, err := s.app.FindAllRecords("stack_pending_reconciles")
		if err != nil {
			return fmt.Errorf("failed to fetch pending reconciles: %w", err)
		}

		stackEvents := make(map[string]pendingEvent)
		for _, rec := range records {
			stackID := rec.GetString("stack")
			stackRec, err := s.app.FindRecordById("stacks", stackID)
			if err != nil || stackRec.GetString("worker") != workerID {
				continue
			}

			created := rec.GetDateTime("created").Time()
			if existing, ok := stackEvents[stackID]; !ok || created.After(existing.Created) {
				if ok {
					if err := s.app.Delete(existing.Record); err != nil {
						return fmt.Errorf("failed to delete superseded pending reconcile %s for stack %s: %w", existing.Record.Id, stackID, err)
					}
				}
				stackEvents[stackID] = pendingEvent{
					Record:  rec,
					Trigger: rec.GetString("trigger"),
					Created: created,
				}
			} else {
				if err := s.app.Delete(rec); err != nil {
					return fmt.Errorf("failed to delete stale pending reconcile %s for stack %s: %w", rec.Id, stackID, err)
				}
			}
		}

		if len(stackEvents) > 0 {
			log.Printf("[scheduler] found %d pending reconciles for worker %s", len(stackEvents), workerID)
		}

		queueTotal := len(stackEvents)
		for stackID, event := range stackEvents {
			log.Printf("[scheduler] triggering pending %s reconcile for stack %s upon worker %s reconnect (queue total: %d)", event.Trigger, stackID, workerID, queueTotal)
			if err := s.app.Delete(event.Record); err != nil {
				return fmt.Errorf("failed to delete pending reconcile %s for stack %s before dispatch: %w", event.Record.Id, stackID, err)
			}
			s.TriggerSync(stackID, event.Trigger, queueTotal, "system")
		}
		return nil
	})
}

func (s *Scheduler) startJob(stack *core.Record) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.startJobLocked(stack)
}

// maxSyncIntervalSeconds bounds any sync_interval_seconds value before it is
// converted to a time.Duration. Without it, `time.Duration(seconds) *
// time.Second` overflows int64 for values above ~9.2e9 and wraps to a
// non-positive duration, which makes time.NewTicker panic. One year is far
// beyond any useful polling cadence, and it is the same ceiling the
// repositories collection enforces at write time (migration 68).
const maxSyncIntervalSeconds = 365 * 24 * 60 * 60

// intervalFromSeconds converts a stored sync_interval_seconds value into a
// ticker interval, clamping out-of-range values instead of overflowing.
// Non-positive (unset) values fall back to the global SCAN_PERIOD.
func intervalFromSeconds(seconds int, label string) time.Duration {
	if seconds <= 0 {
		return config.GetScanPeriod()
	}
	if seconds > maxSyncIntervalSeconds {
		log.Printf("[scheduler] %s: sync_interval_seconds=%d exceeds the maximum of %d, clamping", label, seconds, maxSyncIntervalSeconds)
		seconds = maxSyncIntervalSeconds
	}
	return time.Duration(seconds) * time.Second
}

// resolveSyncInterval returns the sync interval for a stack: wireops.yaml's
// sync.interval (stored as sync_interval_seconds) overrides the global
// SCAN_PERIOD; 0 (unset, or manually-configured stacks) falls back to it.
func resolveSyncInterval(stack *core.Record) time.Duration {
	return intervalFromSeconds(stack.GetInt("sync_interval_seconds"), "stack "+stack.Id)
}

// resolveRepoSyncInterval returns the git fetch interval for a repository:
// its own sync_interval_seconds overrides the global SCAN_PERIOD.
func resolveRepoSyncInterval(repo *core.Record) time.Duration {
	return intervalFromSeconds(repo.GetInt("sync_interval_seconds"), "repo "+repo.Id)
}

func (s *Scheduler) startJobLocked(stack *core.Record) {
	stackID := stack.Id
	interval := resolveSyncInterval(stack)

	// jobCtx is cancelled either when this specific job is unregistered
	// OR when the root context (app shutdown) is cancelled.
	jobCtx, jobCancel := context.WithCancel(s.rootCtx)
	s.jobs[stackID] = jobCancel

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-jobCtx.Done():
				return
			case <-ticker.C:
				if jobCtx.Err() != nil {
					return
				}
				// Use a detached context with timeout so re-registering the stack
				// does not cancel in-flight reconciles. jobCtx is only for
				// stopping the ticker loop.
				reconcileCtx, cancel := context.WithTimeout(s.rootCtx, 10*time.Minute)
				s.safeRun(reconcileCtx, fmt.Sprintf("cron[%s]", stackID), func() error {
					defer cancel()
					select {
					case s.syncSemaphore <- struct{}{}:
						defer func() { <-s.syncSemaphore }()
					case <-reconcileCtx.Done():
						return reconcileCtx.Err()
					}
					return s.reconciler.ReconcileStack(reconcileCtx, stackID, "cron", 0)
				})
			}
		}
	}()
}

func (s *Scheduler) startRepoJob(repo *core.Record) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.startRepoJobLocked(repo)
}

// startRepoJobLocked runs the repo-level fetch ticker: periodically pulls the
// repository's git state so stacks sharing it never need to fetch themselves.
func (s *Scheduler) startRepoJobLocked(repo *core.Record) {
	repoID := repo.Id
	interval := resolveRepoSyncInterval(repo)

	jobCtx, jobCancel := context.WithCancel(s.rootCtx)
	s.repoJobs[repoID] = jobCancel

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-jobCtx.Done():
				return
			case <-ticker.C:
				if jobCtx.Err() != nil {
					return
				}
				reconcileCtx, cancel := context.WithTimeout(s.rootCtx, 10*time.Minute)
				s.safeRun(reconcileCtx, fmt.Sprintf("repo-cron[%s]", repoID), func() error {
					defer cancel()
					select {
					case s.syncSemaphore <- struct{}{}:
						defer func() { <-s.syncSemaphore }()
					case <-reconcileCtx.Done():
						return reconcileCtx.Err()
					}
					_, _, err := s.reconciler.FetchRepo(reconcileCtx, repoID, "cron")
					return err
				})
			}
		}
	}()
}
