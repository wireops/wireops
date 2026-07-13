package envvars

import (
	"fmt"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// CheckStackSecretBackends inspects a stack's local + bound-global secret
// env vars without resolving any value, and returns an error naming every
// disabled/unconfigured external secret backend (vault/infisical)
// referenced, plus the offending env var keys. Meant as a fast pre-flight
// gate — called before git fetch/render — so a disabled backend is caught
// immediately instead of after wasted work deep inside env var resolution.
func CheckStackSecretBackends(app core.App, stackID string) error {
	return checkSecretBackends(app, "stack_env_vars", "stack", stackID, "stack_global_env_vars")
}

// CheckJobSecretBackends is CheckStackSecretBackends for scheduled jobs.
func CheckJobSecretBackends(app core.App, jobID string) error {
	return checkSecretBackends(app, "job_env_vars", "job", jobID, "job_global_env_vars")
}

func checkSecretBackends(app core.App, localCollection, targetField, targetID, bindingCollection string) error {
	offenders := map[string][]string{} // provider -> env var keys

	collect := func(rec *core.Record) {
		if !rec.GetBool("secret") {
			return
		}
		// Mirrors isInternalSecretProvider in internal/hooks/pb_hooks.go —
		// duplicated inline since envvars doesn't import hooks. Only
		// external providers have a backend that can be "disabled"; the
		// internal AES-GCM provider has no such concept.
		provider := rec.GetString("secret_provider")
		if provider == "" || provider == "internal" {
			return
		}
		key := strings.TrimSpace(rec.GetString("key"))
		if key == "" {
			return
		}
		offenders[provider] = append(offenders[provider], key)
	}

	globalRecords, err := loadGlobalRecords(app, bindingCollection, targetField, targetID)
	if err != nil {
		return err
	}
	for _, rec := range globalRecords {
		collect(rec)
	}

	localRecords, err := findAllIfCollectionExists(app, localCollection, dbx.HashExp{targetField: targetID})
	if err != nil {
		return err
	}
	for _, rec := range localRecords {
		collect(rec)
	}

	if len(offenders) == 0 {
		return nil
	}

	providers := make([]string, 0, len(offenders))
	for provider := range offenders {
		providers = append(providers, provider)
	}
	sort.Strings(providers)

	var disabled []string
	for _, provider := range providers {
		rec, err := app.FindFirstRecordByFilter("integrations", "slug = {:slug}", dbx.Params{"slug": provider})
		enabled := err == nil && rec.GetBool("enabled")
		if !enabled {
			keys := offenders[provider]
			sort.Strings(keys)
			disabled = append(disabled, fmt.Sprintf("%s (env vars: %s)", provider, strings.Join(keys, ", ")))
		}
	}
	if len(disabled) == 0 {
		return nil
	}
	return fmt.Errorf("secret backend(s) disabled: %s", strings.Join(disabled, "; "))
}
