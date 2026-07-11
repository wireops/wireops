package policy

import (
	"fmt"
	"strings"
)

// ValidateComposeConfig validates a docker-compose config map against the worker policy.
// It extracts images, volumes, and networks from the config and runs them through
// the respective validation methods.
func (p *WorkerPolicy) ValidateComposeConfig(configMap map[string]interface{}) error {
	if p.Disabled {
		return nil
	}

	var images []string
	var volumes []string
	var networks []string
	var privilegedServices []string
	var hostNetworkServices []string
	var hostPIDServices []string
	var hostIPCServices []string
	var capAdds []string
	var securityOpts []string
	var devices []string

	if svcs, ok := configMap["services"].(map[string]interface{}); ok {
		for svcName, svcRaw := range svcs {
			svc, ok := svcRaw.(map[string]interface{})
			if !ok {
				continue
			}

			if img, ok := svc["image"].(string); ok && img != "" {
				images = append(images, img)
			}

			if privileged, ok := svc["privileged"].(bool); ok && privileged {
				privilegedServices = append(privilegedServices, svcName)
			}
			if mode, ok := svc["network_mode"].(string); ok && mode == "host" {
				hostNetworkServices = append(hostNetworkServices, svcName)
			}
			if pid, ok := svc["pid"].(string); ok && pid == "host" {
				hostPIDServices = append(hostPIDServices, svcName)
			}
			if ipc, ok := svc["ipc"].(string); ok && ipc == "host" {
				hostIPCServices = append(hostIPCServices, svcName)
			}
			if caps, ok := svc["cap_add"].([]interface{}); ok {
				for _, c := range caps {
					if capStr, ok := c.(string); ok && capStr != "" {
						capAdds = append(capAdds, capStr)
					}
				}
			}
			if opts, ok := svc["security_opt"].([]interface{}); ok {
				for _, o := range opts {
					if optStr, ok := o.(string); ok && optStr != "" {
						securityOpts = append(securityOpts, optStr)
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
						devices = append(devices, src)
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
							if strings.HasPrefix(p, "/") || strings.HasPrefix(p, "./") || strings.HasPrefix(p, "../") || strings.HasPrefix(p, "~") {
								src = p
							}
						}
						if src != "" {
							volumes = append(volumes, src)
						}
						continue
					}

					vol, ok := volRaw.(map[string]interface{})
					if !ok {
						continue
					}

					if src, ok := vol["source"].(string); ok && src != "" {
						volumes = append(volumes, src)
					}
				}
			}

			if nets, ok := svc["networks"].(map[string]interface{}); ok {
				for netName := range nets {
					networks = append(networks, netName)
				}
			} else if netsList, ok := svc["networks"].([]interface{}); ok {
				for _, netRaw := range netsList {
					if netName, ok := netRaw.(string); ok && netName != "" {
						networks = append(networks, netName)
					}
				}
			}
		}
	}

	if err := p.ValidateImages(images); err != nil {
		return fmt.Errorf("image policy violation: %w", err)
	}
	if err := p.ValidateVolumes(volumes); err != nil {
		return fmt.Errorf("volume policy violation: %w", err)
	}
	for _, net := range networks {
		if err := p.ValidateNetwork(net); err != nil {
			return fmt.Errorf("network policy violation: %w", err)
		}
	}

	if err := p.ValidatePrivileged(privilegedServices); err != nil {
		return fmt.Errorf("privileged mode policy violation: %w", err)
	}
	if err := p.ValidateHostNetwork(hostNetworkServices); err != nil {
		return fmt.Errorf("host network policy violation: %w", err)
	}
	if err := p.ValidateHostPID(hostPIDServices); err != nil {
		return fmt.Errorf("host pid policy violation: %w", err)
	}
	if err := p.ValidateHostIPC(hostIPCServices); err != nil {
		return fmt.Errorf("host ipc policy violation: %w", err)
	}
	if err := p.ValidateDockerSocket(append(append([]string{}, volumes...), devices...)); err != nil {
		return fmt.Errorf("docker socket policy violation: %w", err)
	}
	if err := p.ValidateCapAdd(capAdds); err != nil {
		return fmt.Errorf("cap_add policy violation: %w", err)
	}
	if err := p.ValidateDevices(devices); err != nil {
		return fmt.Errorf("device policy violation: %w", err)
	}
	if err := p.ValidateSecurityOpt(securityOpts); err != nil {
		return fmt.Errorf("security_opt policy violation: %w", err)
	}

	return nil
}
