// Package depgraph derives a dependency graph — services, networks, and
// volumes as nodes, with depends_on/network/volume relationships as edges —
// from a stack's rendered compose file. It reads the YAML already written to
// disk by internal/sync/renderer.go (itself the output of `docker compose
// config`, re-marshaled after label injection): no docker invocation happens
// here, this package is pure parsing.
package depgraph

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/wireops/wireops/internal/sync"
)

const (
	KindService = "service"
	KindNetwork = "network"
	KindVolume  = "volume"

	EdgeDependsOn = "depends_on"
	EdgeNetwork   = "network"
	EdgeVolume    = "volume"
)

// Node is one graph vertex: a compose service, network, or volume.
type Node struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Image    string `json:"image,omitempty"`
	// Slug is the custom thumbnail icon slug from a service's
	// `customization.image.slug` label/annotation (same convention the
	// stack's containers_list uses, internal/hooks/pb_hooks.go
	// extractContainersFromCompose) — lets the frontend render the same
	// custom icon here as elsewhere in the app instead of a generic one.
	Slug     string `json:"slug,omitempty"`
	Status   string `json:"status,omitempty"`
	Health   string `json:"health,omitempty"`
	External bool   `json:"external"`
}

// Edge is one typed relationship between two nodes.
type Edge struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Type   string `json:"type"`
	Label  string `json:"label,omitempty"`
}

// Graph is the full per-stack dependency graph.
type Graph struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

// ServiceNodeID/NetworkNodeID/VolumeNodeID are exported so callers (the
// route layer, matching live status back onto service nodes) can compute the
// same IDs this package generates without duplicating the prefixing scheme.
func ServiceNodeID(name string) string { return "service:" + name }
func NetworkNodeID(name string) string { return "network:" + name }
func VolumeNodeID(name string) string  { return "volume:" + name }

// BuildFromCompose parses a rendered stack compose file into a dependency
// graph.
func BuildFromCompose(yamlBytes []byte) (*Graph, error) {
	var raw map[string]interface{}
	if err := yaml.Unmarshal(yamlBytes, &raw); err != nil {
		return nil, fmt.Errorf("invalid compose YAML: %w", err)
	}

	g := &Graph{}
	networkNodes := map[string]bool{}
	volumeNodes := map[string]bool{}

	for _, name := range sortedKeys(raw["networks"]) {
		def := raw["networks"].(map[string]interface{})[name]
		g.Nodes = append(g.Nodes, Node{ID: NetworkNodeID(name), Kind: KindNetwork, External: isExternal(def)})
		networkNodes[name] = true
	}
	for _, name := range sortedKeys(raw["volumes"]) {
		def := raw["volumes"].(map[string]interface{})[name]
		g.Nodes = append(g.Nodes, Node{ID: VolumeNodeID(name), Kind: KindVolume, External: isExternal(def)})
		volumeNodes[name] = true
	}

	services, _ := raw["services"].(map[string]interface{})
	for _, name := range sortedKeys(services) {
		svc, ok := services[name].(map[string]interface{})
		if !ok {
			continue
		}

		image, _ := svc["image"].(string)
		g.Nodes = append(g.Nodes, Node{ID: ServiceNodeID(name), Kind: KindService, Image: image, Slug: customImageSlug(svc)})

		for _, dep := range dependsOn(svc["depends_on"]) {
			g.Edges = append(g.Edges, Edge{Source: ServiceNodeID(name), Target: ServiceNodeID(dep.name), Type: EdgeDependsOn, Label: dep.condition})
		}

		for _, netName := range serviceNetworks(svc["networks"]) {
			if !networkNodes[netName] {
				// Referenced but never declared top-level: compose creates it
				// implicitly (e.g. the project's default network) — synthesize
				// an internal node so the edge has a target.
				g.Nodes = append(g.Nodes, Node{ID: NetworkNodeID(netName), Kind: KindNetwork, External: false})
				networkNodes[netName] = true
			}
			g.Edges = append(g.Edges, Edge{Source: ServiceNodeID(name), Target: NetworkNodeID(netName), Type: EdgeNetwork})
		}

		for _, volName := range namedVolumes(svc["volumes"]) {
			if !volumeNodes[volName] {
				g.Nodes = append(g.Nodes, Node{ID: VolumeNodeID(volName), Kind: KindVolume, External: false})
				volumeNodes[volName] = true
			}
			g.Edges = append(g.Edges, Edge{Source: ServiceNodeID(name), Target: VolumeNodeID(volName), Type: EdgeVolume})
		}
	}

	return g, nil
}

