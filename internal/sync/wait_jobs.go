package sync

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"github.com/wireops/wireops/internal/jobscheduler"
)

// waitJobsPollInterval is a var (not const) so tests can shrink it.
var waitJobsPollInterval = 5 * time.Second

const defaultWaitRunningJobsTimeoutSeconds = 300

// waitForRunningJobs blocks the current deploy while job_runs are active for
// the stack's repository, according to the stack's wait_running_jobs policy:
//   - "" / "never": don't wait at all (default, preserves old behavior).
//   - "always": wait indefinitely until no jobs are running.
//   - "timeout": wait up to wait_running_jobs_timeout_seconds, then block the
//     deploy with a clear error (per product decision: timeout always blocks
//     the automatic path — operators use ForceRedeployStack to override).
//
// A failure to query active jobs does not block the deploy — it's treated as
// best-effort the same way the rest of the reconciler treats status lookups.
func (r *Reconciler) waitForRunningJobs(ctx context.Context, stack *core.Record, repoID, stackID, trigger, commitSHA string) error {
	policy := stack.GetString("wait_running_jobs")
	if policy == "" || policy == "never" {
		return nil
	}

	active, err := jobscheduler.ActiveJobRunsForRepository(r.app, repoID)
	if err != nil {
		log.Printf("[reconciler] wait_running_jobs: failed to query active jobs for repo %s: %v", repoID, err)
		return nil
	}
	if len(active) == 0 {
		return nil
	}

	log.Printf("[reconciler] wait_running_jobs: stack %s deploy waiting on %d active job(s) for repo %s (policy=%s)", stackID, len(active), repoID, policy)

	var waitLog *core.Record
	if rec, logErr := r.createSyncLog(stackID, trigger, commitSHA, fmt.Sprintf("waiting for %d running job(s) on repository", len(active))); logErr == nil {
		waitLog = rec
		_ = r.updateSyncLog(rec.Id, "waiting_jobs", fmt.Sprintf("deploy paused: %d job(s) currently running on this repository", len(active)), 0)
	} else {
		log.Printf("[reconciler] wait_running_jobs: failed to create waiting sync log for stack %s: %v", stackID, logErr)
	}

	hasDeadline := policy == "timeout"
	var deadline time.Time
	if hasDeadline {
		timeoutSecs := stack.GetInt("wait_running_jobs_timeout_seconds")
		if timeoutSecs <= 0 {
			timeoutSecs = defaultWaitRunningJobsTimeoutSeconds
		}
		deadline = time.Now().Add(time.Duration(timeoutSecs) * time.Second)
	}

	start := time.Now()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitJobsPollInterval):
		}

		active, err := jobscheduler.ActiveJobRunsForRepository(r.app, repoID)
		if err != nil {
			log.Printf("[reconciler] wait_running_jobs: failed to query active jobs for repo %s: %v", repoID, err)
			break
		}
		if len(active) == 0 {
			break
		}
		if hasDeadline && time.Now().After(deadline) {
			errMsg := fmt.Sprintf("timeout waiting %s for %d running job(s) on repository to finish before deploy", time.Since(start).Round(time.Second), len(active))
			if waitLog != nil {
				_ = r.updateSyncLog(waitLog.Id, "error", errMsg, time.Since(start).Milliseconds())
			}
			_ = r.markError(stack, "stacks")
			return fmt.Errorf("%s", errMsg)
		}
	}

	if waitLog != nil {
		_ = r.updateSyncLog(waitLog.Id, "success", fmt.Sprintf("proceeded after waiting %s for running jobs to finish", time.Since(start).Round(time.Second)), time.Since(start).Milliseconds())
	}
	return nil
}
