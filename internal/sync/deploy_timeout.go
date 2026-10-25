package sync

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// timeoutConvergeTimeout bounds the two reads convergeAfterTimeout makes. The
// deploy has already failed at this point, so a worker that has gone quiet
// must not stretch the failure path further — the record simply stays as it
// was, which is the pre-#178 behaviour.
const timeoutConvergeTimeout = 45 * time.Second

// timeoutConvergence is what the host turned out to be running after a deploy
// its own budget killed.
type timeoutConvergence struct {
	// Status is the post-check's verdict: "active", "degraded" or "error".
	Status string
	// Detail is the post-check's human-readable summary, folded into the
	// sync log so the timeline says what was found rather than only that the
	// deploy was killed.
	Detail string
	// Converged reports whether the containers carry the revision this
	// attempt was deploying, i.e. whether the deployed_* fields should be
	// advanced. False leaves the record exactly as a timeout left it before.
	Converged bool
}

// convergeAfterTimeout asks the worker what is actually on the host after a
// deploy was killed by deploy_timeout_seconds.
//
// `docker compose up` is not atomic. When the budget expires mid-sequence the
// containers it had already recreated keep running, at the new revision — so
// a record left naming the previous commit describes a host that no longer
// exists. That is issue #178 item 2: status "error" and deployed_commit
// pointing at a commit nothing is running, while the health rollup reports
// the services up, and only force_redeploy_stack converging it because
// deploy_stack correctly reports noop against the already-matching render.
//
// The gate is the commit label the renderer injects, read back through
// inspectStackCommit: if the containers are on commitSHA then the host really
// has this change and the record should say so, even when only some services
// came up — "degraded" already means exactly that. If they are still on the
// old revision, nothing is converged.
//
// This only reads container state, which is why it is safe to run on a path
// that has already failed. A post-check that could not verify anything
// converges nothing, whatever status it nominally reports.
func (r *Reconciler) convergeAfterTimeout(ctx context.Context, workerID, stackID, workDir string, composeContent []byte, commitSHA string) timeoutConvergence {
	if r.dispatcher == nil || commitSHA == "" {
		return timeoutConvergence{}
	}

	checkCtx, cancel := freshTimeout(ctx, timeoutConvergeTimeout)
	defer cancel()

	check := r.postDeployCheck(checkCtx, workerID, stackID, workDir, composeContent)
	if check.Skipped {
		// Nothing was actually verified. The post-check reports "active" for
		// its skip paths because the success path can fall back on compose
		// having succeeded — here it did not, so there is no evidence to
		// converge on.
		log.Printf("[reconciler] stack %s: deploy timed out and the post-check could not verify container state (%s), leaving the record alone",
			stackID, check.Detail)
		return timeoutConvergence{Detail: check.Detail}
	}

	containerSHA := r.inspectStackCommit(checkCtx, workerID, stackID)
	if containerSHA != commitSHA {
		log.Printf("[reconciler] stack %s: deploy timed out and the containers are still on %q (wanted %q), leaving the recorded revision alone",
			stackID, containerSHA, commitSHA)
		return timeoutConvergence{
			Status: check.Status,
			Detail: fmt.Sprintf("%s; containers still on %s, recorded revision left unchanged", check.Detail, describeCommit(containerSHA)),
			// Not converged: whatever is running is not this revision.
		}
	}

	log.Printf("[reconciler] stack %s: deploy timed out but the containers run %s, converging the record (status=%s)",
		stackID, commitSHA, check.Status)
	return timeoutConvergence{
		Status:    check.Status,
		Detail:    check.Detail + "; containers run the attempted revision, record converged",
		Converged: true,
	}
}

// describeCommit renders a commit SHA for an operator-facing message,
// distinguishing "the worker reported a different revision" from "the worker
// could not tell us" — inspectStackCommit returns "" for both a missing label
// and a failed inspect.
func describeCommit(sha string) string {
	if sha == "" {
		return "an unknown revision"
	}
	return sha
}

// applyTimeoutConvergence advances the deployed_* bookkeeping to the revision
// the containers were found running. It is the same set of fields a
// successful deploy writes, minus the error clearing: the deploy did fail, and
// last_error_* still describes how.
func applyTimeoutConvergence(stack *core.Record, render *RenderResult, commitSHA string) {
	now := time.Now().UTC().Format(time.RFC3339)
	stack.Set("last_synced_at", now)
	stack.Set("deployed_version", render.Version)
	stack.Set("deployed_commit", commitSHA)
	stack.Set("deployed_checksum", render.Checksum)
	stack.Set("deployed_at", now)
	persistProjectIdentity(stack, render)
}
