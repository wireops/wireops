package app

import (
	"testing"
	"time"

	"github.com/wireops/wireops/worker/handlers"
)

func TestDrainActiveWorkReturnsOnTimeout(t *testing.T) {
	cancel := func() {}
	handlers.ActiveCommands.Store("cmd-timeout", cancel)
	defer handlers.ActiveCommands.Delete("cmd-timeout")

	start := time.Now()
	timedOut := drainActiveWork(30*time.Millisecond, 5*time.Millisecond)
	elapsed := time.Since(start)

	if !timedOut {
		t.Fatalf("expected drainActiveWork to report timeout")
	}
	if elapsed > 250*time.Millisecond {
		t.Fatalf("expected drainActiveWork to return promptly after timeout, took %v", elapsed)
	}
}
