package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/wireops/wireops/internal/compose"
	"github.com/wireops/wireops/internal/protocol"
)

const (
	postCheckAttempts    = 3
	restartLoopThreshold = 3
	restartLoopWindow    = 2 * time.Minute
)

// postCheckInterval is a var (not const) so tests can shrink it to keep the
// retry loop in postDeployCheck fast without changing the retry count.
var postCheckInterval = 3 * time.Second

// postCheckResult is the outcome of verifying container state after a
// `docker compose up` reported success. Status is one of "active" (all
// expected services running/healthy), "degraded" (some but not all), or
// "error" (none of the expected services came up).
type postCheckResult struct {
	Status string
	Detail string
}

// postDeployCheck queries the worker for live container status after a
// deploy and compares it against the services declared in the rendered
// compose file. It retries a few times with a short delay since containers
// (and their healthchecks) may still be starting right after `up -d`
// returns. A failure to query status at all does not fail the deploy —
// the compose command itself already succeeded — it only skips the check.
func (r *Reconciler) postDeployCheck(ctx context.Context, workerID, stackID, workDir string, composeContent []byte) postCheckResult {
	expected, err := compose.ExpectedServiceNames(composeContent)
	if err != nil || len(expected) == 0 {
		log.Printf("[reconciler] post-check: could not determine expected services for stack %s, skipping: %v", stackID, err)
		return postCheckResult{Status: "active", Detail: "post-check skipped: could not parse expected services from compose"}
	}

	projectName := compose.ProjectName(workDir)

	var statuses []compose.ServiceStatus
	var queryErr error
	gotStatus := false

	for attempt := 1; attempt <= postCheckAttempts; attempt++ {
		result, dispatchErr := r.dispatcher.Dispatch(ctx, workerID, protocol.GetStatusCommand{
			CommandID:   fmt.Sprintf("post-check-%s-%d", stackID, attempt),
			ProjectName: projectName,
		})
		switch {
		case dispatchErr != nil:
			queryErr = dispatchErr
		case result.Error != "":
			queryErr = fmt.Errorf("%s", result.Error)
		default:
			if err := json.Unmarshal([]byte(result.Output), &statuses); err != nil {
				queryErr = fmt.Errorf("failed to parse status response: %w", err)
			} else {
				queryErr = nil
				gotStatus = true
				if evaluatePostCheck(expected, statuses).Status == "active" {
					return postCheckResult{Status: "active", Detail: fmt.Sprintf("post-check passed: %d/%d services running", len(expected), len(expected))}
				}
			}
		}

		if attempt < postCheckAttempts {
			select {
			case <-ctx.Done():
				return postCheckResult{Status: "error", Detail: "post-check interrupted: " + ctx.Err().Error()}
			case <-time.After(postCheckInterval):
			}
		}
	}

	if !gotStatus {
		log.Printf("[reconciler] post-check: failed to query status for stack %s after %d attempts: %v", stackID, postCheckAttempts, queryErr)
		return postCheckResult{Status: "active", Detail: "post-check skipped: could not query worker status: " + queryErr.Error()}
	}

	return evaluatePostCheck(expected, statuses)
}

// evaluatePostCheck classifies each expected service as healthy, missing
// (no container, or present but not running), unhealthy (healthcheck
// failing), or restart-looping, and derives the overall stack status.
func evaluatePostCheck(expected []string, statuses []compose.ServiceStatus) postCheckResult {
	byService := make(map[string][]compose.ServiceStatus, len(statuses))
	for _, s := range statuses {
		byService[s.ServiceName] = append(byService[s.ServiceName], s)
	}

	var ok, missing, unhealthy, restartLooping []string
	for _, name := range expected {
		instances := byService[name]
		if len(instances) == 0 {
			missing = append(missing, name)
			continue
		}

		healthy := false
		sawLooping := false
		sawUnhealthy := false
		for _, inst := range instances {
			if isRestartLooping(inst) {
				sawLooping = true
				continue
			}
			if inst.Status != "running" {
				continue
			}
			if inst.Health == "unhealthy" {
				sawUnhealthy = true
				continue
			}
			healthy = true
			break
		}

		switch {
		case healthy:
			ok = append(ok, name)
		case sawLooping:
			restartLooping = append(restartLooping, name)
		case sawUnhealthy:
			unhealthy = append(unhealthy, name)
		default:
			missing = append(missing, name)
		}
	}

	detail := buildPostCheckDetail(len(expected), ok, missing, unhealthy, restartLooping)

	switch {
	case len(ok) == len(expected):
		return postCheckResult{Status: "active", Detail: detail}
	case len(ok) == 0:
		return postCheckResult{Status: "error", Detail: detail}
	default:
		return postCheckResult{Status: "degraded", Detail: detail}
	}
}

// isRestartLooping reports whether a container has restarted suspiciously
// often within a short window, which `docker compose up` alone cannot detect
// since it only checks the exit code of the `up` command itself.
func isRestartLooping(s compose.ServiceStatus) bool {
	if s.RestartCount < restartLoopThreshold {
		return false
	}
	if s.StartedAt == "" {
		return true
	}
	startedAt, err := time.Parse(time.RFC3339Nano, s.StartedAt)
	if err != nil {
		return true
	}
	return time.Since(startedAt) < restartLoopWindow
}

func buildPostCheckDetail(total int, ok, missing, unhealthy, restartLooping []string) string {
	if len(ok) == total {
		return fmt.Sprintf("post-check passed: %d/%d services running", total, total)
	}
	parts := []string{fmt.Sprintf("post-check: %d/%d services healthy", len(ok), total)}
	if len(missing) > 0 {
		parts = append(parts, "missing/not running: "+strings.Join(missing, ", "))
	}
	if len(unhealthy) > 0 {
		parts = append(parts, "unhealthy: "+strings.Join(unhealthy, ", "))
	}
	if len(restartLooping) > 0 {
		parts = append(parts, "restart looping: "+strings.Join(restartLooping, ", "))
	}
	return strings.Join(parts, "; ")
}
