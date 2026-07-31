// Package lint runs static checks over a resolved Docker Compose config and
// reports every problem it finds, rather than stopping at the first one.
//
// Everything here is pure analysis of an already-decoded compose config map —
// no Docker daemon, no network, no filesystem. That is deliberate: the wireops
// server holds the Docker CLI (for `docker compose config`, a client-side
// parse) but never talks to a Docker daemon, so any check needing daemon state
// — does this image exist in the registry, is this host port already taken —
// belongs on a worker and is out of scope for this package.
//
// The checks come in two layers:
//
//   - Structural: things a compose file can express but that will not work, or
//     that indicate an unfinished file (a service with no image, an unresolved
//     ${VAR}).
//   - Advisory: things that work but are worth flagging — an unpinned image
//     tag, a missing restart policy, a port published on every interface, a
//     password sitting in plaintext.
//
// Worker-policy breaches are folded in via [policy.WorkerPolicy.ComposeViolations]
// and reported at error severity, so a lint report is a superset of what the
// deploy-time policy check would reject.
//
// This package itself makes no enforcement decision — Run always returns
// every finding regardless of severity. Callers decide what to do with the
// result: the deploy-time renderer (internal/sync/renderer.go) and stack
// creation (internal/routes) both refuse to proceed when the report has any
// error-severity finding; other callers, such as the compose lint preview
// route, surface the same report purely as advice.
package lint

import (
	"sort"
	"strconv"
	"strings"

	"github.com/wireops/wireops/internal/policy"
)

// Severity ranks a finding. Only worker-policy breaches (and configs that
// cannot deploy at all) reach SeverityError; every advisory check tops out at
// SeverityWarning, so "has errors" stays a meaningful signal.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// Rule IDs, one constant per Register call across the rules_*.go files.
// Using the constant at both the Register call site and in ruleTitles keeps
// the two from drifting apart — a typo in a raw string literal on either side
// would otherwise silently produce an untitled or duplicate rule.
const (
	RuleLatestTag                  = "compose/latest-tag"
	RuleNoRestartPolicy            = "compose/no-restart-policy"
	RuleNoHealthcheck              = "compose/no-healthcheck"
	RuleNoResourceLimits           = "compose/no-resource-limits"
	RulePublishedPortAllInterfaces = "compose/published-port-all-interfaces"
	RulePlaintextSecret            = "compose/plaintext-secret"
	RuleRelativeBindMount          = "compose/relative-bind-mount"
	RulePrivileged                 = "compose/privileged"
	RuleHostNamespace              = "compose/host-namespace"
	RuleDockerSocket               = "compose/docker-socket"
	RuleNoServices                 = "compose/no-services"
	RuleMissingImage               = "compose/missing-image"
	RuleUndeclaredNetwork          = "compose/undeclared-network"
	RuleUndeclaredVolume           = "compose/undeclared-volume"
	RuleUnresolvedVariable         = "compose/unresolved-variable"
	RuleObsoleteVersionKey         = "compose/obsolete-version-key"
	RulePolicyWorkerPolicy         = "policy/worker-policy"
)

// Finding is a single problem found in a compose config.
type Finding struct {
	// Rule is the stable identifier of the check that produced this finding,
	// namespaced by source: "compose/..." for built-in checks, "policy/..."
	// for worker-policy breaches.
	Rule string `json:"rule"`
	// Title is a short, human-readable label for the rule (e.g. "Undeclared
	// network"), for surfaces that show findings as cards rather than a flat
	// list. Rules leave it unset; Run fills it in from ruleTitles so there is
	// one place to update when a rule's wording changes.
	Title    string   `json:"title"`
	Severity Severity `json:"severity"`
	// Service is the compose service the finding belongs to, empty for
	// findings about the file as a whole.
	Service string `json:"service,omitempty"`
	// Path locates the finding inside the config, in dotted form
	// (e.g. "services.web.ports[0]"). Best-effort; may be empty.
	Path string `json:"path,omitempty"`
	// Line is the 1-based line in the *source* compose file this finding
	// points at, or 0 when it could not be located. Set by ResolveLines, not
	// by the rules — Run alone leaves it zero, since the resolved config the
	// rules see carries no position information.
	Line    int    `json:"line,omitempty"`
	Message string `json:"message"`
	// Hint is an optional suggested fix.
	Hint string `json:"hint,omitempty"`
}

// Report is the result of a lint run.
type Report struct {
	Findings []Finding `json:"findings"`
	Errors   int       `json:"errors"`
	Warnings int       `json:"warnings"`
	Infos    int       `json:"infos"`
}

// HasErrors reports whether the run produced any error-severity finding.
func (r Report) HasErrors() bool { return r.Errors > 0 }

