package envvars

import (
	"strings"
	"testing"
)

func TestCheckStackSecretBackendsNoSecrets(t *testing.T) {
	app := newEnvVarsTestApp(t)
	stack := mustCreateEnvRecord(t, app, "stacks", map[string]any{"name": "stack"})
	mustCreateEnvRecord(t, app, "stack_env_vars", map[string]any{
		"stack": stack.Id,
		"key":   "PLAIN",
		"value": "value",
	})

	if err := CheckStackSecretBackends(app, stack.Id); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestCheckStackSecretBackendsInternalProviderAlwaysPasses(t *testing.T) {
	app := newEnvVarsTestApp(t)
	stack := mustCreateEnvRecord(t, app, "stacks", map[string]any{"name": "stack"})
	mustCreateEnvRecord(t, app, "stack_env_vars", map[string]any{
		"stack":           stack.Id,
		"key":             "TOKEN",
		"value":           "ciphertext",
		"secret":          true,
		"secret_provider": "internal",
	})

	if err := CheckStackSecretBackends(app, stack.Id); err != nil {
		t.Fatalf("expected nil for internal provider, got %v", err)
	}
}

func TestCheckStackSecretBackendsVaultEnabled(t *testing.T) {
	app := newEnvVarsTestApp(t)
	mustCreateEnvRecord(t, app, "integrations", map[string]any{"slug": "vault", "enabled": true})
	stack := mustCreateEnvRecord(t, app, "stacks", map[string]any{"name": "stack"})
	mustCreateEnvRecord(t, app, "stack_env_vars", map[string]any{
		"stack":           stack.Id,
		"key":             "DB_PASS",
		"value":           "secret/data/myapp#DB_PASS",
		"secret":          true,
		"secret_provider": "vault",
	})

	if err := CheckStackSecretBackends(app, stack.Id); err != nil {
		t.Fatalf("expected nil for enabled vault backend, got %v", err)
	}
}

func TestCheckStackSecretBackendsVaultDisabled(t *testing.T) {
	app := newEnvVarsTestApp(t)
	mustCreateEnvRecord(t, app, "integrations", map[string]any{"slug": "vault", "enabled": false})
	stack := mustCreateEnvRecord(t, app, "stacks", map[string]any{"name": "stack"})
	mustCreateEnvRecord(t, app, "stack_env_vars", map[string]any{
		"stack":           stack.Id,
		"key":             "DB_PASS",
		"value":           "secret/data/myapp#DB_PASS",
		"secret":          true,
		"secret_provider": "vault",
	})

	err := CheckStackSecretBackends(app, stack.Id)
	if err == nil {
		t.Fatal("expected error for disabled vault backend, got nil")
	}
	if !containsAll(err.Error(), "vault", "DB_PASS") {
		t.Fatalf("error %q does not name provider/key", err.Error())
	}
}

func TestCheckStackSecretBackendsMissingIntegrationRow(t *testing.T) {
	app := newEnvVarsTestApp(t)
	// No "vault" row in integrations at all — must be treated as disabled.
	stack := mustCreateEnvRecord(t, app, "stacks", map[string]any{"name": "stack"})
	mustCreateEnvRecord(t, app, "stack_env_vars", map[string]any{
		"stack":           stack.Id,
		"key":             "DB_PASS",
		"value":           "secret/data/myapp#DB_PASS",
		"secret":          true,
		"secret_provider": "vault",
	})

	if err := CheckStackSecretBackends(app, stack.Id); err == nil {
		t.Fatal("expected error when integrations row is missing entirely, got nil")
	}
}

func TestCheckStackSecretBackendsOnlyNamesDisabledProvider(t *testing.T) {
	app := newEnvVarsTestApp(t)
	mustCreateEnvRecord(t, app, "integrations", map[string]any{"slug": "vault", "enabled": true})
	mustCreateEnvRecord(t, app, "integrations", map[string]any{"slug": "infisical", "enabled": false})
	stack := mustCreateEnvRecord(t, app, "stacks", map[string]any{"name": "stack"})
	mustCreateEnvRecord(t, app, "stack_env_vars", map[string]any{
		"stack": stack.Id, "key": "DB_PASS", "value": "secret/data/myapp#DB_PASS",
		"secret": true, "secret_provider": "vault",
	})
	mustCreateEnvRecord(t, app, "stack_env_vars", map[string]any{
		"stack": stack.Id, "key": "API_KEY", "value": "proj/env#API_KEY",
		"secret": true, "secret_provider": "infisical",
	})

	err := CheckStackSecretBackends(app, stack.Id)
	if err == nil {
		t.Fatal("expected error naming the disabled infisical backend, got nil")
	}
	if containsAll(err.Error(), "vault") {
		t.Fatalf("error %q should not mention the enabled vault backend", err.Error())
	}
	if !containsAll(err.Error(), "infisical", "API_KEY") {
		t.Fatalf("error %q does not name infisical/API_KEY", err.Error())
	}
}

func TestCheckStackSecretBackendsGlobalBinding(t *testing.T) {
	app := newEnvVarsTestApp(t)
	mustCreateEnvRecord(t, app, "integrations", map[string]any{"slug": "vault", "enabled": false})
	stack := mustCreateEnvRecord(t, app, "stacks", map[string]any{"name": "stack"})
	global := mustCreateEnvRecord(t, app, "global_env_vars", map[string]any{
		"key": "SHARED_DB_PASS", "value": "secret/data/shared#DB_PASS",
		"secret": true, "secret_provider": "vault",
	})
	mustCreateEnvRecord(t, app, "stack_global_env_vars", map[string]any{"stack": stack.Id, "global_env_var": global.Id})

	err := CheckStackSecretBackends(app, stack.Id)
	if err == nil {
		t.Fatal("expected error for bound global env var referencing a disabled backend, got nil")
	}
	if !containsAll(err.Error(), "vault", "SHARED_DB_PASS") {
		t.Fatalf("error %q does not name provider/key from global binding", err.Error())
	}
}

func TestCheckJobSecretBackendsDisabled(t *testing.T) {
	app := newEnvVarsTestApp(t)
	mustCreateEnvRecord(t, app, "integrations", map[string]any{"slug": "infisical", "enabled": false})
	job := mustCreateEnvRecord(t, app, "scheduled_jobs", map[string]any{"name": "job"})
	mustCreateEnvRecord(t, app, "job_env_vars", map[string]any{
		"job": job.Id, "key": "API_KEY", "value": "proj/env#API_KEY",
		"secret": true, "secret_provider": "infisical",
	})

	if err := CheckJobSecretBackends(app, job.Id); err == nil {
		t.Fatal("expected error for job referencing a disabled infisical backend, got nil")
	}
}

func TestCheckJobSecretBackendsEnabled(t *testing.T) {
	app := newEnvVarsTestApp(t)
	mustCreateEnvRecord(t, app, "integrations", map[string]any{"slug": "infisical", "enabled": true})
	job := mustCreateEnvRecord(t, app, "scheduled_jobs", map[string]any{"name": "job"})
	mustCreateEnvRecord(t, app, "job_env_vars", map[string]any{
		"job": job.Id, "key": "API_KEY", "value": "proj/env#API_KEY",
		"secret": true, "secret_provider": "infisical",
	})

	if err := CheckJobSecretBackends(app, job.Id); err != nil {
		t.Fatalf("expected nil for enabled infisical backend, got %v", err)
	}
}

func containsAll(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
