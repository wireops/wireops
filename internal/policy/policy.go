// Package policy resolves and enforces worker-level security policies for
// volumes, Docker networks, and container images. Policies can be set globally
// (via the worker_policies singleton collection) or overridden per worker.
//
// # Allowlist semantics (AllowedVolumes, AllowedNetworks, AllowedImages)
//   - An empty list means "everything is permitted" (open policy).
//   - As soon as at least one entry is present, only what is listed is allowed.
//   - Per-worker overrides replace (not merge with) the global list for that resource.
//   - When policy_inherit == true (the default) and a worker has no local override for
//     a resource type, the global value for that type is used instead.
//
// # Boolean flag semantics (PreventLatestImages, BlockHostVolumes)
//   - Global flags are stored in the worker_policies singleton.
//   - Per-worker overrides are stored as a nullable JSON map (policy_flags field).
//     A null value for a flag means "inherit from global"; true/false overrides explicitly.
//   - When policy_inherit == true and the worker has no local flag, the global flag is used.
package policy

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// WorkerPolicy is the resolved effective policy for a specific worker.
type WorkerPolicy struct {
	Disabled bool // when true, the worker policy security system is disabled globally
	// Allowlists — empty = open policy (all permitted).
	AllowedVolumes     []string // host-path prefixes or exact named-volume names
	AllowedNetworks    []string // exact Docker network names
	AllowedImages      []string // image patterns (glob wildcards via filepath.Match)
	AllowedCapAdd      []string // exact Linux capability names (e.g. "NET_ADMIN")
	AllowedDevices     []string // host device paths (e.g. "/dev/ttyUSB0")
	AllowedSecurityOpt []string // exact security_opt entries (e.g. "no-new-privileges:true")

	// Boolean flags — enforce specific restrictions independently of allowlists.
	PreventLatestImages bool // when true, images without a tag or with ":latest" are rejected
	BlockHostVolumes    bool // when true, bind-mounts (host paths) are rejected
	BlockPrivileged     bool // when true, services with privileged: true are rejected
	BlockHostNetwork    bool // when true, services with network_mode: host are rejected
	BlockHostPID        bool // when true, services with pid: host are rejected
	BlockHostIPC        bool // when true, services with ipc: host are rejected
	BlockDockerSocket   bool // when true, mounting /var/run/docker.sock (or /run/docker.sock) is rejected
}

// policyFlags is the nullable wire format for per-worker boolean flag overrides.
// A nil pointer means "inherit from global"; a non-nil value overrides explicitly.
type policyFlags struct {
	PreventLatestImages *bool `json:"prevent_latest_images"`
	BlockHostVolumes    *bool `json:"block_host_volumes"`
	BlockPrivileged     *bool `json:"block_privileged"`
	BlockHostNetwork    *bool `json:"block_host_network"`
	BlockHostPID        *bool `json:"block_host_pid"`
	BlockHostIPC        *bool `json:"block_host_ipc"`
	BlockDockerSocket   *bool `json:"block_docker_socket"`
}

