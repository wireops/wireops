package policy_test

import (
	"testing"

	"github.com/wireops/wireops/internal/policy"
)

// --- ValidateImages ---

func TestValidateImagesEmptyAllowlistAllowsAnything(t *testing.T) {
	p := &policy.WorkerPolicy{AllowedImages: []string{}}
	if err := p.ValidateImages([]string{"nginx:latest", "alpine:3.18", "ghcr.io/myorg/app:v1"}); err != nil {
		t.Errorf("expected no error with empty allowlist, got: %v", err)
	}
}

func TestValidateImagesExactMatch(t *testing.T) {
	p := &policy.WorkerPolicy{AllowedImages: []string{"nginx:latest"}}
	if err := p.ValidateImages([]string{"nginx:latest"}); err != nil {
		t.Errorf("expected exact match to pass, got: %v", err)
	}
}

func TestValidateImagesExactMatchNotInList(t *testing.T) {
	p := &policy.WorkerPolicy{AllowedImages: []string{"nginx:latest"}}
	if err := p.ValidateImages([]string{"alpine:latest"}); err == nil {
		t.Error("expected error for image not in list, got nil")
	}
}

func TestValidateImagesWildcardTag(t *testing.T) {
	p := &policy.WorkerPolicy{AllowedImages: []string{"nginx:*"}}
	for _, img := range []string{"nginx:latest", "nginx:1.25", "nginx:alpine"} {
		if err := p.ValidateImages([]string{img}); err != nil {
			t.Errorf("wildcard tag: expected %q to pass, got: %v", img, err)
		}
	}
}

func TestValidateImagesWildcardTagDifferentRepo(t *testing.T) {
	p := &policy.WorkerPolicy{AllowedImages: []string{"nginx:*"}}
	if err := p.ValidateImages([]string{"apache:latest"}); err == nil {
		t.Error("expected error for different repo with wildcard tag pattern")
	}
}

func TestValidateImagesRegistryWildcard(t *testing.T) {
	p := &policy.WorkerPolicy{AllowedImages: []string{"ghcr.io/myorg/*"}}
	for _, img := range []string{"ghcr.io/myorg/app:v1", "ghcr.io/myorg/worker:latest"} {
		if err := p.ValidateImages([]string{img}); err != nil {
			t.Errorf("registry wildcard: expected %q to pass, got: %v", img, err)
		}
	}
}

func TestValidateImagesRegistryWildcardDifferentOrg(t *testing.T) {
	p := &policy.WorkerPolicy{AllowedImages: []string{"ghcr.io/myorg/*"}}
	if err := p.ValidateImages([]string{"ghcr.io/otherog/app:v1"}); err == nil {
		t.Error("expected error for different org with registry wildcard")
	}
}

func TestValidateImagesMultipleImagesOneViolation(t *testing.T) {
	p := &policy.WorkerPolicy{AllowedImages: []string{"nginx:*"}}
	if err := p.ValidateImages([]string{"nginx:latest", "alpine:latest"}); err == nil {
		t.Error("expected error when one of multiple images is not allowed")
	}
}

// --- ValidateNetwork ---

func TestValidateNetworkEmptyAllowlistAllowsAll(t *testing.T) {
	p := &policy.WorkerPolicy{AllowedNetworks: []string{}}
	if err := p.ValidateNetwork("traefik"); err != nil {
		t.Errorf("expected no error with empty allowlist, got: %v", err)
	}
}

func TestValidateNetworkEmptyNetworkStringAlwaysAllowed(t *testing.T) {
	p := &policy.WorkerPolicy{AllowedNetworks: []string{"traefik"}}
	if err := p.ValidateNetwork(""); err != nil {
		t.Errorf("expected empty network to always be allowed, got: %v", err)
	}
}

func TestValidateNetworkExactMatch(t *testing.T) {
	p := &policy.WorkerPolicy{AllowedNetworks: []string{"traefik", "internal"}}
	if err := p.ValidateNetwork("traefik"); err != nil {
		t.Errorf("expected exact match to pass, got: %v", err)
	}
}

