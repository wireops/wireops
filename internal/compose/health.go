package compose

import "time"

// ServiceHealthState is the observation made for one Compose service.
// RestartLooping remains distinct so deploy post-checks can preserve their
// existing diagnostic detail while health endpoints can present it as
// unhealthy.
type ServiceHealthState string

const (
	ServiceHealthy        ServiceHealthState = "healthy"
	ServiceStarting       ServiceHealthState = "starting"
	ServiceMissing        ServiceHealthState = "missing"
	ServiceUnhealthy      ServiceHealthState = "unhealthy"
	ServiceRestartLooping ServiceHealthState = "restart_looping"
)

const (
	restartLoopThreshold = 3
	restartLoopWindow    = 2 * time.Minute
)

// ClassifyServiceInstances is the shared container-state classifier for the
// deploy post-check and on-demand health observations. requireAllReplicas is
// false for the historical post-check semantics (one ready replica allows the
// deploy to proceed) and true for live health, where every observed replica
// must be sound before the stack is declared healthy.
func ClassifyServiceInstances(instances []ServiceStatus, isInit, requireAllReplicas bool) ServiceHealthState {
	if len(instances) == 0 {
		if isInit {
			return ServiceHealthy
		}
		return ServiceMissing
	}

	var healthy, starting, unhealthy, looping bool
	for _, instance := range instances {
		if IsRestartLooping(instance) {
			looping = true
			continue
		}
		if isInit {
			switch instance.Status {
			case "running":
				healthy = true
			case "exited":
				if instance.ExitCode == 0 {
					healthy = true
				} else {
					unhealthy = true
				}
			case "created", "restarting", "paused":
				starting = true
			default:
				unhealthy = true
			}
			continue
		}

		switch {
		case !requireAllReplicas && instance.Status == "running" && instance.Health != "unhealthy":
			// The deployment post-check historically accepted a running
			// healthcheck-starting container and retried only when no replica
			// was ready at all. Keep that compatibility for its non-strict
			// mode; on-demand health uses the strict mode below.
			healthy = true
		case instance.Status == "exited" || instance.Status == "dead" || instance.Health == "unhealthy":
			unhealthy = true
		case instance.Status != "running" || instance.Health == "starting":
			starting = true
		default:
			healthy = true
		}
	}

	if requireAllReplicas {
		switch {
		case looping:
			return ServiceRestartLooping
		case unhealthy:
			return ServiceUnhealthy
		case starting:
			return ServiceStarting
		case healthy:
			return ServiceHealthy
		default:
			return ServiceMissing
		}
	}

	// Preserve the post-check's established behavior: any ready replica is
	// sufficient during a deploy, while live health deliberately uses the
	// stricter branch above.
	switch {
	case healthy:
		return ServiceHealthy
	case looping:
		return ServiceRestartLooping
	case unhealthy:
		return ServiceUnhealthy
	case isInit:
		return ServiceStarting
	default:
		return ServiceMissing
	}
}

// IsRestartLooping reports a recent, repeated restart. A stale restart count
// does not make an otherwise stable container unhealthy.
func IsRestartLooping(status ServiceStatus) bool {
	if status.RestartCount < restartLoopThreshold {
		return false
	}
	if status.StartedAt == "" {
		return true
	}
	startedAt, err := time.Parse(time.RFC3339Nano, status.StartedAt)
	return err != nil || time.Since(startedAt) < restartLoopWindow
}
