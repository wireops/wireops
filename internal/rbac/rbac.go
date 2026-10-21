package rbac

import (
	"net/http"
	"sort"
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
	CapViewWorkers    Capability = "view_workers"
	CapManageWorkers  Capability = "manage_workers"
	CapManageJobs     Capability = "manage_jobs"
	CapManageSettings Capability = "manage_settings"
	CapManageUsers    Capability = "manage_users"
	CapManageSecurity Capability = "manage_security"
	CapViewAuditLogs  Capability = "view_audit_logs"
	CapUseTerminal    Capability = "use_terminal"
	// CapViewWorkerPolicy gates reading a worker's (or the global) deploy
	// security policy. Writing one still needs CapManageSettings: an operator
	// has to know which rule would reject a compose file it is about to
	// deploy, which is a read, not a settings change.
	CapViewWorkerPolicy Capability = "view_worker_policy"
)

var minimumRoleByCapability = map[Capability]string{
	CapViewMetrics:      RoleMonitoring,
	CapViewStacks:       RoleViewer,
	CapViewJobs:         RoleViewer,
	CapViewLogs:         RoleViewer,
	CapOperateStacks:    RoleOperator,
	CapManageRepos:      RoleOperator,
	CapViewWorkers:      RoleViewer,
	CapManageWorkers:    RoleOperator,
	CapManageJobs:       RoleOperator,
	CapManageSettings:   RoleAdmin,
	CapManageUsers:      RoleAdmin,
	CapManageSecurity:   RoleAdmin,
	CapViewAuditLogs:    RoleAdmin,
	CapUseTerminal:      RoleOperator,
	CapViewWorkerPolicy: RoleOperator,
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

// AllCapabilities returns every known capability, sorted, so a caller can
// enumerate them without reaching into minimumRoleByCapability.
func AllCapabilities() []Capability {
	caps := make([]Capability, 0, len(minimumRoleByCapability))
	for capability := range minimumRoleByCapability {
		caps = append(caps, capability)
	}
	sort.Slice(caps, func(i, j int) bool { return caps[i] < caps[j] })
	return caps
}

// CapabilitiesFor returns the capabilities role actually satisfies, sorted.
// It is derived from the same minimumRoleByCapability table Can consults, so
// an introspection endpoint can never drift from what Require enforces. An
// unknown or empty role satisfies nothing.
func CapabilitiesFor(role string) []Capability {
	granted := make([]Capability, 0, len(minimumRoleByCapability))
	for _, capability := range AllCapabilities() {
		if AtLeast(role, MinimumRole(capability)) {
			granted = append(granted, capability)
		}
	}
	return granted
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
		// The "permission denied" text is load-bearing — the frontend's
		// error parser matches on it — so the detail is added alongside it
		// rather than replacing it. Naming the capability is what lets an
		// API client (notably the MCP server) report *what* is missing
		// instead of just that something was.
		role, _, _ := ResolveActor(e)
		return e.JSON(http.StatusForbidden, map[string]string{
			"error":               "permission denied",
			"required_capability": string(capability),
			"minimum_role":        MinimumRole(capability),
			"actor_role":          role,
		})
	}
}

// RequireSuperuser gates a route to an actual PocketBase superuser session
// (the "_superusers" collection), deliberately stricter than a wireops
// role=admin user (see ResolveActor — role=admin is not the same auth
// principal as a real superuser). Use only for operations dangerous enough
// to warrant that extra bar, e.g. uploading an arbitrary file that later
// becomes a full-data-replace restore target.
func RequireSuperuser() func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		if e != nil && e.Auth != nil && e.Auth.IsSuperuser() {
			return e.Next()
		}
		if e == nil {
			return nil
		}
		if e.Auth == nil {
			return e.JSON(http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		}
		return e.JSON(http.StatusForbidden, map[string]string{"error": "this action requires a PocketBase superuser session, not just a wireops admin role"})
	}
}
