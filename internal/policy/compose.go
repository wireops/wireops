package policy

import (
	"fmt"
	"sort"
	"strings"
)

// isHostPath reports whether a volume source string looks like a host filesystem
// path (bind mount) rather than a named volume reference.
func isHostPath(src string) bool {
	return strings.HasPrefix(src, "/") || strings.HasPrefix(src, "./") || strings.HasPrefix(src, "../") || strings.HasPrefix(src, "~")
}

// namedVolumeDevices extracts host device paths for top-level named volumes
// that are backed by a local bind mount (driver_opts.device), so that
// service references to those named volumes can be resolved back to the
// underlying host path for policy checks such as ValidateDockerSocket.
func namedVolumeDevices(configMap map[string]interface{}) map[string]string {
	result := map[string]string{}
	topVolumes, ok := configMap["volumes"].(map[string]interface{})
	if !ok {
		return result
	}
	for name, defRaw := range topVolumes {
		def, ok := defRaw.(map[string]interface{})
		if !ok {
			continue
		}
		opts, ok := def["driver_opts"].(map[string]interface{})
		if !ok {
			continue
		}
		if device, ok := opts["device"].(string); ok && device != "" {
			result[name] = device
		}
	}
	return result
}

// ownedRef pairs a value extracted from the compose config with the service it
// came from, so violations can name the offending service instead of only the
// offending value.
type ownedRef struct {
	Service string
	Value   string
}

// composeRefs is everything the policy checks care about, pulled out of a
// decoded compose config in a single pass. ValidateComposeConfig (fail on the
// first violation) and ComposeViolations (collect them all) both run off this,
// so the two can never drift in what they look at.
type composeRefs struct {
	images              []ownedRef
	volumes             []ownedRef
	networks            []ownedRef
	privilegedServices  []string
	hostNetworkServices []string
	hostPIDServices     []string
	hostIPCServices     []string
	capAdds             []ownedRef
	securityOpts        []ownedRef
	devices             []ownedRef
	// mounts is volumes + devices + host paths behind referenced named
	// volumes, i.e. every path that could reach the Docker socket.
	mounts []ownedRef
}

func values(refs []ownedRef) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		out = append(out, r.Value)
	}
	return out
}

