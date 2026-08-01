package routes

import "testing"

// TestS3IntegrationConfigKeys covers the "s3" Storage Backend integration's
// required-field validation: bucket/region/access_key/secret must all be
// present to enable it. Encryption-at-rest for its "secret" field is covered
// by internal/integrations.TestStoreRoundTripsEveryEncryptedField now that
// Store owns that logic.
func TestS3IntegrationConfigKeys(t *testing.T) {
	if err := validateRequiredIntegrationConfig("s3", map[string]interface{}{"bucket": "b", "region": "r", "access_key": "ak"}); err == nil {
		t.Fatal("expected error when s3 secret is missing")
	}
	if err := validateRequiredIntegrationConfig("s3", map[string]interface{}{"bucket": "b", "region": "r", "access_key": "ak", "secret": "sk"}); err != nil {
		t.Fatalf("expected no error with all required s3 fields, got %v", err)
	}
}

func TestValidateRequiredIntegrationConfig(t *testing.T) {
	if err := validateRequiredIntegrationConfig("vault", map[string]interface{}{"address": "x"}); err == nil {
		t.Fatal("expected error when vault token is missing")
	}
	if err := validateRequiredIntegrationConfig("vault", map[string]interface{}{"address": "x", "token": "y"}); err != nil {
		t.Fatalf("expected no error with both required vault fields, got %v", err)
	}
	if err := validateRequiredIntegrationConfig("infisical", map[string]interface{}{"client_id": "x"}); err == nil {
		t.Fatal("expected error when infisical client_secret is missing")
	}
	if err := validateRequiredIntegrationConfig("webhook", map[string]interface{}{}); err != nil {
		t.Fatalf("webhook has no required keys, expected nil, got %v", err)
	}

	// allowed_mount / allowed_project_id are optional scoping fields (empty =
	// unrestricted) — enabling vault/infisical without them must still pass.
	if err := validateRequiredIntegrationConfig("vault", map[string]interface{}{"address": "x", "token": "y"}); err != nil {
		t.Fatalf("vault without allowed_mount should still validate, got %v", err)
	}
	if err := validateRequiredIntegrationConfig("infisical", map[string]interface{}{"client_id": "x", "client_secret": "y"}); err != nil {
		t.Fatalf("infisical without allowed_project_id should still validate, got %v", err)
	}
}

// TestScopingFieldsExcludedFromSensitiveAndRequiredKeys documents that
// allowed_mount/allowed_project_id are plain, optional config — never masked,
// never encrypted, never required.
func TestScopingFieldsExcludedFromSensitiveAndRequiredKeys(t *testing.T) {
	cases := []struct {
		slug          string
		disallowedKey string
	}{
		{"vault", "allowed_mount"},
		{"infisical", "allowed_project_id"},
	}

	for _, tc := range cases {
		t.Run(tc.slug, func(t *testing.T) {
			d := integrationDescriptor(tc.slug)
			if len(d.Fields) == 0 {
				t.Fatalf("expected %q to be a registered descriptor with fields", tc.slug)
			}

			keyLists := map[string][]string{
				"sensitive": d.SensitiveKeys(),
				"encrypted": d.EncryptedKeys(),
				"required":  d.RequiredKeys(),
			}
			for kind, keys := range keyLists {
				for _, key := range keys {
					if key == tc.disallowedKey {
						t.Fatalf("%s must not be %s", tc.disallowedKey, kind)
					}
				}
			}
		})
	}
}
