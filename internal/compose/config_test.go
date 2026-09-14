package compose

import "testing"

func TestIsComposeFile(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name: "ValidComposeFile",
			input: `
services:
  web:
    image: nginx
  db:
    image: postgres
`,
			want: true,
		},
		{
			name: "EmptyServicesMap",
			input: `
services: {}
`,
			want: false,
		},
		{
			name: "MissingServicesKey",
			input: `
version: "3"
volumes:
  data: {}
`,
			want: false,
		},
		{
			name: "JobYAMLInput",
			input: `
title: My Job
image: alpine
cron: "0 * * * *"
command: echo hello
`,
			want: false,
		},
		{
			name:  "InvalidYAML",
			input: `{not: valid: yaml:`,
			want:  false,
		},
		{
			name:  "EmptyInput",
			input: ``,
			want:  false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsComposeFile([]byte(tc.input))
			if got != tc.want {
				t.Errorf("IsComposeFile(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

func TestComposeCandidateError(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		wantError bool
	}{
		{
			name: "ValidComposeFile",
			input: `
services:
  web:
    image: nginx
`,
			wantError: false,
		},
		{
			name:      "MalformedYAML",
			input:     `{not: valid: yaml:`,
			wantError: true,
		},
		{
			name: "MissingServicesKey",
			input: `
version: "3"
volumes:
  data: {}
`,
			wantError: true,
		},
		{
			name: "EmptyServicesMap",
			input: `
services: {}
`,
			wantError: true,
		},
		{
			name: "ServicesTypeMismatch",
			input: `
services: []
`,
			wantError: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ComposeCandidateError([]byte(tc.input))
			if tc.wantError && err == nil {
				t.Errorf("ComposeCandidateError(%q) = nil, want error", tc.name)
			}
			if !tc.wantError && err != nil {
				t.Errorf("ComposeCandidateError(%q) = %v, want nil", tc.name, err)
			}
		})
	}
}

func TestExtractProjectName(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		want      string
		wantError bool
	}{
		{
			name: "NamePresent",
			input: `
name: pihole-red
services:
  pihole:
    image: pihole/pihole
`,
			want: "pihole-red",
		},
		{
			// Mirrors the actual incident: two stacks whose compose files
			// live in directories sharing the same trailing segment
			// ("red") must still be distinguishable once rendered, because
			// the renderer forces a distinct top-level `name` per stack.
			name: "DistinctNameForCollidingDirectoryStack",
			input: `
name: jellyfin-red
services:
  jellyfin:
    image: lscr.io/linuxserver/jellyfin
`,
			want: "jellyfin-red",
		},
		{
			name: "NameMissing",
			input: `
services:
  web:
    image: nginx
`,
			wantError: true,
		},
		{
			name:      "MalformedYAML",
			input:     `{not: valid: yaml:`,
			wantError: true,
		},
		{
			name: "EmptyNameValue",
			input: `
name: ""
services:
  web:
    image: nginx
`,
			wantError: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ExtractProjectName([]byte(tc.input))
			if tc.wantError {
				if err == nil {
					t.Fatalf("ExtractProjectName(%q) = %q, nil, want error", tc.name, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ExtractProjectName(%q) unexpected error: %v", tc.name, err)
			}
			if got != tc.want {
				t.Errorf("ExtractProjectName(%q) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

func TestRewriteAutoNamedResources(t *testing.T) {
	t.Run("RewritesNetworkAndVolumeNamesSharingOldPrefix", func(t *testing.T) {
		// Mirrors the actual incident payload: `docker compose config` for
		// stacks/pihole/red resolved the implicit default network to
		// "red_default" (from the shared directory basename "red") before
		// the renderer overrides the top-level project name to "pihole-red".
		configMap := map[string]interface{}{
			"networks": map[string]interface{}{
				"default": map[string]interface{}{"name": "red_default"},
			},
			"volumes": map[string]interface{}{
				"data": map[string]interface{}{"name": "red_data"},
			},
		}

		RewriteAutoNamedResources(configMap, "red", "pihole-red")

		networks := configMap["networks"].(map[string]interface{})
		defaultNet := networks["default"].(map[string]interface{})
		if got := defaultNet["name"]; got != "pihole-red_default" {
			t.Errorf("networks.default.name = %v, want %q", got, "pihole-red_default")
		}

		volumes := configMap["volumes"].(map[string]interface{})
		dataVol := volumes["data"].(map[string]interface{})
		if got := dataVol["name"]; got != "pihole-red_data" {
			t.Errorf("volumes.data.name = %v, want %q", got, "pihole-red_data")
		}
	})

	t.Run("OurUseCaseDistinctStacksSameOldPrefixRewriteToDistinctNames", func(t *testing.T) {
		// The exact real scenario: pihole-red and jellyfin-red both would
		// have defaulted to compose project "red" (shared trailing
		// directory segment). After each is independently rewritten with
		// its own stack name, their resource names must no longer collide.
		piholeConfig := map[string]interface{}{
			"networks": map[string]interface{}{"default": map[string]interface{}{"name": "red_default"}},
		}
		jellyfinConfig := map[string]interface{}{
			"networks": map[string]interface{}{"default": map[string]interface{}{"name": "red_default"}},
		}

		RewriteAutoNamedResources(piholeConfig, "red", "pihole-red")
		RewriteAutoNamedResources(jellyfinConfig, "red", "jellyfin-red")

		piholeName := piholeConfig["networks"].(map[string]interface{})["default"].(map[string]interface{})["name"]
		jellyfinName := jellyfinConfig["networks"].(map[string]interface{})["default"].(map[string]interface{})["name"]
		if piholeName == jellyfinName {
			t.Fatalf("regression: both stacks still resolved to the same default network name %v", piholeName)
		}
		if piholeName != "pihole-red_default" || jellyfinName != "jellyfin-red_default" {
			t.Errorf("got pihole=%v jellyfin=%v, want pihole-red_default and jellyfin-red_default", piholeName, jellyfinName)
		}
	})

	t.Run("NoOpWhenOldNameEmpty", func(t *testing.T) {
		configMap := map[string]interface{}{
			"networks": map[string]interface{}{"default": map[string]interface{}{"name": "red_default"}},
		}
		RewriteAutoNamedResources(configMap, "", "pihole-red")
		got := configMap["networks"].(map[string]interface{})["default"].(map[string]interface{})["name"]
		if got != "red_default" {
			t.Errorf("expected no-op when oldName is empty, got %v", got)
		}
	})

	t.Run("NoOpWhenOldNameMatchesNewName", func(t *testing.T) {
		configMap := map[string]interface{}{
			"networks": map[string]interface{}{"default": map[string]interface{}{"name": "red_default"}},
		}
		RewriteAutoNamedResources(configMap, "red", "red")
		got := configMap["networks"].(map[string]interface{})["default"].(map[string]interface{})["name"]
		if got != "red_default" {
			t.Errorf("expected no-op when oldName == newName, got %v", got)
		}
	})

	t.Run("LeavesUnrelatedCustomNameUntouched", func(t *testing.T) {
		// A user-declared external/custom network name that doesn't happen
		// to start with the old project prefix must be left alone.
		configMap := map[string]interface{}{
			"networks": map[string]interface{}{
				"shared-network": map[string]interface{}{"name": "shared-network"},
			},
		}
		RewriteAutoNamedResources(configMap, "red", "pihole-red")
		got := configMap["networks"].(map[string]interface{})["shared-network"].(map[string]interface{})["name"]
		if got != "shared-network" {
			t.Errorf("expected unrelated custom network name to be untouched, got %v", got)
		}
	})

	t.Run("LeavesExplicitResourceNameUntouchedEvenWhenItSharesOldPrefix", func(t *testing.T) {
		// A network or volume the user explicitly named "<oldName>_something"
		// on purpose (not auto-generated by `docker compose config`) must
		// survive the rewrite. It's only recognizable as "explicit" here
		// because it doesn't match the exact "<oldName>_<resourceKey>"
		// pattern docker compose uses for an implicit name -- the resource's
		// own YAML key is "extra", not "custom", so "red_custom" can't have
		// been auto-generated for it.
		configMap := map[string]interface{}{
			"networks": map[string]interface{}{
				"extra": map[string]interface{}{"name": "red_custom"},
			},
			"volumes": map[string]interface{}{
				"backups": map[string]interface{}{"name": "red_backups-explicit"},
			},
		}
		RewriteAutoNamedResources(configMap, "red", "pihole-red")
		gotNet := configMap["networks"].(map[string]interface{})["extra"].(map[string]interface{})["name"]
		if gotNet != "red_custom" {
			t.Errorf("expected explicit network name sharing the old prefix to be untouched, got %v", gotNet)
		}
		gotVol := configMap["volumes"].(map[string]interface{})["backups"].(map[string]interface{})["name"]
		if gotVol != "red_backups-explicit" {
			t.Errorf("expected explicit volume name sharing the old prefix to be untouched, got %v", gotVol)
		}
	})

	t.Run("HandlesMissingSectionsAndMalformedEntriesWithoutPanicking", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("RewriteAutoNamedResources panicked: %v", r)
			}
		}()
		RewriteAutoNamedResources(map[string]interface{}{}, "red", "pihole-red")
		RewriteAutoNamedResources(map[string]interface{}{"networks": "not-a-map"}, "red", "pihole-red")
		RewriteAutoNamedResources(map[string]interface{}{
			"networks": map[string]interface{}{"default": "not-a-map-either"},
		}, "red", "pihole-red")
		RewriteAutoNamedResources(map[string]interface{}{
			"networks": map[string]interface{}{"default": map[string]interface{}{}},
		}, "red", "pihole-red")
	})
}

func TestPreserveAutoNamedVolumes(t *testing.T) {
	current := map[string]interface{}{
		"volumes": map[string]interface{}{
			"data":    map[string]interface{}{"name": "pihole-red_data"},
			"cache":   map[string]interface{}{"name": "custom-cache"},
			"newdata": map[string]interface{}{"name": "pihole-red_newdata"},
		},
	}
	previous := map[string]interface{}{
		"volumes": map[string]interface{}{
			"data":  map[string]interface{}{"name": "red_data"},
			"cache": map[string]interface{}{"name": "old-custom-cache"},
		},
	}

	PreserveAutoNamedVolumes(current, previous, "pihole-red")
	volumes := current["volumes"].(map[string]interface{})
	if got := volumes["data"].(map[string]interface{})["name"]; got != "red_data" {
		t.Fatalf("data volume = %v, want red_data", got)
	}
	if got := volumes["cache"].(map[string]interface{})["name"]; got != "custom-cache" {
		t.Fatalf("explicit cache volume changed to %v", got)
	}
	if got := volumes["newdata"].(map[string]interface{})["name"]; got != "pihole-red_newdata" {
		t.Fatalf("new volume changed to %v", got)
	}
}

func TestInitServiceNames(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  map[string]bool
	}{
		{
			name: "MapFormLabels",
			input: `
services:
  web:
    image: nginx
  migrate:
    image: myapp
    labels:
      customization.init: "true"
`,
			want: map[string]bool{"migrate": true},
		},
		{
			name: "ListFormLabels",
			input: `
services:
  web:
    image: nginx
  migrate:
    image: myapp
    labels:
      - "customization.init=true"
`,
			want: map[string]bool{"migrate": true},
		},
		{
			name: "FalsyValueIsNotInit",
			input: `
services:
  migrate:
    image: myapp
    labels:
      customization.init: "false"
`,
			want: map[string]bool{},
		},
		{
			name: "NoLabelsDefined",
			input: `
services:
  web:
    image: nginx
`,
			want: map[string]bool{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := InitServiceNames([]byte(tc.input))
			if err != nil {
				t.Fatalf("InitServiceNames(%q) error = %v", tc.name, err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("InitServiceNames(%q) = %v, want %v", tc.name, got, tc.want)
			}
			for name := range tc.want {
				if !got[name] {
					t.Errorf("InitServiceNames(%q) missing expected init service %q, got %v", tc.name, name, got)
				}
			}
		})
	}
}