// extractComposeRefs walks a decoded docker-compose config and collects every
// value the worker policy validates. Services are visited in sorted order so
// that callers reporting more than one violation get a stable ordering rather
// than Go's randomized map iteration.
func extractComposeRefs(configMap map[string]interface{}) composeRefs {
	var refs composeRefs

	svcs, ok := configMap["services"].(map[string]interface{})
	if !ok {
		return refs
	}

	svcNames := make([]string, 0, len(svcs))
	for name := range svcs {
		svcNames = append(svcNames, name)
	}
	sort.Strings(svcNames)

	var namedVolumeRefs []ownedRef

	for _, svcName := range svcNames {
		svc, ok := svcs[svcName].(map[string]interface{})
		if !ok {
			continue
		}

		if img, ok := svc["image"].(string); ok && img != "" {
			refs.images = append(refs.images, ownedRef{Service: svcName, Value: img})
		}

		if privileged, ok := svc["privileged"].(bool); ok && privileged {
			refs.privilegedServices = append(refs.privilegedServices, svcName)
		}
		if mode, ok := svc["network_mode"].(string); ok && mode == "host" {
			refs.hostNetworkServices = append(refs.hostNetworkServices, svcName)
		}
		if pid, ok := svc["pid"].(string); ok && pid == "host" {
			refs.hostPIDServices = append(refs.hostPIDServices, svcName)
		}
		if ipc, ok := svc["ipc"].(string); ok && ipc == "host" {
			refs.hostIPCServices = append(refs.hostIPCServices, svcName)
		}
		if caps, ok := svc["cap_add"].([]interface{}); ok {
			for _, c := range caps {
				if capStr, ok := c.(string); ok && capStr != "" {
					refs.capAdds = append(refs.capAdds, ownedRef{Service: svcName, Value: capStr})
				}
			}
		}
		if opts, ok := svc["security_opt"].([]interface{}); ok {
			for _, o := range opts {
				if optStr, ok := o.(string); ok && optStr != "" {
					refs.securityOpts = append(refs.securityOpts, ownedRef{Service: svcName, Value: optStr})
				}
			}
		}
		if devs, ok := svc["devices"].([]interface{}); ok {
			for _, d := range devs {
				devStr, ok := d.(string)
				if !ok || devStr == "" {
					continue
				}
				src := strings.SplitN(devStr, ":", 2)[0]
				src = strings.TrimSpace(src)
				if src != "" {
					refs.devices = append(refs.devices, ownedRef{Service: svcName, Value: src})
				}
			}
		}

		if vols, ok := svc["volumes"].([]interface{}); ok {
			for _, volRaw := range vols {
				if volStr, ok := volRaw.(string); ok {
					volStr = strings.TrimSpace(volStr)
					volStr = strings.TrimPrefix(volStr, "-")
					volStr = strings.TrimSpace(volStr)
					volStr = strings.Trim(volStr, `"'`)

					parts := strings.Split(volStr, ":")
					src := ""
					if len(parts) > 1 {
						src = strings.TrimSpace(parts[0])
					} else if len(parts) == 1 {
						p := strings.TrimSpace(parts[0])
						if isHostPath(p) {
							src = p
						}
					}
					if src != "" {
						refs.volumes = append(refs.volumes, ownedRef{Service: svcName, Value: src})
						if !isHostPath(src) {
							namedVolumeRefs = append(namedVolumeRefs, ownedRef{Service: svcName, Value: src})
						}
					}
					continue
				}

				vol, ok := volRaw.(map[string]interface{})
				if !ok {
					continue
				}

				if src, ok := vol["source"].(string); ok && src != "" {
					refs.volumes = append(refs.volumes, ownedRef{Service: svcName, Value: src})
					if !isHostPath(src) {
						namedVolumeRefs = append(namedVolumeRefs, ownedRef{Service: svcName, Value: src})
					}
				}
			}
		}

		if nets, ok := svc["networks"].(map[string]interface{}); ok {
			netNames := make([]string, 0, len(nets))
			for netName := range nets {
				netNames = append(netNames, netName)
			}
			sort.Strings(netNames)
			for _, netName := range netNames {
				refs.networks = append(refs.networks, ownedRef{Service: svcName, Value: netName})
			}
		} else if netsList, ok := svc["networks"].([]interface{}); ok {
			for _, netRaw := range netsList {
				if netName, ok := netRaw.(string); ok && netName != "" {
					refs.networks = append(refs.networks, ownedRef{Service: svcName, Value: netName})
				}
			}
		}
	}

	var resolvedVolumeDevices []ownedRef
	if len(namedVolumeRefs) > 0 {
		devicesByName := namedVolumeDevices(configMap)
		for _, ref := range namedVolumeRefs {
			if device, ok := devicesByName[ref.Value]; ok && device != "" {
				resolvedVolumeDevices = append(resolvedVolumeDevices, ownedRef{Service: ref.Service, Value: device})
			}
		}
	}

	refs.mounts = make([]ownedRef, 0, len(refs.volumes)+len(refs.devices)+len(resolvedVolumeDevices))
	refs.mounts = append(refs.mounts, refs.volumes...)
	refs.mounts = append(refs.mounts, refs.devices...)
	refs.mounts = append(refs.mounts, resolvedVolumeDevices...)

	return refs
}

// ValidateComposeConfig validates a docker-compose config map against the worker policy.
// It extracts images, volumes, and networks from the config and runs them through
// the respective validation methods, returning the first violation found.
//
// Use ComposeViolations instead when every violation is wanted (e.g. lint
// reporting) rather than just the one that blocks the deploy.
func (p *WorkerPolicy) ValidateComposeConfig(configMap map[string]interface{}) error {
	if p.Disabled {
		return nil
	}

	refs := extractComposeRefs(configMap)

	if err := p.ValidateImages(values(refs.images)); err != nil {
		return fmt.Errorf("image policy violation: %w", err)
	}
	if err := p.ValidateVolumes(values(refs.volumes)); err != nil {
		return fmt.Errorf("volume policy violation: %w", err)
	}
	for _, net := range refs.networks {
		if err := p.ValidateNetwork(net.Value); err != nil {
			return fmt.Errorf("network policy violation: %w", err)
		}
	}

	if err := p.ValidatePrivileged(refs.privilegedServices); err != nil {
		return fmt.Errorf("privileged mode policy violation: %w", err)
	}
	if err := p.ValidateHostNetwork(refs.hostNetworkServices); err != nil {
		return fmt.Errorf("host network policy violation: %w", err)
	}
	if err := p.ValidateHostPID(refs.hostPIDServices); err != nil {
		return fmt.Errorf("host pid policy violation: %w", err)
	}
	if err := p.ValidateHostIPC(refs.hostIPCServices); err != nil {
		return fmt.Errorf("host ipc policy violation: %w", err)
	}
	if err := p.ValidateDockerSocket(values(refs.mounts)); err != nil {
		return fmt.Errorf("docker socket policy violation: %w", err)
	}
	if err := p.ValidateCapAdd(values(refs.capAdds)); err != nil {
		return fmt.Errorf("cap_add policy violation: %w", err)
	}
	if err := p.ValidateDevices(values(refs.devices)); err != nil {
		return fmt.Errorf("device policy violation: %w", err)
	}
	if err := p.ValidateSecurityOpt(values(refs.securityOpts)); err != nil {
		return fmt.Errorf("security_opt policy violation: %w", err)
	}

	return nil
}

