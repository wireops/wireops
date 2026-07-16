// Package auth carries the pass-through wireops API key from the inbound
// MCP HTTP request into tool handlers. The MCP process never stores or
// bakes in a credential of its own.
package auth

import "context"

type contextKey struct{}

var apiKeyContextKey = contextKey{}

// WithAPIKey returns a context carrying the caller-supplied wireops API key.
func WithAPIKey(ctx context.Context, apiKey string) context.Context {
	return context.WithValue(ctx, apiKeyContextKey, apiKey)
}

// APIKeyFromContext returns the wireops API key stashed by WithAPIKey, if any.
func APIKeyFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(apiKeyContextKey).(string)
	return v, ok && v != ""
}
