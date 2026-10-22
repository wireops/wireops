package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/wireops/wireops/mcp/client"
)

const (
	// waitDefaultTimeout bounds a wait a caller did not bound itself, and
	// waitMaxTimeout bounds what it may ask for. A deploy that takes longer
	// than this is better followed with get_sync_logs than held open.
	waitDefaultTimeout = 120 * time.Second
	waitMaxTimeout     = 600 * time.Second
)

// terminalSyncStatuses are the sync_logs statuses that end a reconcile. The
// reconciler writes one row per reconcile, so the row reaching one of these is
// the signal that the deploy is over.
//
// These are exactly the values the collection allows (pb_migrations 01, 40 and
// 43) minus the ones that mean "still going": "running", "queued" (the worker
// was offline and the reconcile will resume on reconnect) and "waiting_jobs"
// (paused until in-flight job runs on the repository finish). "noop" matters
// most in practice — it is what a sync whose rendered output did not change
// ends as, i.e. the normal outcome of deploy_stack when git has not moved.
var terminalSyncStatuses = map[string]bool{
	"success":  true,
	"error":    true,
	"noop":     true,
	"degraded": true,
}

// statusFrame is the payload of the stream's named "status" event.
type statusFrame struct {
	RecordID string `json:"record_id"`
	Status   string `json:"status"`
}

// waitOutcome is what a waited write tool reports in place of
// {"status":"triggered"}.
type waitOutcome struct {
	Waited          bool   `json:"waited"`
	TimedOut        bool   `json:"timed_out"`
	SyncLogStatus   string `json:"sync_log_status,omitempty"`
	SyncLogID       string `json:"sync_log_id,omitempty"`
	StackStatus     string `json:"stack_status,omitempty"`
	DeployedVersion any    `json:"deployed_version,omitempty"`
	Output          string `json:"output,omitempty"`
	Notice          string `json:"_notice,omitempty"`
}

// clampWaitTimeout resolves a caller's timeout_seconds.
func clampWaitTimeout(seconds int) time.Duration {
	if seconds <= 0 {
		return waitDefaultTimeout
	}
	timeout := time.Duration(seconds) * time.Second
	if timeout > waitMaxTimeout {
		return waitMaxTimeout
	}
	return timeout
}

// awaitReconcile watches a stack's deploy stream until the reconcile trigger
// fired by trigger() reaches a terminal sync_logs status, then reports the
// outcome. The stream is opened *before* trigger runs, because the trigger
// routes are fire-and-forget (scheduler.TriggerSync) and a status transition
// published in between would otherwise be missed.
//
// history=false keeps a stack with a long deploy history from replaying all of
// it ahead of the part being waited for.
func awaitReconcile(ctx context.Context, c *client.Client, apiKey, stackID string, timeout time.Duration, trigger func(context.Context) error) (waitOutcome, error) {
	watchCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	type result struct {
		frame statusFrame
		err   error
	}
	done := make(chan result, 1)
	opened := make(chan struct{})

	go func() {
		var last statusFrame
		err := c.Stream(watchCtx, apiKey, "/api/custom/stacks/"+url.PathEscape(stackID)+"/stream",
			url.Values{"history": {"false"}},
			func() { close(opened) },
			func(event client.StreamEvent) bool {
				if event.Name != "status" {
					return false
				}
				var frame statusFrame
				if err := json.Unmarshal([]byte(event.Data), &frame); err != nil {
					return false
				}
				last = frame
				return terminalSyncStatuses[strings.ToLower(frame.Status)]
			})
		done <- result{frame: last, err: err}
	}()

	// The stream route commits its 200 before the first log line, so this
	// resolves as soon as the subscription is live. Waiting for the first
	// *event* instead would deadlock: the event being waited for is the one
	// the trigger below produces.
	select {
	case <-opened:
	case res := <-done:
		if res.err != nil {
			return waitOutcome{}, fmt.Errorf("watching the stack's deploy stream: %w", res.err)
		}
	case <-watchCtx.Done():
	}

	if err := trigger(ctx); err != nil {
		return waitOutcome{}, err
	}

	var frame statusFrame
	timedOut := false
	select {
	case res := <-done:
		if res.err != nil && watchCtx.Err() == nil {
			return waitOutcome{}, fmt.Errorf("watching the stack's deploy stream: %w", res.err)
		}
		frame = res.frame
		timedOut = watchCtx.Err() != nil && !terminalSyncStatuses[strings.ToLower(frame.Status)]
	case <-watchCtx.Done():
		timedOut = true
	}

	out := waitOutcome{
		Waited:        true,
		TimedOut:      timedOut,
		SyncLogStatus: frame.Status,
		SyncLogID:     frame.RecordID,
	}

	// The status frame says how the reconcile ended; the stack record and the
	// log row say what it ended as. Both are read with the caller's own key on
	// the parent context, so they still work after the watch deadline passed.
	var stack struct {
		Status          string `json:"status"`
		DeployedVersion any    `json:"deployed_version"`
	}
	if err := c.Get(ctx, apiKey, "/api/collections/stacks/records/"+url.PathEscape(stackID),
		url.Values{"fields": {"status,deployed_version"}}, &stack); err == nil {
		out.StackStatus = stack.Status
		out.DeployedVersion = stack.DeployedVersion
	}

	var logs struct {
		Items []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Output string `json:"output"`
		} `json:"items"`
	}
	if err := c.Get(ctx, apiKey, "/api/collections/sync_logs/records", url.Values{
		"filter":  {fmt.Sprintf("stack='%s'", client.EscapeFilterValue(stackID))},
		"sort":    {"-created"},
		"perPage": {"1"},
		"fields":  {"id,status,output"},
	}, &logs); err == nil && len(logs.Items) > 0 {
		out.Output = logs.Items[0].Output
		if out.SyncLogStatus == "" {
			out.SyncLogStatus = logs.Items[0].Status
			out.SyncLogID = logs.Items[0].ID
		}
	}

	// A timeout is reported ahead of any status, because on that path the
	// status was read from the newest sync_logs row rather than from a
	// transition this wait saw — it may well belong to an earlier reconcile.
	if timedOut {
		out.Notice = "The wait timed out; the reconcile may still be running. Poll get_stack_status, or read get_sync_logs for the rest of the output."
		return out, nil
	}
	switch strings.ToLower(out.SyncLogStatus) {
	case "success":
		// Nothing to add: the output is the evidence.
	case "noop":
		out.Notice = "The reconcile finished with nothing to do: the rendered compose output is identical to what is already deployed. That is the normal outcome when git has not changed — use force_redeploy_stack to redeploy the same revision anyway."
	case "degraded":
		out.Notice = "The deploy ran, but the stack came up degraded: at least one non-init service is unhealthy, or an init service exited non-zero. get_stack_services shows which, and get_container_logs why."
	case "":
		// Nothing was observed and nothing was stored; the caller has the
		// stack status and can poll.
	default:
		out.Notice = "The reconcile did not succeed. The output above is the deploy log; lint_compose (with this stack's id) catches compose and undefined-variable problems, and get_container_logs the ones that only show up once a container starts."
	}
	return out, nil
}
