package contextutil

import (
	"context"
	"testing"
)

func TestContextUtil(t *testing.T) {
	ctx := context.Background()

	// 1. GetUserID on nil context should return empty string
	if got := GetUserID(nil); got != "" {
		t.Errorf("GetUserID(nil) = %q; want empty string", got)
	}

	// 2. GetUserID when value is not set should return empty string
	if got := GetUserID(ctx); got != "" {
		t.Errorf("GetUserID(ctx) without key = %q; want empty string", got)
	}

	// 3. WithUserID / GetUserID flow
	testID := "user-123"
	withUser := WithUserID(ctx, testID)
	if got := GetUserID(withUser); got != testID {
		t.Errorf("GetUserID(withUser) = %q; want %q", got, testID)
	}

	// 4. Verify context key collision is avoided by verifying standard string key doesn't retrieve it
	strKeyCtx := context.WithValue(ctx, "userID", testID)
	if got := GetUserID(strKeyCtx); got != "" {
		t.Errorf("GetUserID(strKeyCtx) with string literal key = %q; want empty string (collision check)", got)
	}
}
