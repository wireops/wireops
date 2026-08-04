package lint_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/wireops/wireops/internal/lint"
	"github.com/wireops/wireops/internal/policy"
)

// mustConfig decodes a compose config the way compose.ParseConfigJSON does, so
// the tests exercise rules against the same shape `docker compose config
// --format json` produces.
func mustConfig(t *testing.T, raw string) map[string]interface{} {
	t.Helper()
	var cfg map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("failed to decode test config: %v", err)
	}
	return cfg
}

// findingsFor returns the messages of every finding produced by ruleID.
func findingsFor(report lint.Report, ruleID string) []string {
	var out []string
	for _, f := range report.Findings {
		if f.Rule == ruleID {
			out = append(out, f.Message)
		}
	}
	return out
}

func hasRule(report lint.Report, ruleID string) bool {
	return len(findingsFor(report, ruleID)) > 0
}

func TestRunReportsNothingForACleanConfig(t *testing.T) {
	cfg := mustConfig(t, `{
		"services": {
			"web": {
				"image": "nginx:1.25.3",
				"restart": "unless-stopped",
				"healthcheck": {"test": ["CMD", "true"]},
				"mem_limit": "256m",
				"ports": [{"target": 80, "published": "8080", "host_ip": "127.0.0.1"}]
			}
		}
	}`)

	report := lint.Run(cfg, lint.Context{})
	if len(report.Findings) != 0 {
		t.Fatalf("expected no findings, got %d: %+v", len(report.Findings), report.Findings)
	}
	if report.Summary() != "no findings" {
		t.Errorf("Summary() = %q, want %q", report.Summary(), "no findings")
	}
}

func TestRunFlagsUnpinnedImages(t *testing.T) {
	cases := []struct {
		name  string
		image string
		want  bool
	}{
		{"no tag", "nginx", true},
		{"latest tag", "nginx:latest", true},
		{"pinned tag", "nginx:1.25.3", false},
		{"digest", "nginx@sha256:abc123", false},
		{"registry port with tag", "registry.example.com:5000/app:2.1", false},
		{"registry port without tag", "registry.example.com:5000/app", true},
		{"interpolated", "nginx:${TAG}", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := mustConfig(t, `{"services":{"web":{"image":"`+tc.image+`","restart":"always"}}}`)
			report := lint.Run(cfg, lint.Context{})
			if got := hasRule(report, "compose/latest-tag"); got != tc.want {
				t.Errorf("latest-tag finding for %q = %v, want %v", tc.image, got, tc.want)
			}
		})
	}
}

func TestRunFlagsPortsPublishedOnAllInterfaces(t *testing.T) {
	cases := []struct {
		name string
		port string
		want bool
	}{
		{"long form no host ip", `{"target": 80, "published": "8080"}`, true},
		{"long form all interfaces", `{"target": 80, "published": "8080", "host_ip": "0.0.0.0"}`, true},
		{"long form loopback", `{"target": 80, "published": "8080", "host_ip": "127.0.0.1"}`, false},
		{"short form host port", `"8080:80"`, true},
		{"short form loopback", `"127.0.0.1:8080:80"`, false},
		{"short form container port only", `"80"`, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := mustConfig(t, `{"services":{"web":{"image":"nginx:1.25","restart":"always","ports":[`+tc.port+`]}}}`)
			report := lint.Run(cfg, lint.Context{})
			if got := hasRule(report, "compose/published-port-all-interfaces"); got != tc.want {
				t.Errorf("published-port finding for %s = %v, want %v", tc.port, got, tc.want)
			}
		})
	}
}

func TestRunFlagsPlaintextSecretsButNotInterpolatedOnes(t *testing.T) {
	cfg := mustConfig(t, `{
		"services": {
			"db": {
				"image": "postgres:16",
				"restart": "always",
				"environment": {
					"POSTGRES_PASSWORD": "hunter2",
					"POSTGRES_USER": "app",
					"API_TOKEN": "${API_TOKEN}",
					"LOG_LEVEL": "debug"
				}
			}
		}
	}`)

	msgs := findingsFor(lint.Run(cfg, lint.Context{}), "compose/plaintext-secret")
	if len(msgs) != 1 {
		t.Fatalf("expected exactly 1 plaintext-secret finding, got %d: %v", len(msgs), msgs)
	}
	if !strings.Contains(msgs[0], "POSTGRES_PASSWORD") {
		t.Errorf("finding = %q, want it to name POSTGRES_PASSWORD", msgs[0])
	}
}

