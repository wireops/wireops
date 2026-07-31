package lint

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/wireops/wireops/internal/policy"
)

func init() {
	Register(RuleLatestTag, ruleLatestTag)
	Register(RuleNoRestartPolicy, ruleNoRestartPolicy)
	Register(RuleNoHealthcheck, ruleNoHealthcheck)
	Register(RuleNoResourceLimits, ruleNoResourceLimits)
	Register(RulePublishedPortAllInterfaces, rulePublishedPortAllInterfaces)
	Register(RulePlaintextSecret, rulePlaintextSecret)
	Register(RuleRelativeBindMount, ruleRelativeBindMount)
	Register(RulePrivileged, rulePrivileged)
	Register(RuleHostNamespace, ruleHostNamespace)
	Register(RuleDockerSocket, ruleDockerSocket)
}

// ruleLatestTag flags images that are unpinned, which makes a deploy
// irreproducible: the same revision can pull different bits tomorrow.
//
// It stays quiet when the worker policy already blocks unpinned images —
// rules_policy.go reports those as errors, and one problem should produce one
// finding.
func ruleLatestTag(cfg *Config, ctx Context) []Finding {
	if ctx.policyBlocks(func(p *policy.WorkerPolicy) bool { return p.PreventLatestImages }) {
		return nil
	}
	var out []Finding
	for _, svc := range cfg.Services {
		image, _ := svc.Raw["image"].(string)
		if image == "" || hasInterpolation(image) {
			continue
		}
		tag, pinned := imageTag(image)
		if pinned {
			continue
		}
		msg := fmt.Sprintf("service %q uses image %q, which is not pinned to a specific version", svc.Name, image)
		if tag == "" {
			msg = fmt.Sprintf("service %q uses image %q with no tag, which resolves to :latest", svc.Name, image)
		}
		out = append(out, Finding{
			Severity: SeverityWarning,
			Service:  svc.Name,
			Path:     fmt.Sprintf("services.%s.image", svc.Name),
			Message:  msg,
			Hint:     "pin an explicit tag or a @sha256 digest so redeploys are reproducible",
		})
	}
	return out
}

// ruleNoRestartPolicy flags services that will stay down after a host reboot
// or a daemon restart.
func ruleNoRestartPolicy(cfg *Config, _ Context) []Finding {
	var out []Finding
	for _, svc := range cfg.Services {
		if restart, _ := svc.Raw["restart"].(string); restart != "" && restart != "no" {
			continue
		}
		if hasDeployRestartPolicy(svc.Raw) {
			continue
		}
		out = append(out, Finding{
			Severity: SeverityWarning,
			Service:  svc.Name,
			Path:     fmt.Sprintf("services.%s.restart", svc.Name),
			Message:  fmt.Sprintf("service %q has no restart policy", svc.Name),
			Hint:     "set `restart: unless-stopped` so the container comes back after a reboot or a crash",
		})
	}
	return out
}

// ruleNoHealthcheck flags services with no healthcheck. Without one, wireops's
// post-deploy check can only tell that a container is running, not that the
// app inside it came up.
func ruleNoHealthcheck(cfg *Config, _ Context) []Finding {
	var out []Finding
	for _, svc := range cfg.Services {
		// Any healthcheck key at all means the author considered it —
		// including `disable: true`, which is an explicit opt-out. Take them
		// at their word rather than nagging.
		if _, present := svc.Raw["healthcheck"]; present {
			continue
		}
		out = append(out, Finding{
			Severity: SeverityInfo,
			Service:  svc.Name,
			Path:     fmt.Sprintf("services.%s.healthcheck", svc.Name),
			Message:  fmt.Sprintf("service %q defines no healthcheck", svc.Name),
			Hint:     "add one so post-deploy checks can tell 'running' apart from 'actually serving'",
		})
	}
	return out
}

// ruleNoResourceLimits flags services with no memory or CPU ceiling, which can
// starve everything else on the worker host.
func ruleNoResourceLimits(cfg *Config, _ Context) []Finding {
	var out []Finding
	for _, svc := range cfg.Services {
		if hasResourceLimit(svc.Raw) {
			continue
		}
		out = append(out, Finding{
			Severity: SeverityInfo,
			Service:  svc.Name,
			Path:     fmt.Sprintf("services.%s.deploy.resources.limits", svc.Name),
			Message:  fmt.Sprintf("service %q sets no memory or CPU limit", svc.Name),
			Hint:     "set `deploy.resources.limits.memory` (or `mem_limit`) so one container cannot starve the host",
		})
	}
	return out
}

