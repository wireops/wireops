package lint

import "fmt"

func init() {
	Register("policy/worker-policy", rulePolicyViolations)
}

// policyCheckPaths maps a policy.Violation.Check to the compose key it came
// from, so a policy finding points at the same place in the file as the
// built-in rules do.
var policyCheckPaths = map[string]string{
	"images":        "image",
	"volumes":       "volumes",
	"networks":      "networks",
	"privileged":    "privileged",
	"host_network":  "network_mode",
	"host_pid":      "pid",
	"host_ipc":      "ipc",
	"docker_socket": "volumes",
	"cap_add":       "cap_add",
	"devices":       "devices",
	"security_opt":  "security_opt",
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
