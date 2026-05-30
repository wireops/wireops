// Package tls — client.go
// Configures outbound TLS for the wireops worker process.
// The worker only ever acts as a TLS client (connecting to the server);
// it never terminates TLS itself.
package tls

import (
	"crypto/tls"
	"os"
)

// BuildClientTLSConfig returns a *tls.Config for outbound connections (HTTP
// registration call and WebSocket dialer), or nil when the default
// system-trust-store behaviour is sufficient.
//
// Environment variable:
//   - WORKER_TLS_SKIP_VERIFY — set to "true" to accept self-signed or
//     otherwise untrusted certificates from the server. Required when the
//     server runs with an auto-generated self-signed certificate.
//     Never enable in production with publicly trusted certificates.
func BuildClientTLSConfig() *tls.Config {
	if os.Getenv("WORKER_TLS_SKIP_VERIFY") != "true" {
		return nil
	}
	return &tls.Config{InsecureSkipVerify: true} //nolint:gosec // intentional opt-in
}
