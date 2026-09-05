package telemetry

import (
	"testing"

	"github.com/wireops/wireops/internal/buildinfo"
)

func TestWorkerVersionReturnsBuildVersionByDefault(t *testing.T) {
	t.Setenv("WORKER_VERSION_OVERRIDE", "")

	if got := WorkerVersion(); got != buildinfo.Version {
		t.Fatalf("expected build version %q, got %q", buildinfo.Version, got)
	}
}

func TestWorkerVersionUsesOverrideWhenSet(t *testing.T) {
	t.Setenv("WORKER_VERSION_OVERRIDE", "0.1.0")

	if got := WorkerVersion(); got != "0.1.0" {
		t.Fatalf("expected override version 0.1.0, got %q", got)
	}
}

func TestWorkerVersionTrimsWhitespaceAroundOverride(t *testing.T) {
	t.Setenv("WORKER_VERSION_OVERRIDE", "  0.2.0  ")

	if got := WorkerVersion(); got != "0.2.0" {
		t.Fatalf("expected trimmed override version 0.2.0, got %q", got)
	}
}

func TestWorkerVersionIgnoresWhitespaceOnlyOverride(t *testing.T) {
	t.Setenv("WORKER_VERSION_OVERRIDE", "   ")

	if got := WorkerVersion(); got != buildinfo.Version {
		t.Fatalf("expected build version %q for whitespace-only override, got %q", buildinfo.Version, got)
	}
}
