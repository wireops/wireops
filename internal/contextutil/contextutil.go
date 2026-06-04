package contextutil

import "context"

type contextKey string

const userIDKey contextKey = "userID"

// WithUserID returns a new context with the userID value.
func WithUserID(parent context.Context, userID string) context.Context {
	return context.WithValue(parent, userIDKey, userID)
}

// GetUserID retrieves the userID value from the context, if present.
func GetUserID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(userIDKey).(string); ok {
		return v
	}
	return ""
}