func TestValidateNetworkNotInList(t *testing.T) {
	p := &policy.WorkerPolicy{AllowedNetworks: []string{"traefik"}}
	if err := p.ValidateNetwork("external"); err == nil {
		t.Error("expected error for network not in list, got nil")
	}
}

// --- ValidateVolumes ---

func TestValidateVolumesEmptyAllowlistAllowsAll(t *testing.T) {
	p := &policy.WorkerPolicy{AllowedVolumes: []string{}}
	if err := p.ValidateVolumes([]string{"/data:/data", "/tmp:/tmp", "myvolume:/app"}); err != nil {
		t.Errorf("expected no error with empty allowlist, got: %v", err)
	}
}

func TestValidateVolumesBindMountPrefixMatch(t *testing.T) {
	p := &policy.WorkerPolicy{AllowedVolumes: []string{"/data"}}
	for _, vol := range []string{"/data:/data", "/data/myapp:/app", "/data/logs:/logs:ro"} {
		if err := p.ValidateVolumes([]string{vol}); err != nil {
			t.Errorf("prefix match: expected %q to pass, got: %v", vol, err)
		}
	}

	p2 := &policy.WorkerPolicy{AllowedVolumes: []string{"/var/run/docker.sock"}}
	if err := p2.ValidateVolumes([]string{"/var/run/docker.sock:/var/run/docker.sock"}); err != nil {
		t.Errorf("expected /var/run/docker.sock to pass, got: %v", err)
	}
}

func TestValidateVolumesBindMountNotUnderPrefix(t *testing.T) {
	p := &policy.WorkerPolicy{AllowedVolumes: []string{"/data"}}
	for _, vol := range []string{"/etc:/etc", "/backups:/backups", "/dataextra:/dataextra"} {
		if err := p.ValidateVolumes([]string{vol}); err == nil {
			t.Errorf("expected error for %q not under /data prefix, got nil", vol)
		}
	}
}

func TestValidateVolumesNamedVolumeExactMatch(t *testing.T) {
	p := &policy.WorkerPolicy{AllowedVolumes: []string{"mydb_data"}}
	if err := p.ValidateVolumes([]string{"mydb_data:/var/lib/postgresql/data"}); err != nil {
		t.Errorf("named volume exact match: expected to pass, got: %v", err)
	}
}

func TestValidateVolumesNamedVolumeNotInList(t *testing.T) {
	p := &policy.WorkerPolicy{AllowedVolumes: []string{"mydb_data"}}
	if err := p.ValidateVolumes([]string{"other_volume:/data"}); err == nil {
		t.Error("expected error for named volume not in list, got nil")
	}
}

// --- PreventLatestImages flag ---

func TestPreventLatestImagesUntaggedImageBlocked(t *testing.T) {
	p := &policy.WorkerPolicy{PreventLatestImages: true}
	for _, img := range []string{"nginx", "ghcr.io/org/app", "myregistry.io:5000/app"} {
		if err := p.ValidateImages([]string{img}); err == nil {
			t.Errorf("expected untagged image %q to be blocked", img)
		}
	}
}

func TestPreventLatestImagesLatestTagBlocked(t *testing.T) {
	p := &policy.WorkerPolicy{PreventLatestImages: true}
	for _, img := range []string{"nginx:latest", "alpine:latest", "ghcr.io/org/app:latest"} {
		if err := p.ValidateImages([]string{img}); err == nil {
			t.Errorf("expected :latest image %q to be blocked", img)
		}
	}
}

func TestPreventLatestImagesLatestWithDigestBlocked(t *testing.T) {
	p := &policy.WorkerPolicy{PreventLatestImages: true}
	img := "nginx:latest@sha256:abc123"
	if err := p.ValidateImages([]string{img}); err == nil {
		t.Errorf("expected :latest@digest image %q to be blocked", img)
	}
}

func TestPreventLatestImagesExplicitTagAllowed(t *testing.T) {
	p := &policy.WorkerPolicy{PreventLatestImages: true}
	for _, img := range []string{"nginx:1.25", "alpine:3.18", "ghcr.io/org/app:v2.0.1"} {
		if err := p.ValidateImages([]string{img}); err != nil {
			t.Errorf("expected explicit tag %q to be allowed, got: %v", img, err)
		}
	}
}