// rulePublishedPortAllInterfaces flags ports published without a host IP, which
// exposes them on every interface of the worker host — including public ones,
// bypassing any reverse proxy in front of the stack.
func rulePublishedPortAllInterfaces(cfg *Config, _ Context) []Finding {
	var out []Finding
	for _, svc := range cfg.Services {
		ports, ok := svc.Raw["ports"].([]interface{})
		if !ok {
			continue
		}
		for i, raw := range ports {
			published, hostIP, ok := portBinding(raw)
			if !ok || published == "" {
				continue
			}
			if hostIP != "" && hostIP != "0.0.0.0" && hostIP != "::" {
				continue
			}
			out = append(out, Finding{
				Severity: SeverityWarning,
				Service:  svc.Name,
				Path:     fmt.Sprintf("services.%s.ports[%d]", svc.Name, i),
				Message:  fmt.Sprintf("service %q publishes port %s on all interfaces", svc.Name, published),
				Hint:     "bind to a specific host IP (`127.0.0.1:" + published + ":...`) or drop the host port and route through your reverse proxy",
			})
		}
	}
	return out
}

// secretKeyHints are the substrings that make an environment variable name
// look like it holds a credential.
var secretKeyHints = []string{"PASSWORD", "PASSWD", "SECRET", "TOKEN", "APIKEY", "API_KEY", "PRIVATE_KEY", "ACCESS_KEY", "CREDENTIAL"}

// rulePlaintextSecret flags environment variables that look like credentials
// and carry a literal value, meaning the secret is sitting in the git repo.
// Values that are still interpolations are fine — those come from somewhere
// else at deploy time.
func rulePlaintextSecret(cfg *Config, _ Context) []Finding {
	var out []Finding
	for _, svc := range cfg.Services {
		env := normalizeEnv(svc.Raw["environment"])
		keys := make([]string, 0, len(env))
		for k := range env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := env[k]
			if v == "" || hasInterpolation(v) || !looksLikeSecretKey(k) {
				continue
			}
			out = append(out, Finding{
				Severity: SeverityWarning,
				Service:  svc.Name,
				Path:     fmt.Sprintf("services.%s.environment.%s", svc.Name, k),
				Message:  fmt.Sprintf("service %q sets %s to a literal value", svc.Name, k),
				Hint:     "move it to a stack environment variable marked secret, or to a SOPS-encrypted file",
			})
		}
	}
	return out
}

// ruleRelativeBindMount flags bind mounts that point into the server's own
// repository checkout, which is what a relative path in the compose file
// becomes.
//
// The naive version of this rule — "flag sources that do not start with /" —
// never fires in practice. `docker compose config` resolves relative bind
// sources against the compose file's directory and emits them as absolute
// paths, so by the time the rules run, "./config" has already become
// "/data/repos/<repo-id>/config". That path exists on the *server* and almost
// never on the worker, so the deploy silently creates an empty directory
// there instead of mounting what the author meant.
//
// Detecting it therefore means recognising a source that lands inside the
// checkout, which needs ctx.RepoRoot. Without it the check is skipped rather
// than guessed at. The relative form is still handled for callers that lint a
// raw compose file without going through `docker compose config`.
func ruleRelativeBindMount(cfg *Config, ctx Context) []Finding {
	var out []Finding
	for _, svc := range cfg.Services {
		for _, m := range serviceMounts(svc.Raw) {
			if !m.isBind || m.source == "" {
				continue
			}

			shown := m.source
			if strings.HasPrefix(m.source, "/") {
				rel, inside := underRoot(ctx.RepoRoot, m.source)
				if !inside {
					continue
				}
				// Report the repo-relative form: it is what the author wrote,
				// and it keeps the server's directory layout out of the
				// response.
				shown = "./" + rel
			}

			out = append(out, Finding{
				Severity: SeverityWarning,
				Service:  svc.Name,
				Path:     fmt.Sprintf("services.%s.volumes", svc.Name),
				Message:  fmt.Sprintf("service %q bind-mounts %q, a path inside the repository", svc.Name, shown),
				Hint:     "the worker deploys from a rendered revision directory, not a clone of the repo, so this path will not exist there — use an absolute host path or a named volume",
			})
		}
	}
	return out
}