func TestRunOnlyFlagsVariablesNothingDefines(t *testing.T) {
	cfg := mustConfig(t, `{
		"services": {
			"web": {
				"image": "nginx:${TAG}",
				"restart": "always",
				"environment": {
					"KNOWN": "${DEFINED_VAR}",
					"UNKNOWN": "${MISSING_VAR}",
					"DEFAULTED": "${ALSO_MISSING:-fallback}",
					"ESCAPED": "$$NOT_A_VAR"
				}
			}
		}
	}`)

	// Without EnvKeys the check is off entirely: wireops renders with
	// --no-interpolate, so a surviving ${VAR} is normal, not a finding.
	if got := findingsFor(lint.Run(cfg, lint.Context{}), "compose/unresolved-variable"); len(got) != 0 {
		t.Fatalf("expected no findings without EnvKeys, got %v", got)
	}

	report := lint.Run(cfg, lint.Context{
		EnvKeys: lint.EnvKeysFromPairs([]string{"DEFINED_VAR=x", "TAG=1.25"}),
	})
	msgs := findingsFor(report, "compose/unresolved-variable")
	if len(msgs) != 1 {
		t.Fatalf("expected exactly 1 unresolved-variable finding, got %d: %v", len(msgs), msgs)
	}
	if !strings.Contains(msgs[0], "MISSING_VAR") {
		t.Errorf("finding = %q, want it to name MISSING_VAR", msgs[0])
	}
	for _, unwanted := range []string{"DEFINED_VAR", "ALSO_MISSING", "NOT_A_VAR", "TAG"} {
		if strings.Contains(msgs[0], unwanted) {
			t.Errorf("finding = %q, should not name %s", msgs[0], unwanted)
		}
	}

	var found *lint.Finding
	for i := range report.Findings {
		if report.Findings[i].Rule == "compose/unresolved-variable" {
			found = &report.Findings[i]
		}
	}
	if found == nil {
		t.Fatal("expected an unresolved-variable finding")
	}
	if len(found.Vars) != 1 || found.Vars[0] != "MISSING_VAR" {
		t.Fatalf("Finding.Vars = %v, want [MISSING_VAR]", found.Vars)
	}
}

// TestRunUnresolvedVariableVarsListsEveryMissingKeyInOrder covers a service
// with more than one missing variable: Vars must match Message's contents
// exactly (sorted, deduplicated), not just carry the first one.
func TestRunUnresolvedVariableVarsListsEveryMissingKeyInOrder(t *testing.T) {
	cfg := mustConfig(t, `{
		"services": {
			"web": {
				"image": "nginx:latest",
				"environment": {
					"A": "${ZETA_VAR}",
					"B": "${ALPHA_VAR}",
					"C": "${ALPHA_VAR}"
				}
			}
		}
	}`)

	report := lint.Run(cfg, lint.Context{EnvKeys: lint.EnvKeysFromPairs(nil)})
	msgs := findingsFor(report, "compose/unresolved-variable")
	if len(msgs) != 1 {
		t.Fatalf("expected exactly 1 unresolved-variable finding, got %d: %v", len(msgs), msgs)
	}

	var found *lint.Finding
	for i := range report.Findings {
		if report.Findings[i].Rule == "compose/unresolved-variable" {
			found = &report.Findings[i]
		}
	}
	if found == nil {
		t.Fatal("expected an unresolved-variable finding")
	}
	want := []string{"ALPHA_VAR", "ZETA_VAR"}
	if len(found.Vars) != len(want) {
		t.Fatalf("Finding.Vars = %v, want %v", found.Vars, want)
	}
	for i, v := range want {
		if found.Vars[i] != v {
			t.Fatalf("Finding.Vars = %v, want %v", found.Vars, want)
		}
	}
}