// Load returns the effective WorkerPolicy for the given workerID.
//
// Resolution order:
//  1. Load global policy from the worker_policies singleton.
//  2. Load per-worker overrides from the workers record.
//  3. For each dimension:
//     - If policy_inherit == true (default) and local value is empty/nil → use global.
//     - Otherwise → use local value.
func Load(app core.App, workerID string) (*WorkerPolicy, error) {
	global, err := loadGlobal(app)
	if err != nil {
		return nil, fmt.Errorf("policy: load global: %w", err)
	}

	if workerID == "" {
		return global, nil
	}

	worker, err := app.FindRecordById("workers", workerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Worker not found — return global policy so callers still get a valid object.
			return global, nil
		}
		return nil, fmt.Errorf("policy: find worker: %w", err)
	}

	inherit := true
	// policy_inherit defaults to true. PocketBase returns false (zero) for unset bool fields,
	// so we only override the default when the field has been explicitly saved.
	inheritRaw := worker.Get("policy_inherit")
	if inheritRaw != nil {
		inherit = worker.GetBool("policy_inherit")
	}

	local := &WorkerPolicy{
		Disabled: global.Disabled,
	}

	// --- Allowlists ---
	var localVolumes, localNetworks, localImages, localCapAdd, localDevices, localSecurityOpt *[]string
	_ = worker.UnmarshalJSONField("policy_volumes", &localVolumes)
	_ = worker.UnmarshalJSONField("policy_networks", &localNetworks)
	_ = worker.UnmarshalJSONField("policy_images", &localImages)
	_ = worker.UnmarshalJSONField("policy_cap_add", &localCapAdd)
	_ = worker.UnmarshalJSONField("policy_devices", &localDevices)
	_ = worker.UnmarshalJSONField("policy_security_opt", &localSecurityOpt)

	if localVolumes != nil {
		local.AllowedVolumes = *localVolumes
	} else if inherit {
		local.AllowedVolumes = global.AllowedVolumes
	} else {
		local.AllowedVolumes = []string{}
	}

	if localNetworks != nil {
		local.AllowedNetworks = *localNetworks
	} else if inherit {
		local.AllowedNetworks = global.AllowedNetworks
	} else {
		local.AllowedNetworks = []string{}
	}

	if localImages != nil {
		local.AllowedImages = *localImages
	} else if inherit {
		local.AllowedImages = global.AllowedImages
	} else {
		local.AllowedImages = []string{}
	}

	if localCapAdd != nil {
		local.AllowedCapAdd = *localCapAdd
	} else if inherit {
		local.AllowedCapAdd = global.AllowedCapAdd
	} else {
		local.AllowedCapAdd = []string{}
	}

	if localDevices != nil {
		local.AllowedDevices = *localDevices
	} else if inherit {
		local.AllowedDevices = global.AllowedDevices
	} else {
		local.AllowedDevices = []string{}
	}

	if localSecurityOpt != nil {
		local.AllowedSecurityOpt = *localSecurityOpt
	} else if inherit {
		local.AllowedSecurityOpt = global.AllowedSecurityOpt
	} else {
		local.AllowedSecurityOpt = []string{}
	}

	// --- Boolean flags ---
	var flags policyFlags
	if raw := worker.GetString("policy_flags"); raw != "" {
		_ = json.Unmarshal([]byte(raw), &flags)
	}

	local.PreventLatestImages = resolveFlag(flags.PreventLatestImages, global.PreventLatestImages, inherit)
	local.BlockHostVolumes = resolveFlag(flags.BlockHostVolumes, global.BlockHostVolumes, inherit)
	local.BlockPrivileged = resolveFlag(flags.BlockPrivileged, global.BlockPrivileged, inherit)
	local.BlockHostNetwork = resolveFlag(flags.BlockHostNetwork, global.BlockHostNetwork, inherit)
	local.BlockHostPID = resolveFlag(flags.BlockHostPID, global.BlockHostPID, inherit)
	local.BlockHostIPC = resolveFlag(flags.BlockHostIPC, global.BlockHostIPC, inherit)
	local.BlockDockerSocket = resolveFlag(flags.BlockDockerSocket, global.BlockDockerSocket, inherit)

	return local, nil
}

// resolveFlag resolves the effective boolean value for a policy flag.
//   - If the local override is non-nil: use it (regardless of inherit).
//   - If the local override is nil and inherit == true: use the global value.
//   - If the local override is nil and inherit == false: use false (no restriction).
func resolveFlag(local *bool, global, inherit bool) bool {
	if local != nil {
		return *local
	}
	if inherit {
		return global
	}
	return false
}

// LoadGlobal is the exported version of loadGlobal for use in route handlers.
func LoadGlobal(app core.App) (*WorkerPolicy, error) {
	return loadGlobal(app)
}

func loadGlobal(app core.App) (*WorkerPolicy, error) {
	records, err := app.FindAllRecords("worker_policies")
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("policy: find global policies: %w", err)
	}
	if len(records) == 0 {
		// No global policy configured — open policy for everything.
		return &WorkerPolicy{Disabled: false}, nil
	}
	rec := records[0]
	p := &WorkerPolicy{}

	enabled := true
	if rec.Get("enabled") != nil {
		enabled = rec.GetBool("enabled")
	}
	p.Disabled = !enabled

	_ = rec.UnmarshalJSONField("allowed_volumes", &p.AllowedVolumes)
	_ = rec.UnmarshalJSONField("allowed_networks", &p.AllowedNetworks)
	_ = rec.UnmarshalJSONField("allowed_images", &p.AllowedImages)
	_ = rec.UnmarshalJSONField("allowed_cap_add", &p.AllowedCapAdd)
	_ = rec.UnmarshalJSONField("allowed_devices", &p.AllowedDevices)
	_ = rec.UnmarshalJSONField("allowed_security_opt", &p.AllowedSecurityOpt)
	p.PreventLatestImages = rec.GetBool("prevent_latest_images")
	p.BlockHostVolumes = rec.GetBool("block_host_volumes")
	p.BlockPrivileged = rec.GetBool("block_privileged")
	p.BlockHostNetwork = rec.GetBool("block_host_network")
	p.BlockHostPID = rec.GetBool("block_host_pid")
	p.BlockHostIPC = rec.GetBool("block_host_ipc")
	p.BlockDockerSocket = rec.GetBool("block_docker_socket")
	return p, nil
}

