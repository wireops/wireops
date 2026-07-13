package hooks

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/pocketbase/pocketbase/tools/auth"
	"github.com/pocketbase/pocketbase/tools/router"
	"github.com/pocketbase/pocketbase/tools/types"

	"github.com/wireops/wireops/internal/crypto"
)

func TestValidateRepositoryKeyAssignment(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("new test app: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	keys := core.NewBaseCollection("repository_keys")
	keys.Fields.Add(&core.TextField{Name: "auth_type"})
	if err := app.Save(keys); err != nil {
		t.Fatalf("save keys collection: %v", err)
	}
	repositories := core.NewBaseCollection("repositories")
	repositories.Fields.Add(&core.TextField{Name: "git_url"})
	repositories.Fields.Add(&core.RelationField{Name: "repository_key", CollectionId: keys.Id, MaxSelect: 1})
	if err := app.Save(repositories); err != nil {
		t.Fatalf("save repositories collection: %v", err)
	}

	basicKey := core.NewRecord(keys)
	basicKey.Set("auth_type", "basic")
	if err := app.Save(basicKey); err != nil {
		t.Fatalf("save basic key: %v", err)
	}
	sshKey := core.NewRecord(keys)
	sshKey.Set("auth_type", "ssh_key")
	if err := app.Save(sshKey); err != nil {
		t.Fatalf("save ssh key: %v", err)
	}

	tests := []struct {
		name    string
		gitURL  string
		keyID   string
		wantErr bool
	}{
		{name: "public HTTP", gitURL: "https://example.com/repo.git"},
		{name: "private HTTP", gitURL: "https://example.com/repo.git", keyID: basicKey.Id},
		{name: "HTTP rejects SSH key", gitURL: "https://example.com/repo.git", keyID: sshKey.Id, wantErr: true},
		{name: "SSH requires key", gitURL: "git@example.com:repo.git", wantErr: true},
		{name: "SSH accepts SSH key", gitURL: "git@example.com:repo.git", keyID: sshKey.Id},
		{name: "SSH rejects basic key", gitURL: "git@example.com:repo.git", keyID: basicKey.Id, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository := core.NewRecord(repositories)
			repository.Set("git_url", tt.gitURL)
			repository.Set("repository_key", tt.keyID)
			err := validateRepositoryKeyAssignment(app, repository)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateRepositoryKeyAssignment() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRepositoryKeyTypeCannotChangeAfterCreate(t *testing.T) {
	t.Setenv("SECRET_KEY", "12345678901234567890123456789012")

	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("new test app: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	keys := core.NewBaseCollection("repository_keys")
	keys.Fields.Add(&core.TextField{Name: "name"})
	keys.Fields.Add(&core.TextField{Name: "auth_type"})
	keys.Fields.Add(&core.TextField{Name: "ssh_private_key"})
	keys.Fields.Add(&core.TextField{Name: "ssh_passphrase"})
	keys.Fields.Add(&core.TextField{Name: "ssh_known_host"})
	keys.Fields.Add(&core.TextField{Name: "git_username"})
	keys.Fields.Add(&core.TextField{Name: "git_password"})
	if err := app.Save(keys); err != nil {
		t.Fatalf("save keys collection: %v", err)
	}
	repositories := core.NewBaseCollection("repositories")
	repositories.Fields.Add(&core.RelationField{Name: "repository_key", CollectionId: keys.Id, MaxSelect: 1})
	if err := app.Save(repositories); err != nil {
		t.Fatalf("save repositories collection: %v", err)
	}

	Register(app, nil, nil)

	key := core.NewRecord(keys)
	key.Set("name", "Production")
	key.Set("auth_type", "basic")
	if err := app.Save(key); err != nil {
		t.Fatalf("save key: %v", err)
	}

	key.Set("name", "Production API")
	if err := app.Save(key); err != nil {
		t.Fatalf("rename key: %v", err)
	}

	key.Set("auth_type", "ssh_key")
	err = app.Save(key)
	if err == nil {
		t.Fatal("expected auth_type change to be rejected")
	}
	if !strings.Contains(err.Error(), "Repository key type cannot be changed after creation") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnvSecretMaskedUpdatePreservesEncryptedValue(t *testing.T) {
	t.Setenv("SECRET_KEY", "12345678901234567890123456789012")

	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("new test app: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	envVars := core.NewBaseCollection("stack_env_vars")
	envVars.Fields.Add(&core.TextField{Name: "key"})
	envVars.Fields.Add(&core.TextField{Name: "value"})
	envVars.Fields.Add(&core.BoolField{Name: "secret"})
	envVars.Fields.Add(&core.TextField{Name: "secret_provider"})
	if err := app.Save(envVars); err != nil {
		t.Fatalf("save stack_env_vars collection: %v", err)
	}

	Register(app, nil, nil)

	rec := core.NewRecord(envVars)
	rec.Set("key", "TOKEN")
	rec.Set("value", "initial-secret")
	rec.Set("secret", true)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save secret env var: %v", err)
	}

	saved, err := app.FindRecordById("stack_env_vars", rec.Id)
	if err != nil {
		t.Fatalf("find saved secret env var: %v", err)
	}
	encryptedValue := saved.GetString("value")
	if encryptedValue == "" || encryptedValue == "initial-secret" || !crypto.IsEncrypted(encryptedValue) {
		t.Fatalf("secret value was not encrypted: %q", encryptedValue)
	}

	saved.Set("key", "RENAMED_TOKEN")
	saved.Set("value", "")
	if err := app.Save(saved); err != nil {
		t.Fatalf("save masked secret update: %v", err)
	}

	updated, err := app.FindRecordById("stack_env_vars", rec.Id)
	if err != nil {
		t.Fatalf("find updated secret env var: %v", err)
	}
	if got := updated.GetString("value"); got != encryptedValue {
		t.Fatalf("secret value changed on masked update: got %q, want %q", got, encryptedValue)
	}
}

// TestEnvSecretExternalProviderValueNotEncrypted guards against a
// regression where prepareEnvSecretRecord unconditionally AES-GCM encrypted
// "value" for any secret=true record. Only the "internal" provider decrypts
// it at Resolve time — vault/infisical expect their raw reference string
// ("mount/data/path#field", "project/env#NAME") stored verbatim, so
// encrypting it here would make Resolve() fail trying to parse ciphertext
// as a reference.
func TestEnvSecretExternalProviderValueNotEncrypted(t *testing.T) {
	t.Setenv("SECRET_KEY", "12345678901234567890123456789012")

	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("new test app: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	envVars := core.NewBaseCollection("stack_env_vars")
	envVars.Fields.Add(&core.TextField{Name: "key"})
	envVars.Fields.Add(&core.TextField{Name: "value"})
	envVars.Fields.Add(&core.BoolField{Name: "secret"})
	envVars.Fields.Add(&core.TextField{Name: "secret_provider"})
	if err := app.Save(envVars); err != nil {
		t.Fatalf("save stack_env_vars collection: %v", err)
	}

	Register(app, nil, nil)

	rec := core.NewRecord(envVars)
	rec.Set("key", "DB_PASS")
	rec.Set("value", "secret/data/myapp#DB_PASS")
	rec.Set("secret", true)
	rec.Set("secret_provider", "vault")
	if err := app.Save(rec); err != nil {
		t.Fatalf("save vault-provider secret env var: %v", err)
	}

	saved, err := app.FindRecordById("stack_env_vars", rec.Id)
	if err != nil {
		t.Fatalf("find saved secret env var: %v", err)
	}
	if got := saved.GetString("value"); got != "secret/data/myapp#DB_PASS" {
		t.Fatalf("vault reference was mangled: got %q, want raw reference", got)
	}
	if crypto.IsEncrypted(saved.GetString("value")) {
		t.Fatal("vault reference must not be AES-GCM encrypted at rest")
	}

	// The API-facing enrich step must not blank an external provider's
	// value either — it's a locator, not the secret, so the frontend needs
	// to see and edit it like a normal field. Internal-provider secrets
	// still get blanked (asserted below).
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	e := &core.RequestEvent{App: app, Event: router.Event{Response: httptest.NewRecorder(), Request: req}}
	if err := apis.EnrichRecord(e, saved); err != nil {
		t.Fatalf("enrich vault-provider record: %v", err)
	}
	if got := saved.GetString("value"); got != "secret/data/myapp#DB_PASS" {
		t.Fatalf("vault reference was blanked by enrich: got %q", got)
	}

	internalRec := core.NewRecord(envVars)
	internalRec.Set("key", "INTERNAL_TOKEN")
	internalRec.Set("value", "s3cr3t")
	internalRec.Set("secret", true)
	internalRec.Set("secret_provider", "internal")
	if err := app.Save(internalRec); err != nil {
		t.Fatalf("save internal-provider secret env var: %v", err)
	}
	if err := apis.EnrichRecord(e, internalRec); err != nil {
		t.Fatalf("enrich internal-provider record: %v", err)
	}
	if got := internalRec.GetString("value"); got != "" {
		t.Fatalf("internal-provider secret was not blanked by enrich: got %q", got)
	}
}

func TestEnvVarKeyIsTrimmedAndBlankRejected(t *testing.T) {
	t.Setenv("SECRET_KEY", "12345678901234567890123456789012")

	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("new test app: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	envVars := core.NewBaseCollection("stack_env_vars")
	envVars.Fields.Add(&core.TextField{Name: "key"})
	envVars.Fields.Add(&core.TextField{Name: "value"})
	envVars.Fields.Add(&core.BoolField{Name: "secret"})
	envVars.Fields.Add(&core.TextField{Name: "secret_provider"})
	if err := app.Save(envVars); err != nil {
		t.Fatalf("save stack_env_vars collection: %v", err)
	}

	Register(app, nil, nil)

	rec := core.NewRecord(envVars)
	rec.Set("key", "  API_KEY  ")
	rec.Set("value", "plain-value")
	if err := app.Save(rec); err != nil {
		t.Fatalf("save env var with padded key: %v", err)
	}
	saved, err := app.FindRecordById("stack_env_vars", rec.Id)
	if err != nil {
		t.Fatalf("find saved env var: %v", err)
	}
	if got := saved.GetString("key"); got != "API_KEY" {
		t.Fatalf("env var key = %q, want API_KEY", got)
	}

	blank := core.NewRecord(envVars)
	blank.Set("key", "   ")
	blank.Set("value", "plain-value")
	if err := app.Save(blank); err == nil {
		t.Fatal("expected blank env var key to be rejected")
	}
}

func TestIsSSHGitURL(t *testing.T) {
	tests := []struct {
		name   string
		gitURL string
		want   bool
	}{
		{name: "scp style ssh", gitURL: "git@github.com:wireops/wireops.git", want: true},
		{name: "ssh scheme", gitURL: "ssh://git@github.com/wireops/wireops.git", want: true},
		{name: "https scheme", gitURL: "https://github.com/wireops/wireops.git", want: false},
		{name: "http scheme", gitURL: "http://example.com/repo.git", want: false},
		{name: "local path", gitURL: "/tmp/repo.git", want: false},
		{name: "blank", gitURL: "   ", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSSHGitURL(tt.gitURL); got != tt.want {
				t.Fatalf("isSSHGitURL(%q) = %v, want %v", tt.gitURL, got, tt.want)
			}
		})
	}
}

func TestMaskEmailForLog(t *testing.T) {
	tests := []struct {
		name     string
		email    string
		expected string
	}{
		{
			name:     "normal email",
			email:    "user@example.com",
			expected: "u***@example.com",
		},
		{
			name:     "single char local part",
			email:    "a@domain.org",
			expected: "a***@domain.org",
		},
		{
			name:     "empty email",
			email:    "",
			expected: "[empty]",
		},
		{
			name:     "no @ sign",
			email:    "invalidemail",
			expected: "[invalid]",
		},
		{
			name:     "long local part",
			email:    "verylongemail@domain.org",
			expected: "v***@domain.org",
		},
		{
			name:     "subdomain in domain",
			email:    "admin@mail.example.com",
			expected: "a***@mail.example.com",
		},
		{
			name:     "numbers in email",
			email:    "user123@test.io",
			expected: "u***@test.io",
		},
		{
			name:     "plus addressing",
			email:    "user+tag@example.com",
			expected: "u***@example.com",
		},
		{
			name:     "dots in local part",
			email:    "first.last@company.com",
			expected: "f***@company.com",
		},
		{
			name:     "multiple @ signs (invalid but handled)",
			email:    "bad@@example.com",
			expected: "b***@@example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := maskEmailForLog(tt.email)
			if result != tt.expected {
				t.Errorf("maskEmailForLog(%q) = %q, want %q", tt.email, result, tt.expected)
			}
		})
	}
}

func TestMaskEmailForLog_ConsistentOutput(t *testing.T) {
	// Ensure the function produces consistent output for the same input
	email := "consistent@test.com"
	expected := "c***@test.com"

	for i := 0; i < 100; i++ {
		result := maskEmailForLog(email)
		if result != expected {
			t.Errorf("Inconsistent output on iteration %d: got %q, want %q", i, result, expected)
		}
	}
}

func TestHandleSSOAuthRequest(t *testing.T) {
	scenarios := []struct {
		name                 string
		requireEmailVerified bool
		oauth2User           *auth.AuthUser
		record               *core.Record
		nextErr              error
		expectError          bool
		expectConsumeFalse   bool
	}{
		{
			name:                 "nil oauth2user proceeds to next",
			requireEmailVerified: true,
			oauth2User:           nil,
			record:               nil,
			expectError:          false,
		},
		{
			name:                 "valid user with verified email",
			requireEmailVerified: true,
			oauth2User: &auth.AuthUser{
				Email:   "test@example.com",
				RawUser: map[string]any{"email_verified": true, "groups": []any{"wireops-operators"}},
			},
			record:             core.NewRecord(core.NewBaseCollection("sso_users")),
			expectError:        false,
			expectConsumeFalse: true,
		},
		{
			name:                 "unverified email rejected when required",
			requireEmailVerified: true,
			oauth2User: &auth.AuthUser{
				Email:   "test@example.com",
				RawUser: map[string]any{"email_verified": false},
			},
			record:      core.NewRecord(core.NewBaseCollection("sso_users")),
			expectError: true,
		},
		{
			name:                 "unverified email allowed when not required",
			requireEmailVerified: false,
			oauth2User: &auth.AuthUser{
				Email:   "test@example.com",
				RawUser: map[string]any{"email_verified": false, "groups": []any{"wireops-operators"}},
			},
			record:             core.NewRecord(core.NewBaseCollection("sso_users")),
			expectError:        false,
			expectConsumeFalse: true,
		},
		{
			name:                 "recovers missing email from raw claims",
			requireEmailVerified: false,
			oauth2User: &auth.AuthUser{
				Email:   "",
				RawUser: map[string]any{"email": "recovered@example.com", "groups": []any{"wireops-operators"}},
			},
			record:             core.NewRecord(core.NewBaseCollection("sso_users")),
			expectError:        false,
			expectConsumeFalse: true,
		},
		{
			name:                 "fails if no email is found at all",
			requireEmailVerified: false,
			oauth2User: &auth.AuthUser{
				Email:   "",
				RawUser: map[string]any{},
			},
			record:      core.NewRecord(core.NewBaseCollection("sso_users")),
			expectError: true,
		},
		{
			name:                 "does not reset fields when downstream auth fails",
			requireEmailVerified: true,
			oauth2User: &auth.AuthUser{
				Email:   "test@example.com",
				RawUser: map[string]any{"email_verified": true, "groups": []any{"wireops-operators"}},
			},
			record:      core.NewRecord(core.NewBaseCollection("sso_users")),
			nextErr:     errors.New("downstream auth failed"),
			expectError: true,
		},
	}

	for _, tt := range scenarios {
		t.Run(tt.name, func(t *testing.T) {
			app, err := tests.NewTestApp()
			if err != nil {
				t.Fatalf("new test app: %v", err)
			}
			t.Cleanup(func() { app.Cleanup() })
			if tt.oauth2User != nil && tt.oauth2User.RawUser["groups"] != nil {
				createSSOGroupRoleMapping(t, app, "wireops-operators", "operator")
			}

			handler := HandleSSOAuthRequest(tt.requireEmailVerified)
			app.OnRecordAuthWithOAuth2Request("sso_users").BindFunc(handler)

			if tt.record != nil {
				tt.record.Set("elevate_consumed", true)
				tt.record.Set("elevate_consumed_at", types.NowDateTime())
			}

			event := &core.RecordAuthWithOAuth2RequestEvent{
				RequestEvent: &core.RequestEvent{App: app},
				OAuth2User:   tt.oauth2User,
				Record:       tt.record,
			}
			event.Collection = core.NewBaseCollection("sso_users")

			err = app.OnRecordAuthWithOAuth2Request().Trigger(event, func(e *core.RecordAuthWithOAuth2RequestEvent) error {
				return tt.nextErr
			})

			if tt.expectError && err == nil {
				t.Errorf("expected error but got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if tt.expectConsumeFalse && tt.record != nil {
				if tt.record.GetBool("elevate_consumed") != false {
					t.Errorf("expected elevate_consumed to be false, got true")
				}
				if tt.record.GetString("elevate_consumed_at") != "" {
					t.Errorf("expected elevate_consumed_at to be empty")
				}
			}
			if !tt.expectConsumeFalse && tt.record != nil && tt.nextErr != nil {
				if tt.record.GetBool("elevate_consumed") != true {
					t.Errorf("expected elevate_consumed to remain true when auth fails")
				}
				if tt.record.GetString("elevate_consumed_at") == "" {
					t.Errorf("expected elevate_consumed_at to remain set when auth fails")
				}
			}
			if tt.oauth2User != nil && tt.name == "recovers missing email from raw claims" && tt.oauth2User.Email != "recovered@example.com" {
				t.Errorf("expected email to be recovered, got %q", tt.oauth2User.Email)
			}
		})
	}
}

func TestHandleSSOAuthRequestPersistsElevateResetAfterSuccess(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("new test app: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })
	createSSOGroupRoleMapping(t, app, "wireops-operators", "operator")

	col := core.NewBaseCollection("sso_users")
	col.Fields.Add(&core.BoolField{Name: "elevate_consumed"})
	col.Fields.Add(&core.DateField{Name: "elevate_consumed_at"})
	if err := app.Save(col); err != nil {
		t.Fatalf("save collection: %v", err)
	}

	record := core.NewRecord(col)
	record.Set("elevate_consumed", true)
	record.Set("elevate_consumed_at", types.NowDateTime())
	if err := app.Save(record); err != nil {
		t.Fatalf("save record: %v", err)
	}

	app.OnRecordAuthWithOAuth2Request("sso_users").BindFunc(HandleSSOAuthRequest(true))

	event := &core.RecordAuthWithOAuth2RequestEvent{
		RequestEvent: &core.RequestEvent{App: app},
		OAuth2User: &auth.AuthUser{
			Email:   "test@example.com",
			RawUser: map[string]any{"email_verified": true, "groups": []any{"wireops-operators"}},
		},
		Record: record,
	}
	event.Collection = col

	if err := app.OnRecordAuthWithOAuth2Request().Trigger(event, func(e *core.RecordAuthWithOAuth2RequestEvent) error {
		return nil
	}); err != nil {
		t.Fatalf("trigger auth hook: %v", err)
	}

	reloaded, err := app.FindRecordById("sso_users", record.Id)
	if err != nil {
		t.Fatalf("reload record: %v", err)
	}
	if reloaded.GetBool("elevate_consumed") {
		t.Fatalf("expected elevate_consumed to be reset in db")
	}
	if reloaded.GetString("elevate_consumed_at") != "" {
		t.Fatalf("expected elevate_consumed_at to be NULL in db, got %q", reloaded.GetString("elevate_consumed_at"))
	}
}

func createSSOGroupRoleMapping(t *testing.T, app core.App, group, role string) {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("sso_group_roles")
	if err != nil {
		col = core.NewBaseCollection("sso_group_roles")
		col.Fields.Add(&core.TextField{Name: "group", Required: true})
		col.Fields.Add(&core.SelectField{
			Name:      "role",
			Required:  true,
			MaxSelect: 1,
			Values:    []string{"viewer", "operator", "admin"},
		})
		if err := app.Save(col); err != nil {
			t.Fatalf("create sso_group_roles fixture: %v", err)
		}
	}
	rec := core.NewRecord(col)
	rec.Set("group", group)
	rec.Set("role", role)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save sso group role mapping: %v", err)
	}
}