func TestRunFlagsUnescapedConfigInterpolation(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{"bare var", `server { proxy_pass http://x:$PORT; }`, []string{"PORT"}},
		{"braced var", `server { proxy_pass http://x:${PORT}; }`, []string{"PORT"}},
		{"braced var with default", `server { listen ${PORT:-8080}; }`, []string{"PORT"}},
		{"nginx var, not compose var", `location / { try_files $uri /index.html; }`, []string{"uri"}},
		{"escaped dollar", `location / { try_files $$uri /index.html; }`, nil},
		{"no dollar at all", `server { listen 80; }`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := map[string]interface{}{
				"services": map[string]interface{}{
					"web": map[string]interface{}{"image": "nginx:1.25.3"},
				},
				"configs": map[string]interface{}{
					"nginx-conf": map[string]interface{}{"content": tt.content},
				},
			}
			got := findingsFor(lint.Run(raw, lint.Context{}), "compose/unescaped-config-interpolation")
			if len(tt.want) == 0 {
				if len(got) != 0 {
					t.Fatalf("expected no findings, got %v", got)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("expected 1 finding, got %d: %v", len(got), got)
			}
			for _, name := range tt.want {
				if !strings.Contains(got[0], name) {
					t.Errorf("finding %q does not mention variable %q", got[0], name)
				}
			}
		})
	}
}

func TestRunFlagsUndeclaredNetworksAndVolumes(t *testing.T) {
	cfg := mustConfig(t, `{
		"services": {
			"web": {
				"image": "nginx:1.25",
				"restart": "always",
				"networks": {"declared": {}, "missing": {}},
				"volumes": [
					{"type": "volume", "source": "data", "target": "/data"},
					{"type": "volume", "source": "ghost", "target": "/ghost"},
					{"type": "bind", "source": "/opt/app", "target": "/app"}
				]
			}
		},
		"networks": {"declared": {}},
		"volumes": {"data": {}}
	}`)

	report := lint.Run(cfg, lint.Context{})

	nets := findingsFor(report, "compose/undeclared-network")
	if len(nets) != 1 || !strings.Contains(nets[0], "missing") {
		t.Errorf("undeclared-network findings = %v, want exactly one naming \"missing\"", nets)
	}

	vols := findingsFor(report, "compose/undeclared-volume")
	if len(vols) != 1 || !strings.Contains(vols[0], "ghost") {
		t.Errorf("undeclared-volume findings = %v, want exactly one naming \"ghost\"", vols)
	}
}

func TestRunFlagsRiskyRuntimeOptionsWhenPolicyAllowsThem(t *testing.T) {
	cfg := mustConfig(t, `{
		"services": {
			"agent": {
				"image": "agent:1.0",
				"restart": "always",
				"privileged": true,
				"network_mode": "host",
				"pid": "host",
				"volumes": [{"type": "bind", "source": "/var/run/docker.sock", "target": "/var/run/docker.sock"}]
			}
		}
	}`)

	report := lint.Run(cfg, lint.Context{})
	for _, rule := range []string{"compose/privileged", "compose/host-namespace", "compose/docker-socket"} {
		if !hasRule(report, rule) {
			t.Errorf("expected a %s finding with no policy in context", rule)
		}
	}
	if report.Errors != 0 {
		t.Errorf("advisory rules produced %d error-severity findings, want 0", report.Errors)
	}
}

func TestRunDefersToPolicyInsteadOfDoubleReporting(t *testing.T) {
	cfg := mustConfig(t, `{
		"services": {
			"agent": {
				"image": "agent:latest",
				"restart": "always",
				"privileged": true
			}
		}
	}`)

	wp := &policy.WorkerPolicy{
		PreventLatestImages: true,
		BlockPrivileged:     true,
	}
	report := lint.Run(cfg, lint.Context{Policy: wp})

	// The policy adapter owns these two conditions now, so the advisory rules
	// must stay quiet rather than reporting the same problem twice.
	if hasRule(report, "compose/latest-tag") {
		t.Error("compose/latest-tag fired even though the policy blocks unpinned images")
	}
	if hasRule(report, "compose/privileged") {
		t.Error("compose/privileged fired even though the policy blocks privileged")
	}
	if !hasRule(report, "policy/images") {
		t.Error("expected a policy/images finding")
	}
	if !hasRule(report, "policy/privileged") {
		t.Error("expected a policy/privileged finding")
	}
	if report.Errors != 2 {
		t.Errorf("report.Errors = %d, want 2 (the two policy breaches)", report.Errors)
	}
}

