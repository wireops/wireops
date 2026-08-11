package routes

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/wireops/wireops/internal/constants"
)

// dnsResolveTimeout bounds how long host resolution may take, derived from
// (and capped below) the caller's request context so a slow/unresponsive
// resolver can't hang the request indefinitely.
const dnsResolveTimeout = 5 * time.Second

// validatePublicHost resolves host (optionally "host:port") to its IP
// addresses and rejects any that are loopback/private/link-local/
// unspecified — guarding routes that take a user-supplied hostname and use
// it to make an outbound request (registry connection tests, git host key
// scanning) against SSRF into the server's own private network. Operators
// running a legitimate internal registry/git server can opt back in per-IP
// or per-CIDR via ALLOWED_PRIVATE_IP_RANGES (comma-separated), the same
// escape hatch already used by the git credentials key-scan route.
func validatePublicHost(ctx context.Context, host string) error {
	_, err := resolveValidatedIP(ctx, host)
	return err
}

// resolveValidatedIP resolves host (optionally "host:port") and returns a
// single IP address that passed the private/loopback/allowlist checks below.
// Callers that go on to make an outbound connection should dial this literal
// IP rather than re-resolving the hostname, so a DNS answer that changes
// between the check and the dial (DNS rebinding) can't bypass the guard.
func resolveValidatedIP(ctx context.Context, host string) (net.IP, error) {
	hostOnly := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		hostOnly = h
	}

	var ips []net.IP
	if ip := net.ParseIP(hostOnly); ip != nil {
		ips = append(ips, ip)
	} else {
		resolveCtx, cancel := context.WithTimeout(ctx, dnsResolveTimeout)
		defer cancel()
		resolved, err := net.DefaultResolver.LookupIPAddr(resolveCtx, hostOnly)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve host: %w", err)
		}
		for _, addr := range resolved {
			ips = append(ips, addr.IP)
		}
	}

	allowedRanges := os.Getenv(constants.EnvAllowedPrivateIPRanges)
	isIPAllowed := func(ip net.IP) bool {
		if allowedRanges == "" {
			return false
		}
		for _, part := range strings.Split(allowedRanges, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if _, ipNet, err := net.ParseCIDR(part); err == nil {
				if ipNet.Contains(ip) {
					return true
				}
			} else if parsedIP := net.ParseIP(part); parsedIP != nil && parsedIP.Equal(ip) {
				return true
			}
		}
		return false
	}

	for _, ip := range ips {
		if isIPAllowed(ip) {
			continue
		}
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalMulticast() || ip.IsLinkLocalUnicast() {
			return nil, fmt.Errorf("connecting to private or loopback addresses is not allowed")
		}
	}
	return ips[0], nil
}
