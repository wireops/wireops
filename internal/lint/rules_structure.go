package lint

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// interpolationRe matches a compose interpolation in either the ${VAR} /
// ${VAR:-default} form or the bare $VAR form. The leading "$$" alternative is
// compose's own escape for a literal dollar sign: it is matched so that it
// consumes the pair, then discarded, so "$$VAR" is not mistaken for a
// reference to VAR.
var interpolationRe = regexp.MustCompile(`\$\$|\$\{([^}]*)\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

// interpolation is one ${...} reference found in a compose value.
type interpolation struct {
	// Raw is the reference exactly as written, e.g. "${TAG:-latest}".
	Raw string
	// Name is the variable it reads, e.g. "TAG".
	Name string
	// HasDefault is true for the ${VAR:-x} / ${VAR-x} / ${VAR:+x} forms,
	// which resolve even when VAR is unset and so are never "missing".
	HasDefault bool
}

// parseInterpolations extracts every variable reference in s, skipping "$$"
// escapes.
func parseInterpolations(s string) []interpolation {
	var out []interpolation
	for _, m := range interpolationRe.FindAllStringSubmatch(s, -1) {
		if m[0] == "$$" {
			continue
		}
		expr := m[2]
		if m[1] != "" || (len(m[0]) > 1 && m[0][1] == '{') {
			expr = m[1]
		}
		if expr == "" {
			continue
		}
		ref := interpolation{Raw: m[0]}
		// Split on the first modifier: ":-", ":?", ":+", "-", "?", "+".
		if idx := strings.IndexAny(expr, ":-?+"); idx > 0 {
			ref.Name = expr[:idx]
			ref.HasDefault = !strings.HasPrefix(expr[idx:], ":?") && !strings.HasPrefix(expr[idx:], "?")
		} else {
			ref.Name = expr
		}
		if ref.Name == "" {
			continue
		}
		out = append(out, ref)
	}
	return out
}

func init() {
	Register(RuleNoServices, ruleNoServices)
	Register(RuleMissingImage, ruleMissingImage)
	Register(RuleUndeclaredNetwork, ruleUndeclaredNetwork)
	Register(RuleUndeclaredVolume, ruleUndeclaredVolume)
	Register(RuleUnresolvedVariable, ruleUnresolvedVariable)
	Register(RuleObsoleteVersionKey, ruleObsoleteVersionKey)
	Register(RuleUnescapedConfigInterpolation, ruleUnescapedConfigInterpolation)
}

// ruleNoServices catches a compose file that parses but deploys nothing.
func ruleNoServices(cfg *Config, _ Context) []Finding {
	if len(cfg.Services) > 0 {
		return nil
	}
	return []Finding{{
		Severity: SeverityError,
		Path:     "services",
		Message:  "compose file declares no services",
		Hint:     "add at least one service under the top-level `services:` key",
	}}
}

// ruleMissingImage catches a service that names neither an image to pull nor a
// build context to build one from. wireops workers never build images, so a
// build-only service cannot deploy either — both cases are reported, with
// different wording.
func ruleMissingImage(cfg *Config, _ Context) []Finding {
	var out []Finding
	for _, svc := range cfg.Services {
		image, _ := svc.Raw["image"].(string)
		if image != "" {
			continue
		}
		_, hasBuild := svc.Raw["build"]
		if hasBuild {
			out = append(out, Finding{
				Severity: SeverityError,
				Service:  svc.Name,
				Path:     fmt.Sprintf("services.%s.build", svc.Name),
				Message:  fmt.Sprintf("service %q only has a `build` section and no `image`", svc.Name),
				Hint:     "wireops workers deploy pre-built images; build and push the image, then reference it with `image:`",
			})
			continue
		}
		out = append(out, Finding{
			Severity: SeverityError,
			Service:  svc.Name,
			Path:     fmt.Sprintf("services.%s", svc.Name),
			Message:  fmt.Sprintf("service %q has no `image`", svc.Name),
			Hint:     "every service needs an `image:` to deploy",
		})
	}
	return out
}

// ruleUndeclaredNetwork catches a service attached to a network that has no
// top-level definition. Compose only auto-creates "default"; anything else
// must be declared or the deploy fails on the worker.
func ruleUndeclaredNetwork(cfg *Config, _ Context) []Finding {
	declared := cfg.topLevelKeys("networks")
	var out []Finding
	for _, svc := range cfg.Services {
		for _, net := range serviceNetworks(svc.Raw) {
			if net == "default" || declared[net] {
				continue
			}
			out = append(out, Finding{
				Severity: SeverityError,
				Service:  svc.Name,
				Path:     fmt.Sprintf("services.%s.networks", svc.Name),
				Message:  fmt.Sprintf("service %q joins network %q, which is not declared at the top level", svc.Name, net),
				Hint:     fmt.Sprintf("add %q under the top-level `networks:` key (use `external: true` if the worker already has it)", net),
			})
		}
	}
	return out
}

// ruleUndeclaredVolume catches a service mounting a named volume with no
// top-level definition. Bind mounts (paths) are exempt — they need no
// declaration.
func ruleUndeclaredVolume(cfg *Config, _ Context) []Finding {
	declared := cfg.topLevelKeys("volumes")
	var out []Finding
	for _, svc := range cfg.Services {
		for _, mount := range serviceMounts(svc.Raw) {
			if mount.isBind || mount.source == "" || declared[mount.source] {
				continue
			}
			out = append(out, Finding{
				Severity: SeverityError,
				Service:  svc.Name,
				Path:     fmt.Sprintf("services.%s.volumes", svc.Name),
				Message:  fmt.Sprintf("service %q mounts named volume %q, which is not declared at the top level", svc.Name, mount.source),
				Hint:     fmt.Sprintf("add %q under the top-level `volumes:` key", mount.source),
			})
		}
	}
	return out
}

// ruleUnresolvedVariable catches ${VAR} references that nothing will define at
// deploy time.
//
// wireops renders compose with --no-interpolate and ships a .env alongside it,
// so a ${VAR} surviving into the rendered file is the normal case, not a
// problem — the worker resolves it during `docker compose up`. Only a variable
// that appears in no known source is a real finding, which is why the rule
// does nothing unless the caller supplied EnvKeys: without that set it would
// flag every stack that uses variables at all.
//
// References carrying a default (${VAR:-x}) always resolve and are skipped.
// Each distinct variable is reported once per service, in name order, rather
// than once per occurrence — a var used in five places is one problem.
func ruleUnresolvedVariable(cfg *Config, ctx Context) []Finding {
	if ctx.EnvKeys == nil {
		return nil
	}
	var out []Finding
	for _, svc := range cfg.Services {
		refs := map[string]bool{}
		collectInterpolations(svc.Raw, refs)

		var missing []string
		for name := range refs {
			if !ctx.EnvKeys[name] {
				missing = append(missing, name)
			}
		}
		if len(missing) == 0 {
			continue
		}
		sort.Strings(missing)
		out = append(out, Finding{
			Severity: SeverityWarning,
			Service:  svc.Name,
			Path:     fmt.Sprintf("services.%s", svc.Name),
			Message: fmt.Sprintf("service %q references variable(s) nothing defines: %s",
				svc.Name, strings.Join(missing, ", ")),
			Hint: "add them as stack environment variables, as global variables, or give them a default with ${VAR:-fallback}",
		})
	}
	return out
}

// ruleObsoleteVersionKey flags the top-level `version:` key, which the Compose
// Spec dropped and current Docker Compose warns about on every invocation.
func ruleObsoleteVersionKey(cfg *Config, _ Context) []Finding {
	if _, ok := cfg.Raw["version"]; !ok {
		return nil
	}
	return []Finding{{
		Severity: SeverityInfo,
		Path:     "version",
		Message:  "top-level `version:` key is obsolete",
		Hint:     "remove it — Compose ignores it and warns about it on every run",
	}}
}

// ruleUnescapedConfigInterpolation catches a `configs:` content block (as
// synthesized from dev.wireops.config.* annotations, or declared directly)
// containing a `${VAR}`/`$VAR` reference. `docker compose config` interpolates
// the *entire* rendered file, including embedded config content, before the
// container ever starts — so a template file that legitimately uses shell/nginx
// `$var` syntax gets silently mangled unless the author escapes it as `$$`.
func ruleUnescapedConfigInterpolation(cfg *Config, _ Context) []Finding {
	configs, ok := cfg.Raw["configs"].(map[string]interface{})
	if !ok {
		return nil
	}
	names := make([]string, 0, len(configs))
	for name := range configs {
		names = append(names, name)
	}
	sort.Strings(names)

	var out []Finding
	for _, name := range names {
		entry, ok := configs[name].(map[string]interface{})
		if !ok {
			continue
		}
		content, ok := entry["content"].(string)
		if !ok || content == "" {
			continue
		}
		refs := parseInterpolations(content)
		if len(refs) == 0 {
			continue
		}
		seen := map[string]bool{}
		var vars []string
		for _, ref := range refs {
			if !seen[ref.Name] {
				seen[ref.Name] = true
				vars = append(vars, ref.Name)
			}
		}
		sort.Strings(vars)
		out = append(out, Finding{
			Severity: SeverityWarning,
			Path:     fmt.Sprintf("configs.%s.content", name),
			Message: fmt.Sprintf("config %q content contains unescaped interpolation reference(s): %s",
				name, strings.Join(vars, ", ")),
			Hint: "docker compose interpolates the whole rendered file, including embedded config content, before the container starts — escape a literal $ as $$ if it is not meant to be a compose variable",
		})
	}
	return out
}

// collectInterpolations walks an arbitrary decoded-JSON value and records the
// name of every variable referenced without a default. References that carry
// one always resolve, so they are not collected.
func collectInterpolations(v interface{}, out map[string]bool) {
	switch val := v.(type) {
	case string:
		for _, ref := range parseInterpolations(val) {
			if !ref.HasDefault {
				out[ref.Name] = true
			}
		}
	case []interface{}:
		for _, item := range val {
			collectInterpolations(item, out)
		}
	case map[string]interface{}:
		for _, item := range val {
			collectInterpolations(item, out)
		}
	}
}

// serviceNetworks returns the networks a service joins, handling both the map
// form (`networks: {backend: {...}}`) and the list form (`networks: [backend]`),
// in sorted order.
func serviceNetworks(svc map[string]interface{}) []string {
	var out []string
	switch nets := svc["networks"].(type) {
	case map[string]interface{}:
		for name := range nets {
			out = append(out, name)
		}
		sort.Strings(out)
	case []interface{}:
		for _, raw := range nets {
			if name, ok := raw.(string); ok && name != "" {
				out = append(out, name)
			}
		}
	}
	return out
}

// mount is one entry of a service's `volumes:` list, normalized across the
// short string form and the long map form.
type mount struct {
	source string
	target string
	// isBind is true when source is a host path rather than a named volume.
	isBind bool
	// readOnly reflects `:ro` or `read_only: true`.
	readOnly bool
	// raw is the entry as written, for use in messages.
	raw string
}

func isHostPath(src string) bool {
	return strings.HasPrefix(src, "/") || strings.HasPrefix(src, "./") ||
		strings.HasPrefix(src, "../") || strings.HasPrefix(src, "~")
}

// serviceMounts normalizes a service's volume entries. It mirrors the parsing
// in internal/policy's compose extraction so lint and policy agree on what a
// given entry means.
func serviceMounts(svc map[string]interface{}) []mount {
	list, ok := svc["volumes"].([]interface{})
	if !ok {
		return nil
	}
	var out []mount
	for _, raw := range list {
		switch v := raw.(type) {
		case string:
			spec := strings.Trim(strings.TrimSpace(v), `"'`)
			parts := strings.Split(spec, ":")
			m := mount{raw: spec}
			switch {
			case len(parts) == 1:
				// Anonymous volume: only a container path.
				m.target = parts[0]
			default:
				m.source = strings.TrimSpace(parts[0])
				m.target = strings.TrimSpace(parts[1])
				if len(parts) > 2 && strings.Contains(parts[2], "ro") {
					m.readOnly = true
				}
			}
			m.isBind = isHostPath(m.source)
			out = append(out, m)
		case map[string]interface{}:
			m := mount{}
			m.source, _ = v["source"].(string)
			m.target, _ = v["target"].(string)
			m.readOnly, _ = v["read_only"].(bool)
			typ, _ := v["type"].(string)
			m.isBind = typ == "bind" || isHostPath(m.source)
			m.raw = m.source
			if m.target != "" {
				m.raw = m.source + ":" + m.target
			}
			out = append(out, m)
		}
	}
	return out
}