func TestPreventLatestImagesDigestOnlyBlocked(t *testing.T) {
	p := &policy.WorkerPolicy{PreventLatestImages: true}
	img := "nginx@sha256:abc123"
	// Digest-only (no tag) — treated as untagged, should be blocked.
	if err := p.ValidateImages([]string{img}); err == nil {
		t.Errorf("expected digest-only (untagged) image %q to be blocked", img)
	}
}

func TestPreventLatestImagesFlagFalseAllowsLatest(t *testing.T) {
	p := &policy.WorkerPolicy{PreventLatestImages: false}
	if err := p.ValidateImages([]string{"nginx:latest", "alpine"}); err != nil {
		t.Errorf("expected latest/untagged to be allowed when flag is false, got: %v", err)
	}
}

func TestPreventLatestImagesCombinedWithAllowlist(t *testing.T) {
	// prevent_latest AND allowlist: both checks must pass.
	p := &policy.WorkerPolicy{
		PreventLatestImages: true,
		AllowedImages:       []string{"nginx:*"},
	}
	// Good: explicit tag within allowlist.
	if err := p.ValidateImages([]string{"nginx:1.25"}); err != nil {
		t.Errorf("expected nginx:1.25 to pass, got: %v", err)
	}
	// Bad: :latest blocked by flag even if allowlist would match.
	if err := p.ValidateImages([]string{"nginx:latest"}); err == nil {
		t.Error("expected nginx:latest to be blocked by prevent_latest flag")
	}
	// Bad: not in allowlist.
	if err := p.ValidateImages([]string{"alpine:3.18"}); err == nil {
		t.Error("expected alpine:3.18 to be blocked by allowlist")
	}
}

// --- BlockHostVolumes flag ---

func TestBlockHostVolumesBindMountBlocked(t *testing.T) {
	p := &policy.WorkerPolicy{BlockHostVolumes: true}
	for _, vol := range []string{"/data:/data", "/etc/nginx:/etc/nginx:ro", "./local:/app"} {
		if err := p.ValidateVolumes([]string{vol}); err == nil {
			t.Errorf("expected host bind-mount %q to be blocked", vol)
		}
	}
}

func TestBlockHostVolumesNamedVolumeAllowed(t *testing.T) {
	p := &policy.WorkerPolicy{BlockHostVolumes: true}
	for _, vol := range []string{"mydb_data:/var/lib/postgresql/data", "app_cache:/cache"} {
		if err := p.ValidateVolumes([]string{vol}); err != nil {
			t.Errorf("expected named volume %q to be allowed, got: %v", vol, err)
		}
	}
}

func TestBlockHostVolumesEmptyAllowlistStillBlocksBindMount(t *testing.T) {
	// block_host_volumes=true with empty allowlist: named volumes OK, bind-mounts blocked.
	p := &policy.WorkerPolicy{BlockHostVolumes: true, AllowedVolumes: []string{}}
	if err := p.ValidateVolumes([]string{"/data:/data"}); err == nil {
		t.Error("expected bind-mount to be blocked even with empty allowlist")
	}
	if err := p.ValidateVolumes([]string{"mydb_data:/db"}); err != nil {
		t.Errorf("expected named volume to be allowed, got: %v", err)
	}
}

func TestBlockHostVolumesFlagFalseAllowsBindMount(t *testing.T) {
	p := &policy.WorkerPolicy{BlockHostVolumes: false}
	if err := p.ValidateVolumes([]string{"/data:/data"}); err != nil {
		t.Errorf("expected bind-mount to be allowed when flag is false, got: %v", err)
	}
}

// --- Load (policy resolution) ---

func TestWorkerPolicyToJSONNilSlicesBecomEmptyArrays(t *testing.T) {
	p := &policy.WorkerPolicy{}
	j := p.ToJSON()
	if j.AllowedVolumes == nil {
		t.Error("AllowedVolumes should be empty slice, not nil")
	}
	if j.AllowedNetworks == nil {
		t.Error("AllowedNetworks should be empty slice, not nil")
	}
	if j.AllowedImages == nil {
		t.Error("AllowedImages should be empty slice, not nil")
	}
}