// ValidateVolumes checks that every volume spec in the list is permitted by the policy.
//
// Volume spec format: "hostPath:containerPath[:options]" or "namedVolume:containerPath".
//
// Rules:
//   - If BlockHostVolumes == true: any bind-mount (absolute path, ./, ../, ~) is rejected.
//   - Empty AllowedVolumes → all volumes permitted (subject to BlockHostVolumes).
//   - Bind-mounts: the host path must start with an allowed prefix.
//   - Named volumes: the volume name must exactly match an allowed entry.
func (p *WorkerPolicy) ValidateVolumes(volumes []string) error {
	if p.Disabled {
		return nil
	}
	for _, vol := range volumes {
		if err := p.validateSingleVolume(vol); err != nil {
			return err
		}
	}
	return nil
}

func (p *WorkerPolicy) validateSingleVolume(vol string) error {
	parts := strings.SplitN(vol, ":", 3)
	hostPart := parts[0]

	isBindMount := strings.HasPrefix(hostPart, "/") || strings.HasPrefix(hostPart, "./") ||
		strings.HasPrefix(hostPart, "../") || strings.HasPrefix(hostPart, "~")

	// Boolean flag check: block all host bind-mounts.
	if p.BlockHostVolumes && isBindMount {
		return fmt.Errorf("volume mount path %q is a host bind-mount and the worker policy blocks host volumes", hostPart)
	}

	// Allowlist check (only when non-empty).
	if len(p.AllowedVolumes) == 0 {
		return nil // open policy
	}

	for _, allowed := range p.AllowedVolumes {
		if isBindMount {
			allowedClean := filepath.Clean(allowed)
			hostClean := filepath.Clean(hostPart)
			if hostClean == allowedClean || strings.HasPrefix(hostClean, allowedClean+"/") {
				return nil
			}
		} else {
			if hostPart == allowed {
				return nil
			}
		}
	}

	return fmt.Errorf("volume mount path %q is not in the worker's allowed volume list", hostPart)
}

// ValidateNetwork checks that the network name is permitted by the policy.
// An empty network string is always allowed (means "use default").
func (p *WorkerPolicy) ValidateNetwork(network string) error {
	if p.Disabled {
		return nil
	}
	if network == "" {
		return nil
	}
	if len(p.AllowedNetworks) == 0 {
		return nil // open policy
	}
	for _, allowed := range p.AllowedNetworks {
		if network == allowed {
			return nil
		}
	}
	return fmt.Errorf("network %q is not in the worker's allowed network list", network)
}

// ValidateImages checks that every image reference is permitted by the policy.
//
// Rules applied in order:
//  1. If PreventLatestImages == true: images with no tag, with ":latest", or with ":latest@..." are rejected.
//  2. AllowedImages allowlist (if non-empty): only listed patterns are permitted.
//
// Patterns support glob wildcards via filepath.Match (e.g., "nginx:*", "ghcr.io/org/*").
func (p *WorkerPolicy) ValidateImages(images []string) error {
	if p.Disabled {
		return nil
	}
	for _, img := range images {
		if p.PreventLatestImages {
			if isLatestOrUntagged(img) {
				return fmt.Errorf("image %q uses :latest or has no tag, which is blocked by the worker policy", img)
			}
		}
		if len(p.AllowedImages) > 0 && !p.imageAllowed(img) {
			return fmt.Errorf("image %q is not in the worker's allowed image list", img)
		}
	}
	return nil
}

