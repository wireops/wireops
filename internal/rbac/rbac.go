package rbac

import (
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

const (
	RoleMonitoring = "monitoring"
	RoleViewer     = "viewer"
	RoleOperator   = "operator"
	RoleAdmin      = "admin"
)

const (
	ActorUser  = "user"
	ActorAgent = "agent"
)

type Capability string

const (
	CapViewMetrics    Capability = "view_metrics"
	CapViewStacks     Capability = "view_stacks"
	CapViewJobs       Capability = "view_jobs"
	CapViewLogs       Capability = "view_logs"
	CapOperateStacks  Capability = "operate_stacks"
	CapManageRepos    Capability = "manage_repositories"
	CapManageWorkers  Capability = "manage_workers"
	CapManageJobs     Capability = "manage_jobs"
	CapManageSettings Capability = "manage_settings"
	CapManageUsers    Capability = "manage_users"
	CapManageSecurity Capability = "manage_security"
	CapViewAuditLogs  Capability = "view_audit_logs"
)

var minimumRoleByCapability = map[Capability]string{
	CapViewMetrics:    RoleMonitoring,
	CapViewStacks:     RoleViewer,
	CapViewJobs:       RoleViewer,
	CapViewLogs:       RoleViewer,
	CapOperateStacks:  RoleOperator,
	CapManageRepos:    RoleOperator,
	CapManageWorkers:  RoleOperator,
	CapManageJobs:     RoleOperator,
	CapManageSettings: RoleAdmin,
	CapManageUsers:    RoleAdmin,
	CapManageSecurity: RoleAdmin,
	CapViewAuditLogs:  RoleAdmin,
}

func NormalizeRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case RoleMonitoring:
		return RoleMonitoring
	case RoleViewer:
		return RoleViewer
	case RoleOperator:
		return RoleOperator
	case RoleAdmin:
		return RoleAdmin
	default:
		return ""
	}
}

func MustNormalizeRole(role string) string {
	if normalized := NormalizeRole(role); normalized != "" {
		return normalized
	}
	return RoleViewer
}

func RoleRank(role string) int {
	switch NormalizeRole(role) {
	case RoleMonitoring:
		return 1
	case RoleViewer:
		return 2
	case RoleOperator:
		return 3
	case RoleAdmin:
		return 4
	default:
		return 0
	}
}

func AtLeast(role, minimum string) bool {
	return RoleRank(role) >= RoleRank(minimum)
}

func HighestRole(roles ...string) string {
	best := ""
	for _, role := range roles {
		normalized := NormalizeRole(role)
		if RoleRank(normalized) > RoleRank(best) {
			best = normalized
		}
	}
	return best
}

func MinimumRole(capability Capability) string {
	if role, ok := minimumRoleByCapability[capability]; ok {
		return role
	}
	return RoleAdmin
}

func ResolveActor(e *core.RequestEvent) (string, string, string) {
	if e == nil || e.Auth == nil {
		return "", "", ""
	}
	if e.Auth.IsSuperuser() {
		return RoleAdmin, ActorUser, e.Auth.Id
	}

	col := e.Auth.Collection()
	if col != nil && col.Name == "users" && e.Auth.GetBool("disabled") {
		return "", ActorUser, e.Auth.Id
	}

	role := NormalizeRole(e.Auth.GetString("role"))
	actorType := ActorUser
	if col != nil && col.Name == "service_accounts" {
		actorType = ActorAgent
	}
	return role, actorType, e.Auth.Id
}

func Can(e *core.RequestEvent, capability Capability) bool {
	role, _, _ := ResolveActor(e)
	return AtLeast(role, MinimumRole(capability))
}

func Require(capability Capability) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		if Can(e, capability) {
			return e.Next()
		}
		if e == nil {
			return nil
		}
		if e.Auth == nil {
			return e.JSON(http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		}
		return e.JSON(http.StatusForbidden, map[string]string{"error": "permission denied"})
	}
}
