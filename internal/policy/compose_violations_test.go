package policy_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/wireops/wireops/internal/policy"
)

func decodeConfig(t *testing.T, raw string) map[string]interface{} {
	t.Helper()
	var cfg map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("failed to decode config: %v", err)
	}
	return cfg
}

// TestComposeViolationsMatchesValidateComposeConfig pins the invariant the two
// paths depend on: ComposeViolations must find a violation exactly when
// ValidateComposeConfig rejects the config, and its first message for that
// kind must be the text ValidateComposeConfig would have returned. Lint
// reports the full list; the renderer still blocks on the first.
func TestComposeViolationsMatchesValidateComposeConfig(t *testing.T) {
	cases := []struct {
		name   string
		policy *policy.WorkerPolicy
		config string
	}{
		{
			name:   "unpinned image",
			policy: &policy.WorkerPolicy{PreventLatestImages: true},
			config: `{"services":{"web":{"image":"nginx:latest"}}}`,
		},
		{
			name:   "image not in allowlist",
			policy: &policy.WorkerPolicy{AllowedImages: []string{"nginx:*"}},
			config: `{"services":{"web":{"image":"redis:7"}}}`,
		},
		{
			name:   "host bind mount blocked",
			policy: &policy.WorkerPolicy{BlockHostVolumes: true},
			config: `{"services":{"web":{"image":"nginx:1.25","volumes":[{"type":"bind","source":"/opt/data","target":"/data"}]}}}`,
		},
		{
			name:   "network not in allowlist",
			policy: &policy.WorkerPolicy{AllowedNetworks: []string{"allowed"}},
			config: `{"services":{"web":{"image":"nginx:1.25","networks":{"forbidden":{}}}}}`,
		},
		{
			name:   "privileged blocked",
			policy: &policy.WorkerPolicy{BlockPrivileged: true},
			config: `{"services":{"web":{"image":"nginx:1.25","privileged":true}}}`,
		},
		{
			name:   "host network blocked",
			policy: &policy.WorkerPolicy{BlockHostNetwork: true},
			config: `{"services":{"web":{"image":"nginx:1.25","network_mode":"host"}}}`,
		},
		{
			name:   "host pid blocked",
			policy: &policy.WorkerPolicy{BlockHostPID: true},
			config: `{"services":{"web":{"image":"nginx:1.25","pid":"host"}}}`,
		},
		{
			name:   "host ipc blocked",
			policy: &policy.WorkerPolicy{BlockHostIPC: true},
			config: `{"services":{"web":{"image":"nginx:1.25","ipc":"host"}}}`,
		},
		{
			name:   "docker socket blocked",
			policy: &policy.WorkerPolicy{BlockDockerSocket: true},
			config: `{"services":{"web":{"image":"nginx:1.25","volumes":["/var/run/docker.sock:/var/run/docker.sock"]}}}`,
		},
		{
			name:   "docker socket behind a named volume",
			policy: &policy.WorkerPolicy{BlockDockerSocket: true},
			config: `{"services":{"web":{"image":"nginx:1.25","volumes":[{"type":"volume","source":"sock","target":"/sock"}]}},"volumes":{"sock":{"driver_opts":{"device":"/var/run/docker.sock"}}}}`,
		},
		{
			name:   "cap_add not in allowlist",
			policy: &policy.WorkerPolicy{AllowedCapAdd: []string{"NET_ADMIN"}},
			config: `{"services":{"web":{"image":"nginx:1.25","cap_add":["SYS_ADMIN"]}}}`,
		},
		{
			name:   "device not in allowlist",
			policy: &policy.WorkerPolicy{AllowedDevices: []string{"/dev/ttyUSB0"}},
			config: `{"services":{"web":{"image":"nginx:1.25","devices":["/dev/sda:/dev/sda"]}}}`,
		},
		{
			name:   "security_opt not in allowlist",
			policy: &policy.WorkerPolicy{AllowedSecurityOpt: []string{"no-new-privileges:true"}},
			config: `{"services":{"web":{"image":"nginx:1.25","security_opt":["seccomp:unconfined"]}}}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := decodeConfig(t, tc.config)

			err := tc.policy.ValidateComposeConfig(cfg)
			violations := tc.policy.ComposeViolations(cfg)

			if err == nil {
				t.Fatalf("ValidateComposeConfig returned nil, want a violation")
			}
			if len(violations) == 0 {
				t.Fatalf("ComposeViolations returned none, but ValidateComposeConfig returned %v", err)
			}
			if violations[0].Message != err.Error() {
				t.Errorf("first violation message = %q, want the blocking error %q", violations[0].Message, err.Error())
			}
			if violations[0].Service != "web" {
				t.Errorf("violation service = %q, want %q", violations[0].Service, "web")
			}
			if violations[0].Check == "" {
				t.Error("violation has no Check set")
			}
		})
	}
}

func TestComposeViolationsCleanConfig(t *testing.T) {
	wp := &policy.WorkerPolicy{
		PreventLatestImages: true,
		BlockPrivileged:     true,
		BlockDockerSocket:   true,
		BlockHostVolumes:    true,
	}
	cfg := decodeConfig(t, `{"services":{"web":{"image":"nginx:1.25.3","volumes":[{"type":"volume","source":"data","target":"/data"}]}},"volumes":{"data":{}}}`)

	if err := wp.ValidateComposeConfig(cfg); err != nil {
		t.Fatalf("ValidateComposeConfig = %v, want nil", err)
	}
	if got := wp.ComposeViolations(cfg); len(got) != 0 {
		t.Errorf("ComposeViolations = %+v, want none", got)
	}
}

func TestComposeViolationsReturnsNothingWhenPolicyDisabled(t *testing.T) {
	wp := &policy.WorkerPolicy{Disabled: true, PreventLatestImages: true}
	cfg := decodeConfig(t, `{"services":{"web":{"image":"nginx:latest"}}}`)

	if got := wp.ComposeViolations(cfg); len(got) != 0 {
		t.Errorf("ComposeViolations = %+v, want none with the policy disabled", got)
	}
}

// TestComposeViolationsCollectsAcrossServices is the behaviour lint depends on
// and ValidateComposeConfig deliberately does not provide.
func TestComposeViolationsCollectsAcrossServices(t *testing.T) {
	wp := &policy.WorkerPolicy{PreventLatestImages: true, BlockPrivileged: true}
	cfg := decodeConfig(t, `{
		"services": {
			"c": {"image": "three:latest"},
			"a": {"image": "one:latest", "privileged": true},
			"b": {"image": "two:latest"}
		}
	}`)

	violations := wp.ComposeViolations(cfg)
	if len(violations) != 4 {
		t.Fatalf("violations = %d, want 4 (3 unpinned images + 1 privileged): %+v", len(violations), violations)
	}

	// Services are visited in sorted order, so the report is stable across
	// runs despite Go's randomized map iteration.
	var imageServices []string
	for _, v := range violations {
		if v.Check == "images" {
			imageServices = append(imageServices, v.Service)
		}
	}
	if strings.Join(imageServices, ",") != "a,b,c" {
		t.Errorf("image violation services = %v, want [a b c] in that order", imageServices)
	}
}