// underRoot reports whether path is inside root, returning the relative
// remainder when it is. An empty root matches nothing, so the caller's check
// is disabled rather than applied against "".
func underRoot(root, path string) (string, bool) {
	if root == "" {
		return "", false
	}
	cleanRoot := filepath.Clean(root)
	cleanPath := filepath.Clean(path)
	if cleanPath == cleanRoot {
		return "", true
	}
	prefix := cleanRoot + string(filepath.Separator)
	if !strings.HasPrefix(cleanPath, prefix) {
		return "", false
	}
	return strings.TrimPrefix(cleanPath, prefix), true
}

// rulePrivileged flags privileged containers when the worker policy does not
// already block them.
func rulePrivileged(cfg *Config, ctx Context) []Finding {
	if ctx.policyBlocks(func(p *policy.WorkerPolicy) bool { return p.BlockPrivileged }) {
		return nil
	}
	var out []Finding
	for _, svc := range cfg.Services {
		if privileged, _ := svc.Raw["privileged"].(bool); !privileged {
			continue
		}
		out = append(out, Finding{
			Severity: SeverityWarning,
			Service:  svc.Name,
			Path:     fmt.Sprintf("services.%s.privileged", svc.Name),
			Message:  fmt.Sprintf("service %q runs privileged", svc.Name),
			Hint:     "privileged disables container isolation — grant specific capabilities with `cap_add` instead",
		})
	}
	return out
}

// ruleHostNamespace flags services sharing the host's network, PID, or IPC
// namespace, skipping whichever of the three the worker policy already blocks.
func ruleHostNamespace(cfg *Config, ctx Context) []Finding {
	type check struct {
		key     string
		value   string
		label   string
		blocked func(*policy.WorkerPolicy) bool
		hint    string
	}
	checks := []check{
		{"network_mode", "host", "the host network namespace",
			func(p *policy.WorkerPolicy) bool { return p.BlockHostNetwork },
			"publish specific ports instead — host networking bypasses Docker's network isolation entirely"},
		{"pid", "host", "the host PID namespace",
			func(p *policy.WorkerPolicy) bool { return p.BlockHostPID },
			"the container can see and signal every process on the worker host"},
		{"ipc", "host", "the host IPC namespace",
			func(p *policy.WorkerPolicy) bool { return p.BlockHostIPC },
			"the container shares shared memory with every other process on the host"},
	}

	var out []Finding
	for _, svc := range cfg.Services {
		for _, c := range checks {
			if ctx.policyBlocks(c.blocked) {
				continue
			}
			if v, _ := svc.Raw[c.key].(string); v != c.value {
				continue
			}
			out = append(out, Finding{
				Severity: SeverityWarning,
				Service:  svc.Name,
				Path:     fmt.Sprintf("services.%s.%s", svc.Name, c.key),
				Message:  fmt.Sprintf("service %q shares %s", svc.Name, c.label),
				Hint:     c.hint,
			})
		}
	}
	return out
}

var dockerSocketPaths = map[string]bool{
	"/var/run/docker.sock": true,
	"/run/docker.sock":     true,
}

// ruleDockerSocket flags containers given the Docker socket, when the worker
// policy does not already block it. Read-write access to the socket is
// equivalent to root on the worker host.
func ruleDockerSocket(cfg *Config, ctx Context) []Finding {
	if ctx.policyBlocks(func(p *policy.WorkerPolicy) bool { return p.BlockDockerSocket }) {
		return nil
	}
	var out []Finding
	for _, svc := range cfg.Services {
		for _, m := range serviceMounts(svc.Raw) {
			if !dockerSocketPaths[strings.TrimSuffix(m.source, "/")] {
				continue
			}
			msg := fmt.Sprintf("service %q mounts the Docker socket read-write", svc.Name)
			hint := "read-write access to the socket is root on the worker host — add `:ro`, or use a socket proxy that filters the API"
			if m.readOnly {
				msg = fmt.Sprintf("service %q mounts the Docker socket", svc.Name)
				hint = "even read-only, the socket exposes every container on the host — prefer a socket proxy that filters the API"
			}
			out = append(out, Finding{
				Severity: SeverityWarning,
				Service:  svc.Name,
				Path:     fmt.Sprintf("services.%s.volumes", svc.Name),
				Message:  msg,
				Hint:     hint,
			})
		}
	}
	return out
}

// hasInterpolation reports whether s contains a variable reference, meaning
// its real value is only known at deploy time. Rules that inspect literal
// values (an image tag, a password) skip such values rather than judging the
// unexpanded text.
func hasInterpolation(s string) bool {
	return len(parseInterpolations(s)) > 0
}

