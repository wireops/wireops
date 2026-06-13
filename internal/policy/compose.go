package policy

import (
	"fmt"
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

	if svcs, ok := configMap["services"].(map[string]interface{}); ok {
		for _, svcRaw := range svcs {
			svc, ok := svcRaw.(map[string]interface{})
			if !ok {
				continue
			}

			if img, ok := svc["image"].(string); ok && img != "" {
				images = append(images, img)
			}

			if vols, ok := svc["volumes"].([]interface{}); ok {
				for _, volRaw := range vols {
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

	return nil
}