// Summary renders the counts as a short one-line string for logs and for the
// deploy timeline's phase detail.
func (r Report) Summary() string {
	if len(r.Findings) == 0 {
		return "no findings"
	}
	parts := make([]string, 0, 3)
	if r.Errors > 0 {
		parts = append(parts, plural(r.Errors, "error"))
	}
	if r.Warnings > 0 {
		parts = append(parts, plural(r.Warnings, "warning"))
	}
	if r.Infos > 0 {
		parts = append(parts, plural(r.Infos, "info"))
	}
	return strings.Join(parts, ", ")
}

func plural(n int, word string) string {
	s := strconv.Itoa(n) + " " + word
	if n != 1 {
		s += "s"
	}
	return s
}

// Service is one compose service paired with its name, so rules do not each
// have to repeat the map type assertion.
type Service struct {
	Name string
	Raw  map[string]interface{}
}

// Config is a decoded compose config with its services pulled out in name
// order. Rules iterate Services rather than the raw map so that findings come
// out in a stable order instead of Go's randomized map iteration order.
type Config struct {
	Raw      map[string]interface{}
	Services []Service
}

// NewConfig builds the rule-facing view over a decoded compose config, as
// produced by compose.ParseConfigJSON.
func NewConfig(raw map[string]interface{}) *Config {
	c := &Config{Raw: raw}
	svcs, ok := raw["services"].(map[string]interface{})
	if !ok {
		return c
	}
	names := make([]string, 0, len(svcs))
	for name := range svcs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		svc, ok := svcs[name].(map[string]interface{})
		if !ok {
			continue
		}
		c.Services = append(c.Services, Service{Name: name, Raw: svc})
	}
	return c
}

// topLevelKeys returns the names declared under a top-level block such as
// "networks" or "volumes". A block present but null (valid compose for
// "use the defaults") yields an empty set, not a missing one.
func (c *Config) topLevelKeys(block string) map[string]bool {
	out := map[string]bool{}
	m, ok := c.Raw[block].(map[string]interface{})
	if !ok {
		return out
	}
	for name := range m {
		out[name] = true
	}
	return out
}

// Context carries what rules need beyond the compose config itself.
type Context struct {
	// Policy is the effective worker policy for the target worker. When nil,
	// policy-derived findings are skipped and the advisory rules that would
	// otherwise defer to the policy report on their own.
	Policy *policy.WorkerPolicy

	// EnvKeys is the set of variable names that will be defined at deploy
	// time (stack env vars, global vars, the repo's own .env). A nil map
	// means "not known", which disables the undefined-variable check rather
	// than reporting every ${VAR} in the file as missing — wireops renders
	// with --no-interpolate, so surviving references are expected.
	EnvKeys map[string]bool

	// RepoRoot is the server-side path of the repository checkout the config
	// was resolved from. `docker compose config` rewrites relative bind-mount
	// sources into absolute server paths, so this is what lets a rule tell a
	// path that was written relative (and therefore will not exist on the
	// worker) apart from one the author really did write as an absolute host
	// path. Empty disables that check.
	RepoRoot string
}

// policyBlocks reports whether the context has a policy that is both active
// and has the given flag set. Advisory rules use it to stay quiet about a
// condition the policy adapter is already reporting as an error.
func (c Context) policyBlocks(flag func(*policy.WorkerPolicy) bool) bool {
	if c.Policy == nil || c.Policy.Disabled {
		return false
	}
	return flag(c.Policy)
}

// Rule is a single check. Check must be pure and must not depend on map
// iteration order for the order of the findings it returns.
type Rule interface {
	ID() string
	Check(cfg *Config, ctx Context) []Finding
}

type ruleFunc struct {
	id string
	fn func(*Config, Context) []Finding
}

func (r ruleFunc) ID() string                             { return r.id }
func (r ruleFunc) Check(c *Config, ctx Context) []Finding { return r.fn(c, ctx) }

var registry []Rule

// Register adds a rule to the global registry. Rule files call it from init(),
// mirroring how internal/integrations registers its plugins. Run sorts by ID,
// so registration order does not affect output.
func Register(id string, fn func(*Config, Context) []Finding) {
	registry = append(registry, ruleFunc{id: id, fn: fn})
}

// Rules returns the registered rule IDs in the order Run applies them.
func Rules() []string {
	ids := make([]string, 0, len(registry))
	for _, r := range registry {
		ids = append(ids, r.ID())
	}
	sort.Strings(ids)
	return ids
}