func looksLikeSecretKey(key string) bool {
	upper := strings.ToUpper(key)
	for _, hint := range secretKeyHints {
		if strings.Contains(upper, hint) {
			return true
		}
	}
	return false
}

// imageTag splits an image reference into its tag and reports whether that
// reference is pinned. A @sha256 digest counts as pinned regardless of tag;
// a missing tag or ":latest" does not.
func imageTag(image string) (tag string, pinned bool) {
	if idx := strings.Index(image, "@"); idx != -1 {
		return "", true
	}
	lastSlash := strings.LastIndex(image, "/")
	suffix := image[lastSlash+1:]
	colon := strings.LastIndex(suffix, ":")
	if colon == -1 {
		return "", false
	}
	tag = suffix[colon+1:]
	return tag, tag != "" && tag != "latest"
}

// hasDeployRestartPolicy reports whether the service sets a restart policy
// through the Swarm-style `deploy.restart_policy` block instead of `restart:`.
func hasDeployRestartPolicy(svc map[string]interface{}) bool {
	deploy, ok := svc["deploy"].(map[string]interface{})
	if !ok {
		return false
	}
	rp, ok := deploy["restart_policy"].(map[string]interface{})
	if !ok {
		return false
	}
	condition, _ := rp["condition"].(string)
	return condition != "" && condition != "none"
}

// hasResourceLimit reports whether the service caps memory or CPU, through
// either the Compose Spec `deploy.resources.limits` block or the legacy
// top-level `mem_limit` / `cpus` keys.
func hasResourceLimit(svc map[string]interface{}) bool {
	if v, ok := svc["mem_limit"]; ok && v != nil {
		return true
	}
	if v, ok := svc["cpus"]; ok && v != nil {
		return true
	}
	deploy, ok := svc["deploy"].(map[string]interface{})
	if !ok {
		return false
	}
	resources, ok := deploy["resources"].(map[string]interface{})
	if !ok {
		return false
	}
	limits, ok := resources["limits"].(map[string]interface{})
	if !ok {
		return false
	}
	for _, key := range []string{"memory", "cpus"} {
		if v, ok := limits[key]; ok && v != nil && v != "" {
			return true
		}
	}
	return false
}

// portBinding normalizes one entry of a service's `ports:` list, returning the
// published host port and the host IP it binds to. `docker compose config`
// emits the long map form, but the short string form is handled too so the
// rule also works on a config that was never round-tripped through the CLI.
func portBinding(raw interface{}) (published, hostIP string, ok bool) {
	switch v := raw.(type) {
	case map[string]interface{}:
		published = portNumber(v["published"])
		hostIP, _ = v["host_ip"].(string)
		return published, hostIP, true
	case string:
		// Strip the protocol suffix, then read right-to-left:
		// "target", "published:target", "ip:published:target".
		spec := v
		if idx := strings.Index(spec, "/"); idx != -1 {
			spec = spec[:idx]
		}
		parts := strings.Split(spec, ":")
		switch len(parts) {
		case 1:
			return "", "", true // container port only, not published
		case 2:
			return parts[0], "", true
		default:
			return parts[len(parts)-2], strings.Join(parts[:len(parts)-2], ":"), true
		}
	}
	return "", "", false
}

// portNumber renders a `docker compose config` JSON port field (decoded as
// float64, string, or absent) as a plain integer string, avoiding fmt.Sprint's
// scientific-notation rendering of large float64 values.
func portNumber(v interface{}) string {
	switch n := v.(type) {
	case float64:
		if n == 0 {
			return ""
		}
		return strconv.FormatFloat(n, 'f', 0, 64)
	case string:
		return n
	default:
		return ""
	}
}

// normalizeEnv flattens a service's `environment` into a map, accepting both
// the map form and the "KEY=value" list form.
func normalizeEnv(raw interface{}) map[string]string {
	out := map[string]string{}
	switch v := raw.(type) {
	case map[string]interface{}:
		for k, val := range v {
			switch s := val.(type) {
			case string:
				out[k] = s
			case nil:
				out[k] = ""
			default:
				out[k] = fmt.Sprint(s)
			}
		}
	case []interface{}:
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				continue
			}
			k, val, found := strings.Cut(s, "=")
			if !found {
				continue
			}
			out[k] = val
		}
	}
	return out
}