func TestValidateDisabledPolicyAllowsEverything(t *testing.T) {
	p := &policy.WorkerPolicy{
		Disabled:            true,
		AllowedImages:       []string{"only-this-image:*"},
		AllowedNetworks:     []string{"only-this-network"},
		AllowedVolumes:      []string{"/only-this-volume"},
		PreventLatestImages: true,
		BlockHostVolumes:    true,
	}

	if err := p.ValidateImages([]string{"nginx:latest", "alpine:3.18"}); err != nil {
		t.Errorf("expected no image error when policy is disabled, got: %v", err)
	}

	if err := p.ValidateNetwork("other-network"); err != nil {
		t.Errorf("expected no network error when policy is disabled, got: %v", err)
	}

	if err := p.ValidateVolumes([]string{"/other-volume:/app", "./local:/app"}); err != nil {
		t.Errorf("expected no volume error when policy is disabled, got: %v", err)
	}
}

// --- ValidatePrivileged ---

func TestValidatePrivilegedBlockedWhenFlagSet(t *testing.T) {
	p := &policy.WorkerPolicy{BlockPrivileged: true}
	if err := p.ValidatePrivileged([]string{"web"}); err == nil {
		t.Error("expected error when a service is privileged and flag is set")
	}
}

func TestValidatePrivilegedAllowedWhenFlagUnset(t *testing.T) {
	p := &policy.WorkerPolicy{BlockPrivileged: false}
	if err := p.ValidatePrivileged([]string{"web"}); err != nil {
		t.Errorf("expected no error when flag is unset, got: %v", err)
	}
}

func TestValidatePrivilegedAllowedWhenNoServices(t *testing.T) {
	p := &policy.WorkerPolicy{BlockPrivileged: true}
	if err := p.ValidatePrivileged(nil); err != nil {
		t.Errorf("expected no error when no services are privileged, got: %v", err)
	}
}

// --- ValidateHostNetwork ---

func TestValidateHostNetworkBlockedWhenFlagSet(t *testing.T) {
	p := &policy.WorkerPolicy{BlockHostNetwork: true}
	if err := p.ValidateHostNetwork([]string{"web"}); err == nil {
		t.Error("expected error for network_mode: host when flag is set")
	}
}

func TestValidateHostNetworkAllowedWhenFlagUnset(t *testing.T) {
	p := &policy.WorkerPolicy{BlockHostNetwork: false}
	if err := p.ValidateHostNetwork([]string{"web"}); err != nil {
		t.Errorf("expected no error when flag is unset, got: %v", err)
	}
}

// --- ValidateHostPID / ValidateHostIPC ---

func TestValidateHostPIDBlockedWhenFlagSet(t *testing.T) {
	p := &policy.WorkerPolicy{BlockHostPID: true}
	if err := p.ValidateHostPID([]string{"web"}); err == nil {
		t.Error("expected error for pid: host when flag is set")
	}
}

func TestValidateHostIPCBlockedWhenFlagSet(t *testing.T) {
	p := &policy.WorkerPolicy{BlockHostIPC: true}
	if err := p.ValidateHostIPC([]string{"web"}); err == nil {
		t.Error("expected error for ipc: host when flag is set")
	}
}

// --- ValidateDockerSocket ---

func TestValidateDockerSocketBlockedForKnownPaths(t *testing.T) {
	p := &policy.WorkerPolicy{BlockDockerSocket: true}
	for _, path := range []string{"/var/run/docker.sock", "/run/docker.sock"} {
		if err := p.ValidateDockerSocket([]string{path}); err == nil {
			t.Errorf("expected docker socket mount %q to be blocked", path)
		}
	}
}

func TestValidateDockerSocketAllowedForOtherPaths(t *testing.T) {
	p := &policy.WorkerPolicy{BlockDockerSocket: true}
	if err := p.ValidateDockerSocket([]string{"/data", "/var/run/other.sock"}); err != nil {
		t.Errorf("expected non-socket mounts to be allowed, got: %v", err)
	}
}

func TestValidateDockerSocketAllowedWhenFlagUnset(t *testing.T) {
	p := &policy.WorkerPolicy{BlockDockerSocket: false}
	if err := p.ValidateDockerSocket([]string{"/var/run/docker.sock"}); err != nil {
		t.Errorf("expected docker socket mount to be allowed when flag unset, got: %v", err)
	}
}