func TestRunReportsEveryPolicyBreachNotJustTheFirst(t *testing.T) {
	cfg := mustConfig(t, `{
		"services": {
			"a": {"image": "one:latest", "restart": "always"},
			"b": {"image": "two:latest", "restart": "always"},
			"c": {"image": "three:latest", "restart": "always"}
		}
	}`)

	wp := &policy.WorkerPolicy{PreventLatestImages: true}
	report := lint.Run(cfg, lint.Context{Policy: wp})

	msgs := findingsFor(report, "policy/images")
	if len(msgs) != 3 {
		t.Fatalf("policy/images findings = %d, want 3 (one per service): %v", len(msgs), msgs)
	}
	// ValidateComposeConfig, the blocking path, still stops at the first.
	if err := wp.ValidateComposeConfig(cfg); err == nil {
		t.Error("ValidateComposeConfig returned nil, want the first violation")
	}
}

func TestRunSkipsPolicyFindingsWhenPolicyIsDisabled(t *testing.T) {
	cfg := mustConfig(t, `{"services":{"web":{"image":"nginx:latest","restart":"always"}}}`)

	report := lint.Run(cfg, lint.Context{Policy: &policy.WorkerPolicy{
		Disabled:            true,
		PreventLatestImages: true,
	}})

	if report.Errors != 0 {
		t.Errorf("report.Errors = %d, want 0 with the policy system disabled", report.Errors)
	}
	// With the policy inert, the advisory rule takes over rather than the
	// condition going unreported entirely.
	if !hasRule(report, "compose/latest-tag") {
		t.Error("expected compose/latest-tag to report once the policy is disabled")
	}
}

func TestRunIsDeterministic(t *testing.T) {
	cfg := mustConfig(t, `{
		"services": {
			"zeta":  {"image": "zeta:latest"},
			"alpha": {"image": "alpha:latest"},
			"mid":   {"image": "mid:latest"}
		}
	}`)

	first, err := json.Marshal(lint.Run(cfg, lint.Context{}))
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	// Go randomizes map iteration, so a rule that walked the services map
	// directly would produce a different order on some runs.
	for i := 0; i < 25; i++ {
		next, err := json.Marshal(lint.Run(cfg, lint.Context{}))
		if err != nil {
			t.Fatalf("marshal failed: %v", err)
		}
		if string(next) != string(first) {
			t.Fatalf("run %d differed from the first:\n%s\n%s", i, first, next)
		}
	}
}

func TestRunSortsErrorsBeforeWarningsBeforeInfos(t *testing.T) {
	cfg := mustConfig(t, `{
		"services": {
			"web": {"image": "nginx:latest"}
		}
	}`)

	report := lint.Run(cfg, lint.Context{Policy: &policy.WorkerPolicy{PreventLatestImages: true}})
	if len(report.Findings) < 3 {
		t.Fatalf("expected at least 3 findings, got %d", len(report.Findings))
	}

	rank := map[lint.Severity]int{lint.SeverityError: 0, lint.SeverityWarning: 1, lint.SeverityInfo: 2}
	for i := 1; i < len(report.Findings); i++ {
		prev, cur := report.Findings[i-1], report.Findings[i]
		if rank[prev.Severity] > rank[cur.Severity] {
			t.Fatalf("finding %d (%s) sorts after %d (%s)", i-1, prev.Severity, i, cur.Severity)
		}
	}
}