// isLatestOrUntagged returns true if the image has no explicit tag or uses ":latest".
// It also handles digest references like "nginx:latest@sha256:...".
func isLatestOrUntagged(image string) bool {
	// Strip digest suffix.
	imagePart := image
	if idx := strings.Index(image, "@"); idx != -1 {
		imagePart = image[:idx]
	}

	// Find the last colon after any registry host (which may itself contain a colon for port).
	// A tag separator colon appears after the last "/" (or at end if no slash).
	// We look for the rightmost colon that is after the last slash.
	lastSlash := strings.LastIndex(imagePart, "/")
	suffix := imagePart[lastSlash+1:]
	colonInSuffix := strings.LastIndex(suffix, ":")

	if colonInSuffix == -1 {
		// No tag at all (e.g., "nginx", "ghcr.io/org/app").
		return true
	}

	tag := suffix[colonInSuffix+1:]
	return tag == "" || tag == "latest"
}

func (p *WorkerPolicy) imageAllowed(image string) bool {
	for _, pattern := range p.AllowedImages {
		if matchPattern(pattern, image) {
			return true
		}
	}
	return false
}

// matchPattern returns true if image matches pattern.
// Supports glob wildcards: "*" matches any sequence of characters (including "/").
// filepath.Match is used; "/" is treated as a separator, so "ghcr.io/org/*" requires
// an exact org prefix before the wildcard.
func matchPattern(pattern, image string) bool {
	if pattern == image {
		return true
	}
	matched, err := filepath.Match(pattern, image)
	if err != nil {
		return false
	}
	return matched
}

// ValidatePrivileged rejects the policy if any of the given service names run privileged.
func (p *WorkerPolicy) ValidatePrivileged(services []string) error {
	if p.Disabled || !p.BlockPrivileged || len(services) == 0 {
		return nil
	}
	return fmt.Errorf("service %q runs with privileged: true, which is blocked by the worker policy", services[0])
}

// ValidateHostNetwork rejects the policy if any of the given service names use network_mode: host.
func (p *WorkerPolicy) ValidateHostNetwork(services []string) error {
	if p.Disabled || !p.BlockHostNetwork || len(services) == 0 {
		return nil
	}
	return fmt.Errorf("service %q uses network_mode: host, which is blocked by the worker policy", services[0])
}

// ValidateHostPID rejects the policy if any of the given service names use pid: host.
func (p *WorkerPolicy) ValidateHostPID(services []string) error {
	if p.Disabled || !p.BlockHostPID || len(services) == 0 {
		return nil
	}
	return fmt.Errorf("service %q uses pid: host, which is blocked by the worker policy", services[0])
}

// ValidateHostIPC rejects the policy if any of the given service names use ipc: host.
func (p *WorkerPolicy) ValidateHostIPC(services []string) error {
	if p.Disabled || !p.BlockHostIPC || len(services) == 0 {
		return nil
	}
	return fmt.Errorf("service %q uses ipc: host, which is blocked by the worker policy", services[0])
}

// dockerSocketPaths are the well-known host paths for the Docker daemon's Unix socket.
var dockerSocketPaths = map[string]bool{
	"/var/run/docker.sock": true,
	"/run/docker.sock":     true,
}

// ValidateDockerSocket rejects the policy if any mount source (from volumes or devices)
// targets the Docker daemon socket.
func (p *WorkerPolicy) ValidateDockerSocket(mounts []string) error {
	if p.Disabled || !p.BlockDockerSocket {
		return nil
	}
	for _, m := range mounts {
		src := filepath.Clean(strings.SplitN(m, ":", 2)[0])
		prefix := src
		if prefix != string(filepath.Separator) {
			prefix += string(filepath.Separator)
		}
		for sockPath := range dockerSocketPaths {
			if src == sockPath || strings.HasPrefix(sockPath, prefix) {
				return fmt.Errorf("mount %q exposes the Docker socket, which is blocked by the worker policy", m)
			}
		}
	}
	return nil
}

// ValidateCapAdd checks that every requested Linux capability is permitted by the policy.
// Empty AllowedCapAdd means open policy (all permitted).
func (p *WorkerPolicy) ValidateCapAdd(caps []string) error {
	if p.Disabled || len(p.AllowedCapAdd) == 0 {
		return nil
	}
	for _, c := range caps {
		if !stringInList(c, p.AllowedCapAdd) {
			return fmt.Errorf("capability %q is not in the worker's allowed cap_add list", c)
		}
	}
	return nil
}

// ValidateDevices checks that every requested host device is permitted by the policy.
// Empty AllowedDevices means open policy (all permitted).
func (p *WorkerPolicy) ValidateDevices(devices []string) error {
	if p.Disabled || len(p.AllowedDevices) == 0 {
		return nil
	}
	for _, d := range devices {
		if !stringInList(d, p.AllowedDevices) {
			return fmt.Errorf("device %q is not in the worker's allowed device list", d)
		}
	}
	return nil
}

