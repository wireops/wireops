package sync

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"github.com/wireops/wireops/internal/docker"
	"github.com/wireops/wireops/internal/notify"
)

type Scheduler struct {
	mu         sync.Mutex
	jobs       map[string]context.CancelFunc // keyed by stack ID
	reconciler *Reconciler
	app        core.App

	// rootCtx / rootCancel are used for a global graceful shutdown.
	// Shutdown() cancels rootCtx, causing all goroutines to stop.
	rootCtx    context.Context
	rootCancel context.CancelFunc
}

func NewScheduler(app core.App, dockerClient *docker.Client, dispatcher WorkerDispatcher) *Scheduler {
	notifier := notify.New(app)
	rootCtx, rootCancel := context.WithCancel(context.Background())
	return &Scheduler{
		jobs:       make(map[string]context.CancelFunc),
		reconciler: NewReconciler(app, dockerClient, notifier, dispatcher),
		app:        app,
		rootCtx:    rootCtx,
		rootCancel: rootCancel,
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

func (s *Scheduler) Start() error {
	stacks, err := s.app.FindAllRecords("stacks")
	if err != nil {
		return err
	}
	for _, stack := range stacks {
		if stack.GetString("status") != "paused" {
			s.startJob(stack)
		}
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

func (s *Scheduler) TriggerSync(stackID, trigger string, queueTotal int) {
	ctx := s.rootCtx
	go s.safeRun(ctx, fmt.Sprintf("sync[%s] trigger=%s", stackID, trigger), func() error {
		return s.reconciler.ReconcileStack(ctx, stackID, trigger, queueTotal)
	})
}

func (s *Scheduler) TriggerRollback(stackID, commitSHA string) {
	ctx := s.rootCtx
	go s.safeRun(ctx, fmt.Sprintf("rollback[%s]", stackID), func() error {
		return s.reconciler.RollbackStack(ctx, stackID, commitSHA)
	})
}

func (s *Scheduler) TriggerForceRedeploy(stackID string, recreateContainers, recreateVolumes, recreateNetworks bool) {
	ctx := s.rootCtx
	go s.safeRun(ctx, fmt.Sprintf("force-redeploy[%s]", stackID), func() error {
		return s.reconciler.ForceRedeployStack(ctx, stackID, recreateContainers, recreateVolumes, recreateNetworks)
	})
}

func (s *Scheduler) TriggerTransfer(stackID, targetWorkerID string) {
	ctx := s.rootCtx
	go s.safeRun(ctx, fmt.Sprintf("transfer[%s]", stackID), func() error {
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
					_ = s.app.Delete(existing.Record)
				}
				stackEvents[stackID] = pendingEvent{
					Record:  rec,
					Trigger: rec.GetString("trigger"),
					Created: created,
				}
			} else {
				_ = s.app.Delete(rec)
			}
		}

		if len(stackEvents) > 0 {
			log.Printf("[scheduler] found %d pending reconciles for worker %s", len(stackEvents), workerID)
		}

		queueTotal := len(stackEvents)
		for stackID, event := range stackEvents {
			log.Printf("[scheduler] triggering pending %s reconcile for stack %s upon worker %s reconnect (queue total: %d)", event.Trigger, stackID, workerID, queueTotal)
			_ = s.app.Delete(event.Record)
			s.TriggerSync(stackID, event.Trigger, queueTotal)
		}
		return nil
	})
}

func (s *Scheduler) startJob(stack *core.Record) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.startJobLocked(stack)
}

func (s *Scheduler) startJobLocked(stack *core.Record) {
	stackID := stack.Id
	interval := stack.GetInt("poll_interval")
	if interval <= 0 {
		interval = 60
	}

	// jobCtx is cancelled either when this specific job is unregistered
	// OR when the root context (app shutdown) is cancelled.
	jobCtx, jobCancel := context.WithCancel(s.rootCtx)
	s.jobs[stackID] = jobCancel

	go func() {
		ticker := time.NewTicker(time.Duration(interval) * time.Second)
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
				// (e.g. when user changes poll_interval) does not cancel in-flight
				// reconciles. jobCtx is only for stopping the ticker loop.
				reconcileCtx, cancel := context.WithTimeout(s.rootCtx, 10*time.Minute)
				s.safeRun(reconcileCtx, fmt.Sprintf("cron[%s]", stackID), func() error {
					defer cancel()
					return s.reconciler.ReconcileStack(reconcileCtx, stackID, "cron", 0)
				})
			}
		}
	}()
}
