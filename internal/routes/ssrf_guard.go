package routes

import (
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/wireops/wireops/internal/constants"
)

// validatePublicHost resolves host (optionally "host:port") to its IP
// addresses and rejects any that are loopback/private/link-local/
// unspecified — guarding routes that take a user-supplied hostname and use
// it to make an outbound request (registry connection tests, git host key
// scanning) against SSRF into the server's own private network. Operators
// running a legitimate internal registry/git server can opt back in per-IP
// or per-CIDR via ALLOWED_PRIVATE_IP_RANGES (comma-separated), the same
// escape hatch already used by the git credentials key-scan route.
func validatePublicHost(host string) error {
	hostOnly := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		hostOnly = h
	}

	var ips []net.IP
	if ip := net.ParseIP(hostOnly); ip != nil {
		ips = append(ips, ip)
	} else {
		resolved, err := net.LookupIP(hostOnly)
		if err != nil {
			return fmt.Errorf("failed to resolve host: %w", err)
		}
		ips = resolved
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
			return fmt.Errorf("connecting to private or loopback addresses is not allowed")
		}
	}
	return nil
}
