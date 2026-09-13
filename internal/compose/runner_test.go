package compose

import (
	"reflect"
	"strings"
	"testing"
)

func TestBuildUpArgs(t *testing.T) {
	cases := []struct {
		name          string
		composeFile   string
		removeOrphans bool
		want          []string
	}{
		{
			name:        "NoFlags",
			composeFile: "docker-compose.yml",
			want:        []string{"compose", "-f", "docker-compose.yml", "up", "-d"},
		},
		{
			name:          "RemoveOrphansOnly",
			composeFile:   "docker-compose.yml",
			removeOrphans: true,
			want:          []string{"compose", "-f", "docker-compose.yml", "up", "-d", "--remove-orphans"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildUpArgs(tc.composeFile, tc.removeOrphans)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("buildUpArgs(%q, %v) = %v, want %v", tc.composeFile, tc.removeOrphans, got, tc.want)
			}
		})
	}
}

func TestBuildPullArgs(t *testing.T) {
	cases := []struct {
		name        string
		composeFile string
		forcePull   bool
		want        []string
	}{
		{
			name:        "NoFlags",
			composeFile: "docker-compose.yml",
			want:        []string{"compose", "-f", "docker-compose.yml", "pull"},
		},
		{
			name:        "ForcePull",
			composeFile: "compose.yml",
			forcePull:   true,
			want:        []string{"compose", "-f", "compose.yml", "pull", "--policy", "always"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildPullArgs(tc.composeFile, tc.forcePull)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("buildPullArgs(%q, %v) = %v, want %v", tc.composeFile, tc.forcePull, got, tc.want)
			}
		})
	}
}

func TestSanitizeProjectName(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{name: "AlreadyValidKebabCase", input: "pihole-red", want: "pihole-red"},
		{name: "UppercaseIsLowered", input: "Pihole-Red", want: "pihole-red"},
		{name: "SpacesBecomeDashes", input: "my stack name", want: "my-stack-name"},
		{name: "LeadingDigitIsFine", input: "1password", want: "1password"},
		{name: "LeadingDashesAreStripped", input: "--pihole-red", want: "pihole-red"},
		{name: "SymbolsBecomeDashes", input: "pihole@red!", want: "pihole-red-"},
		{name: "EmptyStringFallsBackToStack", input: "", want: "stack"},
		{name: "OnlyInvalidCharsFallsBackToStack", input: "@@@", want: "stack"},
		{name: "UnderscoresArePreserved", input: "qbittorrent_green", want: "qbittorrent_green"},
		{name: "LeadingUnderscoreIsStripped", input: "_api", want: "api"},
		{name: "LeadingUnderscoresAndDashesAreBothStripped", input: "_-_api", want: "api"},
		// The actual incident this guards against: stacks whose compose_path
		// happens to share a trailing directory segment (e.g.
		// stacks/pihole/red and stacks/jellyfin/red) must sanitize to
		// distinct project names since they're derived from the stack's own
		// unique name, not from any shared path segment.
		{name: "OurUseCasePiholeRed", input: "pihole-red", want: "pihole-red"},
		{name: "OurUseCaseJellyfinRed", input: "jellyfin-red", want: "jellyfin-red"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeProjectName(tc.input)
			if got != tc.want {
				t.Errorf("SanitizeProjectName(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestSanitizeProjectNameNeverCollidesForOurRedNodeStacks(t *testing.T) {
	// Regression test for the incident: stacks/pihole/red and
	// stacks/jellyfin/red both resolved to compose project "red" (the
	// shared trailing directory segment), so deploying one deleted the
	// other's containers via --remove-orphans. The stack names themselves
	// ("pihole-red", "jellyfin-red", "qbittorrent-red") are guaranteed
	// unique in wireops, so sanitizing them must keep that uniqueness.
	names := []string{"pihole-red", "jellyfin-red", "qbittorrent-red"}
	seen := make(map[string]string, len(names))
	for _, n := range names {
		sanitized := SanitizeProjectName(n)
		if prev, ok := seen[sanitized]; ok {
			t.Fatalf("SanitizeProjectName collision: %q and %q both sanitize to %q", prev, n, sanitized)
		}
		seen[sanitized] = n
	}
}

// TestSanitizeProjectNameCanCollideOnNormalization documents a known,
// intentional limitation of SanitizeProjectName as a pure string
// transform: two distinct stack names can normalize to the same project
// name (e.g. "prod/api" and "prod-api" both become "prod-api", since '/'
// and '-' both map to '-'). SanitizeProjectName has no visibility into
// other stacks, so it cannot reject this itself -- that's what
// sync.Renderer.ensureProjectNameUnique is for, checked against every
// other stack's name at render time. This test exists so a future change
// to the character-mapping rules doesn't accidentally "fix" this away
// without updating ensureProjectNameUnique's test coverage to match.
func TestSanitizeProjectNameCanCollideOnNormalization(t *testing.T) {
	a := SanitizeProjectName("prod/api")
	b := SanitizeProjectName("prod-api")
	if a != b {
		t.Fatalf(`expected "prod/api" and "prod-api" to normalize to the same value (documenting why a DB-level uniqueness check is required), got %q and %q`, a, b)
	}
}

func TestSafeEnv(t *testing.T) {
	t.Run("empty dockerConfigDir adds no DOCKER_CONFIG", func(t *testing.T) {
		env := safeEnv("")
		for _, kv := range env {
			if strings.HasPrefix(kv, "DOCKER_CONFIG=") {
				t.Fatalf("expected no DOCKER_CONFIG entry, found %q", kv)
			}
		}
	})

	t.Run("non-empty dockerConfigDir appends DOCKER_CONFIG", func(t *testing.T) {
		env := safeEnv("/tmp/registry-auth-dir")
		want := "DOCKER_CONFIG=/tmp/registry-auth-dir"
		found := false
		for _, kv := range env {
			if kv == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected env to contain %q, got %v", want, env)
		}
	})

	t.Run("non-empty dockerConfigDir overrides an inherited DOCKER_CONFIG", func(t *testing.T) {
		t.Setenv("DOCKER_CONFIG", "/inherited/from/worker/host")
		env := safeEnv("/tmp/registry-auth-dir")
		// safeEnv appends its own DOCKER_CONFIG after copying os.Environ(),
		// so the appended entry must be the last (and therefore winning, per
		// exec.Cmd.Env / os/exec semantics) DOCKER_CONFIG in the slice.
		lastIdx := -1
		for i, kv := range env {
			if strings.HasPrefix(kv, "DOCKER_CONFIG=") {
				lastIdx = i
			}
		}
		if lastIdx == -1 || env[lastIdx] != "DOCKER_CONFIG=/tmp/registry-auth-dir" {
			t.Fatalf("expected the last DOCKER_CONFIG entry to be the per-command dir, got %v", env)
		}
	})
}
