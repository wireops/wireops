package stackdef

import (
	"errors"
	"testing"
)

func TestWithBypass(t *testing.T) {
	const stackID = "stack123"

	t.Run("NotBypassedByDefault", func(t *testing.T) {
		if IsBypassed(stackID) {
			t.Fatal("expected no bypass before WithBypass runs")
		}
	})

	t.Run("ActiveInsideAndClearedAfter", func(t *testing.T) {
		var seen bool
		if err := WithBypass(stackID, func() error {
			seen = IsBypassed(stackID)
			return nil
		}); err != nil {
			t.Fatalf("WithBypass returned %v", err)
		}
		if !seen {
			t.Fatal("expected the bypass to be active inside fn")
		}
		if IsBypassed(stackID) {
			t.Fatal("expected the bypass to be cleared after WithBypass returns")
		}
	})

	t.Run("ClearedAndPropagatedWhenFnFails", func(t *testing.T) {
		want := errors.New("save failed")
		if err := WithBypass(stackID, func() error { return want }); !errors.Is(err, want) {
			t.Fatalf("WithBypass returned %v, want %v", err, want)
		}
		if IsBypassed(stackID) {
			t.Fatal("expected the bypass to be cleared after a failing fn")
		}
	})

	t.Run("ScopedToOneStack", func(t *testing.T) {
		if err := WithBypass(stackID, func() error {
			if IsBypassed("other") {
				t.Error("expected the bypass not to leak to another stack")
			}
			return nil
		}); err != nil {
			t.Fatalf("WithBypass returned %v", err)
		}
	})
}
