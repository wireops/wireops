package sync

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"github.com/wireops/wireops/internal/protocol"
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
// inspectStack: if *every* container is on commitSHA then the host really has
// this change and the record should say so, even when only some services came
// up — "degraded" already means exactly that.
//
// Four things make it refuse, because the label is a weaker signal than it
// looks:
//
//   - the containers straddle two revisions, which is precisely what a kill
//     mid-sequence produces — some services recreated at the new commit, the
//     rest never reached. Settling that by whichever container Docker listed
//     first would record a revision the stack is only half running.
//   - the worker is too old to report the set, so agreement cannot be checked.
//   - commitSHA is what was already deployed (previousSHA). The containers
//     carry that label whether or not this deploy ever recreated them, so it
//     cannot show that this render reached the host. deployed_commit is
//     already correct in that case, so nothing contradicts the host; only
//     version and checksum are unverifiable, and those live in compose
//     annotations rather than labels, so there is no finer signal to consult.
//   - the post-check verified nothing.
//
// Leaving the record behind is the safe direction: a stale deployed_checksum
// makes the next reconcile redeploy and converge properly, while a falsely
// advanced one makes it *skip* and strands the stack claiming a revision it
// may not be running.
//
// This only reads container state, which is why it is safe to run on a path
// that has already failed.
func (r *Reconciler) convergeAfterTimeout(ctx context.Context, workerID, stackID, workDir string, composeContent []byte, commitSHA, previousSHA string) timeoutConvergence {
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

	if commitSHA == previousSHA {
		// Same commit as the last deploy, so every container already carried
		// this label before the attempt. Nothing it reports can show whether
		// this render was applied.
		log.Printf("[reconciler] stack %s: deploy timed out on the already-deployed commit %s, so the container labels cannot confirm this revision; leaving the record alone",
			stackID, commitSHA)
		return timeoutConvergence{
			Status: check.Status,
			Detail: check.Detail + "; commit unchanged from the last deploy, so the container labels cannot confirm this revision, record left unchanged",
		}
	}

	revision, disagreement := agreedRevision(r.inspectStack(checkCtx, workerID, stackID))
	if revision != commitSHA {
		if disagreement == "" {
			disagreement = "containers still on " + describeCommit(revision)
		}
		log.Printf("[reconciler] stack %s: deploy timed out and the record was left alone (wanted %s): %s",
			stackID, commitSHA, disagreement)
		return timeoutConvergence{
			Status: check.Status,
			Detail: fmt.Sprintf("%s; %s, recorded revision left unchanged", check.Detail, disagreement),
			// Not converged: the host is not provably on this revision.
		}
	}

	log.Printf("[reconciler] stack %s: deploy timed out but every container runs %s, converging the record (status=%s)",
		stackID, commitSHA, check.Status)
	return timeoutConvergence{
		Status:    check.Status,
		Detail:    check.Detail + "; every container runs the attempted revision, record converged",
		Converged: true,
	}
}

// agreedRevision reports the single commit every one of the stack's containers
// is running. When there is no such commit it returns "" plus an
// operator-facing phrase naming why, so the sync log distinguishes "straddling
// two revisions" and "this worker cannot say" from the plain "on a different
// revision" the caller phrases itself.
func agreedRevision(inspected protocol.InspectResult) (sha, disagreement string) {
	switch {
	case len(inspected.CommitSHAs) == 1:
		return inspected.CommitSHAs[0], ""
	case len(inspected.CommitSHAs) > 1:
		return "", "containers straddle revisions " + strings.Join(inspected.CommitSHAs, " and ")
	case inspected.CommitSHA != "":
		// Labels came back but not the set: a worker predating
		// protocol.InspectResult.CommitSHAs. It reported one container's label
		// and cannot say whether the others match it.
		return "", "worker reports only one container's revision, so agreement could not be checked"
	default:
		// No containers, no labels, or the inspect failed. The caller's own
		// phrasing covers this via describeCommit.
		return "", ""
	}
}

// describeCommit renders a commit SHA for an operator-facing message,
// distinguishing "the worker reported a different revision" from "the worker
// could not tell us" — agreedRevision returns "" for no containers, no labels
// and a failed inspect alike.
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