// Violation is a single policy breach found in a compose config, carrying
// enough structure for a caller to render it per-service instead of as one
// opaque error string.
type Violation struct {
	// Check names the policy dimension that rejected the value — "images",
	// "volumes", "networks", "privileged", "host_network", "host_pid",
	// "host_ipc", "docker_socket", "cap_add", "devices" or "security_opt".
	Check string `json:"check"`
	// Service is the compose service the offending value came from. Empty
	// only when the value could not be attributed to a single service.
	Service string `json:"service,omitempty"`
	// Subject is the offending value itself (image ref, mount path, network
	// name, capability, …). For whole-service checks such as "privileged" it
	// is the service name.
	Subject string `json:"subject,omitempty"`
	// Message is the same human-readable text ValidateComposeConfig would
	// have returned for this violation.
	Message string `json:"message"`
}

// ComposeViolations returns every worker-policy violation in a compose config,
// where ValidateComposeConfig stops at the first. Checks run in the same order
// and produce the same messages, so a caller reporting all of them lists the
// blocking violation first among values of the same kind.
//
// Each value is validated on its own (a one-element slice through the same
// Validate* method ValidateComposeConfig uses) so per-item attribution comes
// for free and the two paths cannot report different text for the same breach.
func (p *WorkerPolicy) ComposeViolations(configMap map[string]interface{}) []Violation {
	if p.Disabled {
		return nil
	}

	refs := extractComposeRefs(configMap)
	var out []Violation

	add := func(check string, ref ownedRef, err error, prefix string) {
		if err == nil {
			return
		}
		out = append(out, Violation{
			Check:   check,
			Service: ref.Service,
			Subject: ref.Value,
			Message: fmt.Sprintf("%s: %v", prefix, err),
		})
	}

	for _, img := range refs.images {
		add("images", img, p.ValidateImages([]string{img.Value}), "image policy violation")
	}
	for _, vol := range refs.volumes {
		add("volumes", vol, p.ValidateVolumes([]string{vol.Value}), "volume policy violation")
	}
	for _, net := range refs.networks {
		add("networks", net, p.ValidateNetwork(net.Value), "network policy violation")
	}
	for _, svc := range refs.privilegedServices {
		add("privileged", ownedRef{Service: svc, Value: svc}, p.ValidatePrivileged([]string{svc}), "privileged mode policy violation")
	}
	for _, svc := range refs.hostNetworkServices {
		add("host_network", ownedRef{Service: svc, Value: svc}, p.ValidateHostNetwork([]string{svc}), "host network policy violation")
	}
	for _, svc := range refs.hostPIDServices {
		add("host_pid", ownedRef{Service: svc, Value: svc}, p.ValidateHostPID([]string{svc}), "host pid policy violation")
	}
	for _, svc := range refs.hostIPCServices {
		add("host_ipc", ownedRef{Service: svc, Value: svc}, p.ValidateHostIPC([]string{svc}), "host ipc policy violation")
	}
	for _, mount := range refs.mounts {
		add("docker_socket", mount, p.ValidateDockerSocket([]string{mount.Value}), "docker socket policy violation")
	}
	for _, c := range refs.capAdds {
		add("cap_add", c, p.ValidateCapAdd([]string{c.Value}), "cap_add policy violation")
	}
	for _, d := range refs.devices {
		add("devices", d, p.ValidateDevices([]string{d.Value}), "device policy violation")
	}
	for _, o := range refs.securityOpts {
		add("security_opt", o, p.ValidateSecurityOpt([]string{o.Value}), "security_opt policy violation")
	}

	return out
}
