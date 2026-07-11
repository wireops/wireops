package sync

import (
	"context"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"github.com/wireops/wireops/internal/config"
)

func newFlagsTestStack(t *testing.T) *core.Record {
	t.Helper()
	col := core.NewBaseCollection("stacks_flags_test")
	col.Fields.Add(&core.BoolField{Name: "force_pull"})
	col.Fields.Add(&core.BoolField{Name: "remove_orphans"})
	col.Fields.Add(&core.TextField{Name: "config_source"})
	col.Fields.Add(&core.NumberField{Name: "deploy_timeout_seconds"})
	return core.NewRecord(col)
}

func TestResolveComposeRuntimeFlags(t *testing.T) {
	cases := []struct {
		name              string
		forcePull         bool
		removeOrphans     bool
		configSource      string
		wantForcePull     bool
		wantRemoveOrphans bool
	}{
		{
			name:              "ManualStackAlwaysRemovesOrphans",
			forcePull:         false,
			removeOrphans:     false,
			configSource:      "manual",
			wantForcePull:     false,
			wantRemoveOrphans: true,
		},
		{
			name:              "PreExistingStackWithoutConfigSource",
			removeOrphans:     false,
			configSource:      "",
			wantRemoveOrphans: true,
		},
		{
			name:              "WireopsFileExplicitFalseHonored",
			removeOrphans:     false,
			configSource:      "wireops_file",
			wantRemoveOrphans: false,
		},
		{
			name:              "WireopsFileExplicitTrueHonored",
			removeOrphans:     true,
			configSource:      "wireops_file",
			wantRemoveOrphans: true,
		},
		{
			name:              "ForcePullPassedThrough",
			forcePull:         true,
			configSource:      "wireops_file",
			removeOrphans:     true,
			wantForcePull:     true,
			wantRemoveOrphans: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stack := newFlagsTestStack(t)
			stack.Set("force_pull", tc.forcePull)
			stack.Set("remove_orphans", tc.removeOrphans)
			stack.Set("config_source", tc.configSource)

			gotForcePull, gotRemoveOrphans := resolveComposeRuntimeFlags(stack)
			if gotForcePull != tc.wantForcePull {
				t.Errorf("forcePull = %v, want %v", gotForcePull, tc.wantForcePull)
			}
			if gotRemoveOrphans != tc.wantRemoveOrphans {
				t.Errorf("removeOrphans = %v, want %v", gotRemoveOrphans, tc.wantRemoveOrphans)
			}
		})
	}
}

func TestWithDeployTimeout(t *testing.T) {
	t.Run("GlobalDefaultAppliedWhenUnset", func(t *testing.T) {
		stack := newFlagsTestStack(t)
		ctx := context.Background()
		derived, cancel := withDeployTimeout(ctx, stack)
		defer cancel()
		deadline, ok := derived.Deadline()
		if !ok {
			t.Fatal("expected a deadline (global default) when deploy_timeout_seconds is unset")
		}
		if remaining := time.Until(deadline); remaining <= 0 || remaining > config.GetDeployTimeout() {
			t.Errorf("deadline out of expected range: %v", remaining)
		}
	})

	t.Run("TimeoutAppliedWhenPositive", func(t *testing.T) {
		stack := newFlagsTestStack(t)
		stack.Set("deploy_timeout_seconds", 30)
		ctx := context.Background()
		derived, cancel := withDeployTimeout(ctx, stack)
		defer cancel()
		deadline, ok := derived.Deadline()
		if !ok {
			t.Fatal("expected a deadline when deploy_timeout_seconds > 0")
		}
		if remaining := time.Until(deadline); remaining <= 0 || remaining > 30*time.Second {
			t.Errorf("deadline out of expected range: %v", remaining)
		}
	})

	t.Run("GlobalDefaultAppliedWhenZeroOrNegative", func(t *testing.T) {
		stack := newFlagsTestStack(t)
		stack.Set("deploy_timeout_seconds", 0)
		ctx := context.Background()
		derived, cancel := withDeployTimeout(ctx, stack)
		defer cancel()
		if _, ok := derived.Deadline(); !ok {
			t.Error("expected a deadline (global default) when deploy_timeout_seconds is 0")
		}
	})
}
