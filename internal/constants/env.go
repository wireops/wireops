package constants

// Environment variable names read by the registry secrets feature
// (internal/registry, internal/routes/registry_credentials.go,
// internal/routes/ssrf_guard.go) — named here instead of inline string
// literals so the two call sites that read ALLOWED_PRIVATE_IP_RANGES (the
// git key-scan route and the registry SSRF guard) can't drift apart.
const (
	// EnvSecretKey is the AES-GCM key (see internal/crypto) that encrypts
	// registry_credentials.password at rest, same as repository_keys'
	// git_password/ssh_private_key.
	EnvSecretKey = "SECRET_KEY"

	// EnvAllowedPrivateIPRanges is a comma-separated list of CIDRs/IPs
	// operators can allowlist to opt a legitimate internal registry (or git
	// host, for key-scan) back into requests that would otherwise be
	// blocked as SSRF into the server's own private network.
	EnvAllowedPrivateIPRanges = "ALLOWED_PRIVATE_IP_RANGES"
)