// sortedKeys returns the sorted keys of v if it's a map[string]interface{},
// or nil otherwise — every top-level compose block (services/networks/
// volumes) is iterated this way so output order is stable rather than
// following Go's randomized map order.
func sortedKeys(v interface{}) []string {
	m, ok := v.(map[string]interface{})
	if !ok {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// customImageSlug reads a service's custom thumbnail icon slug from its
// `customization.image.slug` label or annotation (labels/annotations at the
// service level or under deploy:), mirroring extractContainersFromCompose in
// internal/hooks/pb_hooks.go so the graph shows the same custom icon the
// rest of the app does.
func customImageSlug(svc map[string]interface{}) string {
	check := func(meta map[string]interface{}) (string, bool) {
		if val, ok := meta["customization.image.slug"].(string); ok && val != "" {
			return val, true
		}
		return "", false
	}

	if slug, ok := check(sync.NormalizeToMap(svc["labels"])); ok {
		return slug
	}
	if slug, ok := check(sync.NormalizeToMap(svc["annotations"])); ok {
		return slug
	}
	if deploy, ok := svc["deploy"].(map[string]interface{}); ok {
		if slug, ok := check(sync.NormalizeToMap(deploy["labels"])); ok {
			return slug
		}
		if slug, ok := check(sync.NormalizeToMap(deploy["annotations"])); ok {
			return slug
		}
	}
	return ""
}

// isExternal reports whether a top-level networks:/volumes: entry declares
// `external: true` or the nested `external: { name: ... }` form — either
// way, the resource is not owned by this stack.
func isExternal(def interface{}) bool {
	m, ok := def.(map[string]interface{})
	if !ok {
		return false
	}
	switch ext := m["external"].(type) {
	case bool:
		return ext
	case map[string]interface{}:
		return true
	}
	return false
}

type dependency struct {
	name      string
	condition string
}

// dependsOn normalizes a service's depends_on, accepting both the short list
// form (["a", "b"]) and the long map form ({"a": {"condition": "..."}}).
func dependsOn(v interface{}) []dependency {
	switch val := v.(type) {
	case []interface{}:
		var out []dependency
		for _, item := range val {
			if name, ok := item.(string); ok && name != "" {
				out = append(out, dependency{name: name})
			}
		}
		return out
	case map[string]interface{}:
		names := make([]string, 0, len(val))
		for name := range val {
			names = append(names, name)
		}
		sort.Strings(names)
		out := make([]dependency, 0, len(names))
		for _, name := range names {
			condition := ""
			if condMap, ok := val[name].(map[string]interface{}); ok {
				condition, _ = condMap["condition"].(string)
			}
			out = append(out, dependency{name: name, condition: condition})
		}
		return out
	}
	return nil
}

// serviceNetworks normalizes a service's networks: entry, accepting both the
// list form and the map form (which carries per-network aliases/ips this
// package doesn't need).
func serviceNetworks(v interface{}) []string {
	switch nets := v.(type) {
	case map[string]interface{}:
		names := make([]string, 0, len(nets))
		for name := range nets {
			names = append(names, name)
		}
		sort.Strings(names)
		return names
	case []interface{}:
		var out []string
		for _, raw := range nets {
			if name, ok := raw.(string); ok && name != "" {
				out = append(out, name)
			}
		}
		return out
	}
	return nil
}

// isBindSource reports whether a volume mount source is a host path (bind
// mount) rather than a named volume reference — mirrors
// internal/lint/rules_structure.go's isHostPath.
func isBindSource(src string) bool {
	return strings.HasPrefix(src, "/") || strings.HasPrefix(src, "./") ||
		strings.HasPrefix(src, "../") || strings.HasPrefix(src, "~")
}

// namedVolumes normalizes a service's volumes: entry (short string form and
// long map form) into the set of named-volume sources it references,
// excluding bind mounts and anonymous volumes — mirrors the mount
// classification in internal/lint/rules_structure.go's serviceMounts.
func namedVolumes(v interface{}) []string {
	list, ok := v.([]interface{})
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	for _, raw := range list {
		switch m := raw.(type) {
		case string:
			spec := strings.Trim(strings.TrimSpace(m), `"'`)
			parts := strings.Split(spec, ":")
			if len(parts) < 2 {
				continue // anonymous volume: only a container path
			}
			source := strings.TrimSpace(parts[0])
			if source == "" || isBindSource(source) {
				continue
			}
			seen[source] = true
		case map[string]interface{}:
			typ, _ := m["type"].(string)
			if typ != "volume" {
				continue
			}
			source, _ := m["source"].(string)
			if source == "" || isBindSource(source) {
				continue
			}
			seen[source] = true
		}
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