// --- ValidateCapAdd / ValidateDevices / ValidateSecurityOpt ---

func TestValidateCapAddEmptyAllowlistAllowsAll(t *testing.T) {
	p := &policy.WorkerPolicy{AllowedCapAdd: []string{}}
	if err := p.ValidateCapAdd([]string{"NET_ADMIN", "SYS_ADMIN"}); err != nil {
		t.Errorf("expected no error with empty allowlist, got: %v", err)
	}
}

func TestValidateCapAddNotInListBlocked(t *testing.T) {
	p := &policy.WorkerPolicy{AllowedCapAdd: []string{"NET_ADMIN"}}
	if err := p.ValidateCapAdd([]string{"SYS_ADMIN"}); err == nil {
		t.Error("expected error for capability not in allowlist")
	}
}

func TestValidateDevicesEmptyAllowlistAllowsAll(t *testing.T) {
	p := &policy.WorkerPolicy{AllowedDevices: []string{}}
	if err := p.ValidateDevices([]string{"/dev/ttyUSB0"}); err != nil {
		t.Errorf("expected no error with empty allowlist, got: %v", err)
	}
}

func TestValidateDevicesNotInListBlocked(t *testing.T) {
	p := &policy.WorkerPolicy{AllowedDevices: []string{"/dev/ttyUSB0"}}
	if err := p.ValidateDevices([]string{"/dev/sda"}); err == nil {
		t.Error("expected error for device not in allowlist")
	}
}

func TestValidateSecurityOptEmptyAllowlistAllowsAll(t *testing.T) {
	p := &policy.WorkerPolicy{AllowedSecurityOpt: []string{}}
	if err := p.ValidateSecurityOpt([]string{"seccomp:unconfined"}); err != nil {
		t.Errorf("expected no error with empty allowlist, got: %v", err)
	}
}

func TestValidateSecurityOptNotInListBlocked(t *testing.T) {
	p := &policy.WorkerPolicy{AllowedSecurityOpt: []string{"no-new-privileges:true"}}
	if err := p.ValidateSecurityOpt([]string{"seccomp:unconfined"}); err == nil {
		t.Error("expected error for security_opt not in allowlist")
	}
}

func TestValidateHardenedChecksDisabledPolicyAllowsEverything(t *testing.T) {
	p := &policy.WorkerPolicy{
		Disabled:           true,
		BlockPrivileged:    true,
		BlockHostNetwork:   true,
		BlockHostPID:       true,
		BlockHostIPC:       true,
		BlockDockerSocket:  true,
		AllowedCapAdd:      []string{"NET_ADMIN"},
		AllowedDevices:     []string{"/dev/ttyUSB0"},
		AllowedSecurityOpt: []string{"no-new-privileges:true"},
	}
	if err := p.ValidatePrivileged([]string{"web"}); err != nil {
		t.Errorf("expected no error when policy is disabled, got: %v", err)
	}
	if err := p.ValidateHostNetwork([]string{"web"}); err != nil {
		t.Errorf("expected no error when policy is disabled, got: %v", err)
	}
	if err := p.ValidateHostPID([]string{"web"}); err != nil {
		t.Errorf("expected no error when policy is disabled, got: %v", err)
	}
	if err := p.ValidateHostIPC([]string{"web"}); err != nil {
		t.Errorf("expected no error when policy is disabled, got: %v", err)
	}
	if err := p.ValidateDockerSocket([]string{"/var/run/docker.sock"}); err != nil {
		t.Errorf("expected no error when policy is disabled, got: %v", err)
	}
	if err := p.ValidateCapAdd([]string{"SYS_ADMIN"}); err != nil {
		t.Errorf("expected no error when policy is disabled, got: %v", err)
	}
	if err := p.ValidateDevices([]string{"/dev/sda"}); err != nil {
		t.Errorf("expected no error when policy is disabled, got: %v", err)
	}
	if err := p.ValidateSecurityOpt([]string{"seccomp:unconfined"}); err != nil {
		t.Errorf("expected no error when policy is disabled, got: %v", err)
	}
}
