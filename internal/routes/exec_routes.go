package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pocketbase/pocketbase/core"

	"github.com/wireops/wireops/internal/audit"
	"github.com/wireops/wireops/internal/protocol"
	"github.com/wireops/wireops/internal/rbac"
)

const (
	// execRouteDefaultTimeout and execRouteMaxTimeout bound the run. The
	// worker applies the same deadline to the exec itself, so it stops
	// reading a command that outlives it rather than holding the stream open
	// after the server stopped listening.
	execRouteDefaultTimeout = 30 * time.Second
	execRouteMaxTimeout     = 300 * time.Second
	// execRouteResultGrace is how much longer than the worker's own deadline
	// the server waits for an answer. Both clocks measure the same timeout,
	// but the server's starts first and the worker spends time on its
	// ownership and host-escape checks before its own starts — so without
	// this the dispatch context would always expire first and a timed-out run
	// would surface as a 503 instead of its partial output and timed_out.
	execRouteResultGrace = 15 * time.Second
)

// execRequest is the body of POST /api/custom/stacks/{id}/exec.
type execRequest struct {
	ContainerID    string   `json:"container_id"`
	Cmd            []string `json:"cmd"`
	WorkingDir     string   `json:"working_dir"`
	User           string   `json:"user"`
	TimeoutSeconds int      `json:"timeout_seconds"`
}

// registerExecRoutes mounts the one-shot exec route.
//
// It is the non-interactive counterpart to the terminal session routes and
// carries the same capability (CapUseTerminal): it runs arbitrary commands in
// a container, which is the terminal's privilege by another door. What it adds
// over driving a pty is an exact answer — separated stdout/stderr and the real
// exit code — instead of a transcript a caller has to parse and a completion
// heuristic.
func (rr routeRegistrar) registerExecRoutes() {
	rr.r.POST("/api/custom/stacks/{id}/exec", func(e *core.RequestEvent) error {
		stackID := e.Request.PathValue("id")

		var body execRequest
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		}
		if body.ContainerID == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "container_id is required"})
		}
		if len(body.Cmd) == 0 {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "cmd is required and must be a non-empty argv array"})
		}

		_, projectName, workerID, ok := rr.resolveStackAndWorker(e, stackID)
		if !ok {
			return nil
		}

		// A worker that predates the exec command would never answer, so the
		// dispatch would sit until it timed out. Refuse up front and name the
		// capability, so a caller can fall back to the terminal path.
		worker, err := rr.app.FindRecordById("workers", workerID)
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "worker not found"})
		}
		if !recordHasCapability(worker, protocol.CapabilityExecOneShotV1) {
			return e.JSON(http.StatusConflict, map[string]string{
				"error":               "the assigned worker does not support one-shot exec",
				"required_capability": protocol.CapabilityExecOneShotV1,
				"upgrade_hint":        "upgrade the worker, or use the terminal session routes, which work on any worker but interleave stdout and stderr",
			})
		}

		timeout := execRouteDefaultTimeout
		if body.TimeoutSeconds > 0 {
			timeout = time.Duration(body.TimeoutSeconds) * time.Second
			if timeout > execRouteMaxTimeout {
				timeout = execRouteMaxTimeout
			}
		}
		// Dispatch's own fallback deadline is 5 minutes and only applies when
		// the context carries none, so the caller's timeout has to be put on
		// the context here — plus the grace period, so the worker's own
		// timed-out result wins the race against this deadline.
		ctx, cancel := context.WithTimeout(e.Request.Context(), timeout+execRouteResultGrace)
		defer cancel()

		// Audited with the argv: the pty path records everything in a session
		// transcript, and a one-shot exec must not be the quieter way to run
		// the same command.
		audit.RecordRequest(rr.app, e, audit.Event{
			Action:       "stack.exec",
			ResourceType: "stack",
			ResourceID:   stackID,
			Metadata: map[string]any{
				"container_id": body.ContainerID,
				"cmd":          body.Cmd,
				"working_dir":  body.WorkingDir,
				"user":         body.User,
			},
		})

		res, dispatchErr := rr.workerSvc.Dispatch(ctx, workerID, protocol.ExecCommand{
			CommandID:      fmt.Sprintf("exec-%s", uuid.NewString()),
			StackID:        stackID,
			ProjectName:    projectName,
			ContainerID:    body.ContainerID,
			Cmd:            body.Cmd,
			WorkingDir:     body.WorkingDir,
			User:           body.User,
			TimeoutSeconds: int(timeout.Seconds()),
		})
		if dispatchErr != nil {
			return e.JSON(http.StatusServiceUnavailable, map[string]string{"error": dispatchErr.Error()})
		}
		if res.Error != "" {
			status := http.StatusInternalServerError
			lowered := strings.ToLower(res.Error)
			switch {
			case strings.Contains(lowered, "does not belong to stack"):
				status = http.StatusForbidden
			case strings.Contains(lowered, "exec is blocked"):
				// The worker's own refusal for privileged/host-network/
				// docker-socket containers, which no policy can opt out of.
				status = http.StatusForbidden
			}
			return e.JSON(status, map[string]string{"error": res.Error})
		}

		var result protocol.ExecResult
		if err := json.Unmarshal([]byte(res.Output), &result); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to decode exec result: " + err.Error()})
		}
		return e.JSON(http.StatusOK, result)
	}).BindFunc(rbac.Require(rbac.CapUseTerminal))
}

// recordHasCapability reports whether a worker record advertises capability.
// The worker sends its capability list on connect (see
// internal/worker/service.go), and an absent or unreadable field means "not
// supported" rather than an error — an old worker simply does not send one.
func recordHasCapability(worker *core.Record, capability string) bool {
	var capabilities []string
	if err := worker.UnmarshalJSONField("capabilities", &capabilities); err != nil {
		return false
	}
	for _, candidate := range capabilities {
		if candidate == capability {
			return true
		}
	}
	return false
}
