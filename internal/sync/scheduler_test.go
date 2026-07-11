package sync

import (
	"os"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

func newSchedulerTestStack(t *testing.T) *core.Record {
	t.Helper()
	col := core.NewBaseCollection("stacks_scheduler_test")
	col.Fields.Add(&core.NumberField{Name: "sync_interval_seconds"})
	return core.NewRecord(col)
}

func TestResolveSyncInterval(t *testing.T) {
	t.Setenv("SCAN_PERIOD", "")

	t.Run("PositiveOverrideUsed", func(t *testing.T) {
		stack := newSchedulerTestStack(t)
		stack.Set("sync_interval_seconds", 45)

		got := resolveSyncInterval(stack)
		if want := 45 * time.Second; got != want {
			t.Errorf("resolveSyncInterval() = %v, want %v", got, want)
		}
	})

	t.Run("ZeroFallsBackToGlobalScanPeriod", func(t *testing.T) {
		stack := newSchedulerTestStack(t)
		stack.Set("sync_interval_seconds", 0)

		got := resolveSyncInterval(stack)
		if want := 10 * time.Second; got != want {
			t.Errorf("resolveSyncInterval() = %v, want %v (default SCAN_PERIOD)", got, want)
		}
	})

	t.Run("NegativeFallsBackToGlobalScanPeriod", func(t *testing.T) {
		stack := newSchedulerTestStack(t)
		stack.Set("sync_interval_seconds", -5)

		got := resolveSyncInterval(stack)
		if want := 10 * time.Second; got != want {
			t.Errorf("resolveSyncInterval() = %v, want %v (default SCAN_PERIOD)", got, want)
		}
	})

	t.Run("FallbackHonorsCustomScanPeriodEnv", func(t *testing.T) {
		if err := os.Setenv("SCAN_PERIOD", "30"); err != nil {
			t.Fatalf("failed to set SCAN_PERIOD: %v", err)
		}
		defer os.Unsetenv("SCAN_PERIOD")

		stack := newSchedulerTestStack(t)

		got := resolveSyncInterval(stack)
		if want := 30 * time.Second; got != want {
			t.Errorf("resolveSyncInterval() = %v, want %v", got, want)
		}
	})
}
