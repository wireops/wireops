package routes

import (
	"testing"

	"github.com/wireops/wireops/internal/constants"
)

func TestValidatePublicHostBlocksPrivateAndLoopback(t *testing.T) {
	t.Setenv(constants.EnvAllowedPrivateIPRanges, "")

	cases := []string{
		"127.0.0.1",
		"127.0.0.1:5000",
		"localhost",
		"10.0.0.5",
		"172.16.0.1",
		"192.168.1.1",
		"0.0.0.0",
		"169.254.169.254", // cloud metadata endpoint
	}
	for _, host := range cases {
		if err := validatePublicHost(host); err == nil {
			t.Errorf("validatePublicHost(%q) = nil, want an error", host)
		}
	}
}

func TestValidatePublicHostAllowsPublicAddresses(t *testing.T) {
	t.Setenv(constants.EnvAllowedPrivateIPRanges, "")

	// Public IP literals only — no DNS resolution, so this test doesn't
	// depend on network access.
	cases := []string{"8.8.8.8", "1.1.1.1:443"}
	for _, host := range cases {
		if err := validatePublicHost(host); err != nil {
			t.Errorf("validatePublicHost(%q) = %v, want nil", host, err)
		}
	}
}

func TestValidatePublicHostRespectsAllowlist(t *testing.T) {
	t.Setenv(constants.EnvAllowedPrivateIPRanges, "10.0.0.0/8,127.0.0.1")

	if err := validatePublicHost("10.1.2.3"); err != nil {
		t.Errorf("expected 10.1.2.3 to be allowed by CIDR, got %v", err)
	}
	if err := validatePublicHost("127.0.0.1:5000"); err != nil {
		t.Errorf("expected 127.0.0.1 to be allowed by exact match, got %v", err)
	}
	if err := validatePublicHost("192.168.1.1"); err == nil {
		t.Error("expected 192.168.1.1 to still be blocked (not in allowlist)")
	}
}