// ValidateSecurityOpt checks that every requested security_opt entry is permitted by the policy.
// Empty AllowedSecurityOpt means open policy (all permitted).
func (p *WorkerPolicy) ValidateSecurityOpt(opts []string) error {
	if p.Disabled || len(p.AllowedSecurityOpt) == 0 {
		return nil
	}
	for _, o := range opts {
		if !stringInList(o, p.AllowedSecurityOpt) {
			return fmt.Errorf("security_opt %q is not in the worker's allowed security_opt list", o)
		}
	}
	return nil
}

func stringInList(s string, list []string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

// PolicyJSON is the wire format used by the API for reading and writing global policies.
type PolicyJSON struct {
	Enabled             bool     `json:"enabled"`
	AllowedVolumes      []string `json:"allowed_volumes"`
	AllowedNetworks     []string `json:"allowed_networks"`
	AllowedImages       []string `json:"allowed_images"`
	AllowedCapAdd       []string `json:"allowed_cap_add"`
	AllowedDevices      []string `json:"allowed_devices"`
	AllowedSecurityOpt  []string `json:"allowed_security_opt"`
	PreventLatestImages bool     `json:"prevent_latest_images"`
	BlockHostVolumes    bool     `json:"block_host_volumes"`
	BlockPrivileged     bool     `json:"block_privileged"`
	BlockHostNetwork    bool     `json:"block_host_network"`
	BlockHostPID        bool     `json:"block_host_pid"`
	BlockHostIPC        bool     `json:"block_host_ipc"`
	BlockDockerSocket   bool     `json:"block_docker_socket"`
}

// ToJSON converts a WorkerPolicy to the API wire format.
func (p *WorkerPolicy) ToJSON() PolicyJSON {
	res := PolicyJSON{
		Enabled:             !p.Disabled,
		AllowedVolumes:      p.AllowedVolumes,
		AllowedNetworks:     p.AllowedNetworks,
		AllowedImages:       p.AllowedImages,
		AllowedCapAdd:       p.AllowedCapAdd,
		AllowedDevices:      p.AllowedDevices,
		AllowedSecurityOpt:  p.AllowedSecurityOpt,
		PreventLatestImages: p.PreventLatestImages,
		BlockHostVolumes:    p.BlockHostVolumes,
		BlockPrivileged:     p.BlockPrivileged,
		BlockHostNetwork:    p.BlockHostNetwork,
		BlockHostPID:        p.BlockHostPID,
		BlockHostIPC:        p.BlockHostIPC,
		BlockDockerSocket:   p.BlockDockerSocket,
	}
	if res.AllowedVolumes == nil {
		res.AllowedVolumes = []string{}
	}
	if res.AllowedNetworks == nil {
		res.AllowedNetworks = []string{}
	}
	if res.AllowedImages == nil {
		res.AllowedImages = []string{}
	}
	if res.AllowedCapAdd == nil {
		res.AllowedCapAdd = []string{}
	}
	if res.AllowedDevices == nil {
		res.AllowedDevices = []string{}
	}
	if res.AllowedSecurityOpt == nil {
		res.AllowedSecurityOpt = []string{}
	}
	return res
}

// WorkerPolicyOverrideJSON is the full wire format for per-worker policy,
// including the inherit flag and nullable boolean flag overrides.
type WorkerPolicyOverrideJSON struct {
	Inherit            bool      `json:"inherit"`
	AllowedVolumes     *[]string `json:"allowed_volumes"`
	AllowedNetworks    *[]string `json:"allowed_networks"`
	AllowedImages      *[]string `json:"allowed_images"`
	AllowedCapAdd      *[]string `json:"allowed_cap_add"`
	AllowedDevices     *[]string `json:"allowed_devices"`
	AllowedSecurityOpt *[]string `json:"allowed_security_opt"`
	// Nullable booleans: use a pointer so null (unset/inherit) is distinguishable from false.
	PreventLatestImages *bool `json:"prevent_latest_images"`
	BlockHostVolumes    *bool `json:"block_host_volumes"`
	BlockPrivileged     *bool `json:"block_privileged"`
	BlockHostNetwork    *bool `json:"block_host_network"`
	BlockHostPID        *bool `json:"block_host_pid"`
	BlockHostIPC        *bool `json:"block_host_ipc"`
	BlockDockerSocket   *bool `json:"block_docker_socket"`
}
