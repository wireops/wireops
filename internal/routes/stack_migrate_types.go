package routes

// MigratePreview is the response body for POST
// /api/custom/stacks/{id}/migrate/preview: a read-only report comparing the
// stack's current (source) resolved compose config against what it would
// resolve to on the target repository, so an operator can judge whether a
// repo migration is safe before committing to it.
type MigratePreview struct {
	SourceRepository string           `json:"source_repository"`
	TargetRepository string           `json:"target_repository"`
	Services         MigrateDiff      `json:"services"`
	Volumes          MigrateDiff      `json:"volumes"`
	Networks         MigrateDiff      `json:"networks"`
	ProjectName      ProjectNameCheck `json:"project_name"`
	Sops             SopsCheck        `json:"sops"`
	Warnings         []MigrateWarning `json:"warnings"`
}

// MigrateDiff is a named-resource set comparison (services, volumes, or
// networks) between the source and target compose configs.
type MigrateDiff struct {
	Added   []string `json:"added"`
	Removed []string `json:"removed"`
	Common  []string `json:"common"`
}

// ProjectNameCheck compares the `docker compose config` top-level `name:`
// between source and target — see §1.2/§6 of the migration plan: containers
// always recreate on migration, but a changed project name means the old
// project's containers become orphans the new deploy's --remove-orphans
// can't reach.
type ProjectNameCheck struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Same   bool   `json:"same"`
}

// SopsCheck reports whether the target repository's secrets.yaml (if any)
// can be decrypted with the target repository's own SOPS age key. Status is
// one of "none" (no secrets.yaml on the target), "ok" (decrypts),
// "undecryptable" (present but doesn't decrypt with the target's key — the
// common case right after a migration, since secrets.yaml is encrypted
// per-repository), or "source_had_secrets" (the source had one, the target
// has none).
type SopsCheck struct {
	Status             string `json:"status"`
	TargetAgePublicKey string `json:"target_age_public_key,omitempty"`
}

// MigrateWarning is one advisory finding surfaced by the preview. Nothing
// in this list blocks POST /migrate — migration is always the operator's
// call; the UI uses Severity to decide how loudly to render each row.
type MigrateWarning struct {
	Severity string `json:"severity"` // "critical" | "warn" | "info"
	Code     string `json:"code"`
	Message  string `json:"message"`
}