func TestReportSummary(t *testing.T) {
	cases := []struct {
		name   string
		report lint.Report
		want   string
	}{
		{"empty", lint.Report{}, "no findings"},
		{"one of each", lint.Report{
			Findings: make([]lint.Finding, 3),
			Errors:   1, Warnings: 1, Infos: 1,
		}, "1 error, 1 warning, 1 info"},
		{"plurals and gaps", lint.Report{
			Findings: make([]lint.Finding, 5),
			Warnings: 5,
		}, "5 warnings"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.report.Summary(); got != tc.want {
				t.Errorf("Summary() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEnvKeysFromPairs(t *testing.T) {
	keys := lint.EnvKeysFromPairs([]string{"A=1", "B=", "C", "=orphan", "D=x=y"})
	for _, want := range []string{"A", "B", "D"} {
		if !keys[want] {
			t.Errorf("expected key %q to be present", want)
		}
	}
	for _, unwanted := range []string{"C", ""} {
		if keys[unwanted] {
			t.Errorf("did not expect key %q", unwanted)
		}
	}
	if keys == nil {
		t.Error("EnvKeysFromPairs returned nil, which would disable the check entirely")
	}
}

func TestRunFlagsMissingImageAndBuildOnlyServices(t *testing.T) {
	cfg := mustConfig(t, `{
		"services": {
			"builder": {"build": {"context": "."}, "restart": "always"},
			"empty":   {"restart": "always"}
		}
	}`)

	msgs := findingsFor(lint.Run(cfg, lint.Context{}), "compose/missing-image")
	if len(msgs) != 2 {
		t.Fatalf("missing-image findings = %d, want 2: %v", len(msgs), msgs)
	}
	if !strings.Contains(msgs[0], "build") {
		t.Errorf("build-only finding = %q, want it to mention the build section", msgs[0])
	}
}

func TestRunFlagsRelativeBindMounts(t *testing.T) {
	cfg := mustConfig(t, `{
		"services": {
			"web": {
				"image": "nginx:1.25",
				"restart": "always",
				"volumes": [
					{"type": "bind", "source": "./config", "target": "/etc/nginx"},
					{"type": "bind", "source": "/opt/data", "target": "/data"}
				]
			}
		}
	}`)

	msgs := findingsFor(lint.Run(cfg, lint.Context{}), "compose/relative-bind-mount")
	if len(msgs) != 1 || !strings.Contains(msgs[0], "./config") {
		t.Errorf("relative-bind-mount findings = %v, want exactly one naming ./config", msgs)
	}
}

// TestRunFlagsBindMountsInsideTheRepoCheckout covers what `docker compose
// config` actually emits: a relative bind source like "./config" arrives
// already rewritten to an absolute server path. Flagging only sources that do
// not start with "/" would therefore never fire.
func TestRunFlagsBindMountsInsideTheRepoCheckout(t *testing.T) {
	cfg := mustConfig(t, `{
		"services": {
			"web": {
				"image": "nginx:1.25",
				"restart": "always",
				"volumes": [
					{"type": "bind", "source": "/data/repos/abc123/config", "target": "/etc/nginx"},
					{"type": "bind", "source": "/opt/host-data", "target": "/data"}
				]
			}
		}
	}`)

	// Without RepoRoot the check cannot tell the two apart, so it stays quiet.
	if got := findingsFor(lint.Run(cfg, lint.Context{}), "compose/relative-bind-mount"); len(got) != 0 {
		t.Fatalf("expected no findings without RepoRoot, got %v", got)
	}

	msgs := findingsFor(lint.Run(cfg, lint.Context{RepoRoot: "/data/repos/abc123"}), "compose/relative-bind-mount")
	if len(msgs) != 1 {
		t.Fatalf("relative-bind-mount findings = %d, want exactly 1 (the in-repo mount): %v", len(msgs), msgs)
	}
	// The repo-relative form is what the author wrote, and it keeps the
	// server's directory layout out of the response.
	if !strings.Contains(msgs[0], "./config") {
		t.Errorf("finding = %q, want it to name the path in repo-relative form", msgs[0])
	}
	if strings.Contains(msgs[0], "/data/repos") {
		t.Errorf("finding = %q, must not leak the server workspace path", msgs[0])
	}
}

func TestRunDoesNotFlagHealthcheckOptOut(t *testing.T) {
	cfg := mustConfig(t, `{
		"services": {
			"opted-out": {"image": "a:1", "restart": "always", "healthcheck": {"disable": true}},
			"defined":   {"image": "b:1", "restart": "always", "healthcheck": {"test": ["CMD", "true"]}},
			"missing":   {"image": "c:1", "restart": "always"}
		}
	}`)

	msgs := findingsFor(lint.Run(cfg, lint.Context{}), "compose/no-healthcheck")
	if len(msgs) != 1 {
		t.Fatalf("no-healthcheck findings = %d, want 1 (only the service with no healthcheck key): %v", len(msgs), msgs)
	}
	if !strings.Contains(msgs[0], "missing") {
		t.Errorf("finding = %q, want it to name the service with no healthcheck at all", msgs[0])
	}
}
