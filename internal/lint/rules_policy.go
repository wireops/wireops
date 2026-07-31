package lint

import "fmt"

func init() {
	Register(RulePolicyWorkerPolicy, rulePolicyViolations)
}

// Values policy.Violation.Check can carry — see the field's doc comment on
// [policy.Violation] for the authoritative list. Named here (rather than left
// as raw strings) so policyCheckPaths and ruleTitles can't drift apart.
const (
	policyCheckImages       = "images"
	policyCheckVolumes      = "volumes"
	policyCheckNetworks     = "networks"
	policyCheckPrivileged   = "privileged"
	policyCheckHostNetwork  = "host_network"
	policyCheckHostPID      = "host_pid"
	policyCheckHostIPC      = "host_ipc"
	policyCheckDockerSocket = "docker_socket"
	policyCheckCapAdd       = "cap_add"
	policyCheckDevices      = "devices"
	policyCheckSecurityOpt  = "security_opt"
)

// policyCheckPaths maps a policy.Violation.Check to the compose key it came
// from, so a policy finding points at the same place in the file as the
// built-in rules do.
var policyCheckPaths = map[string]string{
	policyCheckImages:       "image",
	policyCheckVolumes:      "volumes",
	policyCheckNetworks:     "networks",
	policyCheckPrivileged:   "privileged",
	policyCheckHostNetwork:  "network_mode",
	policyCheckHostPID:      "pid",
	policyCheckHostIPC:      "ipc",
	policyCheckDockerSocket: "volumes",
	policyCheckCapAdd:       "cap_add",
	policyCheckDevices:      "devices",
	policyCheckSecurityOpt:  "security_opt",
}

// rulePolicyViolations folds the worker deploy policy into the lint report, so
// a single run tells the author everything wrong with the file — including the
// violations that will actually block the deploy.
//
// These are the only findings lint reports at error severity. The policy check
// in the renderer remains the thing that enforces them; this rule just surfaces
// them earlier, and all at once instead of one per failed deploy.
//
// With no policy in context (nil, or the policy system globally disabled) the
// rule reports nothing, and the advisory rules that normally defer to the
// policy report on their own instead.
func rulePolicyViolations(cfg *Config, ctx Context) []Finding {
	if ctx.Policy == nil || ctx.Policy.Disabled {
		return nil
	}

	var out []Finding
	for _, v := range ctx.Policy.ComposeViolations(cfg.Raw) {
		path := ""
		if key, ok := policyCheckPaths[v.Check]; ok && v.Service != "" {
			path = fmt.Sprintf("services.%s.%s", v.Service, key)
		}
		out = append(out, Finding{
			Rule:     "policy/" + v.Check,
			Severity: SeverityError,
			Service:  v.Service,
			Path:     path,
			Message:  v.Message,
			Hint:     "blocked by this worker's deploy policy — adjust the compose file, or the policy under Settings",
		})
	}
	return out
}