// EnvKeysFromPairs builds Context.EnvKeys from a "KEY=VALUE" slice, the shape
// the reconciler and the env var routes already pass around. It always returns
// a non-nil map, so an empty env set still enables the undefined-variable
// check (correctly: nothing is defined, so every reference is missing).
func EnvKeysFromPairs(pairs []string) map[string]bool {
	keys := make(map[string]bool, len(pairs))
	for _, pair := range pairs {
		if key, _, found := strings.Cut(pair, "="); found && key != "" {
			keys[key] = true
		}
	}
	return keys
}

// ruleTitles maps a rule ID to the short label shown on its finding cards.
// This is the single place to update when a rule's wording changes — add an
// entry here alongside the Register call in the rule's file. Anything left
// unmapped (a rule someone forgets to add, or a future one) falls back to
// humanizeRule instead of rendering blank.
var ruleTitles = map[string]string{
	RuleLatestTag:                       "Unpinned image tag",
	RuleNoRestartPolicy:                 "No restart policy",
	RuleNoHealthcheck:                   "No healthcheck",
	RuleNoResourceLimits:                "No resource limits",
	RulePublishedPortAllInterfaces:      "Port published on all interfaces",
	RulePlaintextSecret:                 "Plaintext secret",
	RuleRelativeBindMount:               "Relative bind mount",
	RulePrivileged:                      "Privileged container",
	RuleHostNamespace:                   "Host namespace shared",
	RuleDockerSocket:                    "Docker socket mounted",
	RuleNoServices:                      "No services defined",
	RuleMissingImage:                    "Missing image",
	RuleUndeclaredNetwork:               "Undeclared network",
	RuleUndeclaredVolume:                "Undeclared volume",
	RuleUnresolvedVariable:              "Unresolved variable",
	RuleObsoleteVersionKey:              "Obsolete version key",
	"policy/" + policyCheckImages:       "Image blocked by policy",
	"policy/" + policyCheckVolumes:      "Volume blocked by policy",
	"policy/" + policyCheckNetworks:     "Network blocked by policy",
	"policy/" + policyCheckPrivileged:   "Privileged blocked by policy",
	"policy/" + policyCheckHostNetwork:  "Host network blocked by policy",
	"policy/" + policyCheckHostPID:      "Host PID blocked by policy",
	"policy/" + policyCheckHostIPC:      "Host IPC blocked by policy",
	"policy/" + policyCheckDockerSocket: "Docker socket blocked by policy",
	"policy/" + policyCheckCapAdd:       "Capability addition blocked by policy",
	"policy/" + policyCheckDevices:      "Device mount blocked by policy",
	"policy/" + policyCheckSecurityOpt:  "Security option blocked by policy",
}

// titleForRule looks up a rule's card title, humanizing the rule ID itself
// when it is not in ruleTitles.
func titleForRule(rule string) string {
	if title, ok := ruleTitles[rule]; ok {
		return title
	}
	return humanizeRule(rule)
}

// humanizeRule turns a rule ID's suffix (the part after the last "/") into a
// sentence-cased label, e.g. "compose/some-new-rule" -> "Some new rule".
func humanizeRule(rule string) string {
	slug := rule
	if idx := strings.LastIndex(rule, "/"); idx != -1 {
		slug = rule[idx+1:]
	}
	words := strings.NewReplacer("-", " ", "_", " ").Replace(slug)
	if words == "" {
		return words
	}
	return strings.ToUpper(words[:1]) + words[1:]
}

// severityRank orders severities most-serious-first for report sorting.
func severityRank(s Severity) int {
	switch s {
	case SeverityError:
		return 0
	case SeverityWarning:
		return 1
	default:
		return 2
	}
}

// Run applies every registered rule to a decoded compose config.
//
// Findings come back sorted by severity, then rule ID, then service — so the
// blocking problems lead, and two runs over the same input always produce
// byte-identical output regardless of Go's map iteration order.
func Run(raw map[string]interface{}, ctx Context) Report {
	cfg := NewConfig(raw)

	ordered := make([]Rule, len(registry))
	copy(ordered, registry)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID() < ordered[j].ID() })

	report := Report{Findings: []Finding{}}
	for _, rule := range ordered {
		for _, f := range rule.Check(cfg, ctx) {
			if f.Rule == "" {
				f.Rule = rule.ID()
			}
			if f.Title == "" {
				f.Title = titleForRule(f.Rule)
			}
			report.Findings = append(report.Findings, f)
			switch f.Severity {
			case SeverityError:
				report.Errors++
			case SeverityWarning:
				report.Warnings++
			default:
				report.Infos++
			}
		}
	}

	sort.SliceStable(report.Findings, func(i, j int) bool {
		a, b := report.Findings[i], report.Findings[j]
		if ra, rb := severityRank(a.Severity), severityRank(b.Severity); ra != rb {
			return ra < rb
		}
		if a.Rule != b.Rule {
			return a.Rule < b.Rule
		}
		return a.Service < b.Service
	})

	return report
}
