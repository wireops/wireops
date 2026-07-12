package sync

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"github.com/wireops/wireops/internal/constants"
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
//
// When it actually has to wait (active jobs found), it eagerly creates the
// deploy's sync_logs row (so the operator can see "waiting" in real time
// instead of only after the fact) and records a policy_check phase on it.
// The caller reuses the returned record as the deploy's sync log instead of
// creating a second one, avoiding the old two-rows-per-deploy artifact. When
// no wait is needed (the common case), it returns (nil, nil) and the caller
// records policy_check itself once its own sync log exists.
func (r *Reconciler) waitForRunningJobs(ctx context.Context, stack *core.Record, repoID, stackID, trigger, commitSHA string) (*core.Record, error) {
	policy := stack.GetString("wait_running_jobs")
	if policy == "" || policy == "never" {
		return nil, nil
	}

	active, err := jobscheduler.ActiveJobRunsForRepository(r.app, repoID)
	if err != nil {
		log.Printf("[reconciler] wait_running_jobs: failed to query active jobs for repo %s: %v", repoID, err)
		return nil, nil
	}
	if len(active) == 0 {
		return nil, nil
	}

	log.Printf("[reconciler] wait_running_jobs: stack %s deploy waiting on %d active job(s) for repo %s (policy=%s)", stackID, len(active), repoID, policy)

	var waitLog *core.Record
	var pt *phaseTracker
	if rec, logErr := r.createSyncLog(stackID, trigger, commitSHA, fmt.Sprintf("waiting for %d running job(s) on repository", len(active))); logErr == nil {
		waitLog = rec
		pt = newPhaseTracker(r.app, rec.Id)
		_ = pt.start(constants.PhasePolicyCheck)
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
	queryFailed := false
	for {
		select {
		case <-ctx.Done():
			return waitLog, ctx.Err()
		case <-time.After(waitJobsPollInterval):
		}

		active, err := jobscheduler.ActiveJobRunsForRepository(r.app, repoID)
		if err != nil {
			log.Printf("[reconciler] wait_running_jobs: failed to query active jobs for repo %s: %v", repoID, err)
			queryFailed = true
			break
		}
		if len(active) == 0 {
			break
		}
		if hasDeadline && time.Now().After(deadline) {
			errMsg := fmt.Sprintf("timeout waiting %s for %d running job(s) on repository to finish before deploy", time.Since(start).Round(time.Second), len(active))
			if waitLog != nil {
				_ = pt.finish(constants.PhasePolicyCheck, constants.PhaseStatusError, errMsg)
				_ = r.updateSyncLog(waitLog.Id, "error", errMsg, time.Since(start).Milliseconds())
			}
			_ = r.markError(stack, "stacks")
			return waitLog, fmt.Errorf("%s", errMsg)
		}
	}

	if waitLog != nil {
		// Status goes back to "running" (not a terminal "success"): the
		// deploy itself continues after this — this row is reused as the
		// deploy's own sync log, not a standalone "wait" record anymore.
		if queryFailed {
			detail := fmt.Sprintf("proceeded after %s: failed to query active jobs, continuing best-effort", time.Since(start).Round(time.Second))
			_ = pt.finish(constants.PhasePolicyCheck, constants.PhaseStatusSuccess, detail)
			_ = r.updateSyncLog(waitLog.Id, "running", detail, time.Since(start).Milliseconds())
		} else {
			detail := fmt.Sprintf("proceeded after waiting %s for running jobs to finish", time.Since(start).Round(time.Second))
			_ = pt.finish(constants.PhasePolicyCheck, constants.PhaseStatusSuccess, detail)
			_ = r.updateSyncLog(waitLog.Id, "running", detail, time.Since(start).Milliseconds())
		}
	}
	return waitLog, nil
}
