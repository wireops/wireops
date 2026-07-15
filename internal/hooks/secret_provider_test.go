package hooks

import "testing"

func TestValidateSecretProviderAcceptsImplementedProviders(t *testing.T) {
	for _, provider := range []string{"internal", "vault", "infisical"} {
		if err := validateSecretProvider(provider); err != nil {
			t.Errorf("validateSecretProvider(%q) = %v, want nil", provider, err)
		}
	}
}

func TestValidateSecretProviderRejectsUnknownProvider(t *testing.T) {
	if err := validateSecretProvider("does-not-exist"); err == nil {
		t.Fatal("validateSecretProvider(\"does-not-exist\") = nil, want error")
	}
}

func TestIsInternalSecretProvider(t *testing.T) {
	for _, provider := range []string{"", "internal"} {
		if !isInternalSecretProvider(provider) {
			t.Errorf("isInternalSecretProvider(%q) = false, want true", provider)
		}
	}
	for _, provider := range []string{"vault", "infisical"} {
		if isInternalSecretProvider(provider) {
			t.Errorf("isInternalSecretProvider(%q) = true, want false", provider)
		}
	}
}
