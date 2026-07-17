package hooks

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/go-git/go-git/v5/plumbing/transport"
	validation "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"gopkg.in/yaml.v3"

	"github.com/wireops/wireops/internal/audit"
	"github.com/wireops/wireops/internal/config"
	"github.com/wireops/wireops/internal/crypto"
	"github.com/wireops/wireops/internal/git"
	"github.com/wireops/wireops/internal/jobscheduler"
	"github.com/wireops/wireops/internal/logstream"
	"github.com/wireops/wireops/internal/oidc"
	"github.com/wireops/wireops/internal/rbac"
	"github.com/wireops/wireops/internal/safepath"
	"github.com/wireops/wireops/internal/secrets"
	"github.com/wireops/wireops/internal/sync"
)

func isSSHGitURL(gitURL string) bool {
	gitURL = strings.TrimSpace(gitURL)
	if gitURL == "" {
		return false
	}
	if strings.HasPrefix(gitURL, "ssh://") {
		return true
	}
	if strings.Contains(gitURL, "://") {
		return false
	}
	at := strings.Index(gitURL, "@")
	colon := strings.Index(gitURL, ":")
	return at > 0 && colon > at
}

func validateRepositoryKeyAssignment(app core.App, repository *core.Record) error {
	keyID := strings.TrimSpace(repository.GetString("repository_key"))
	if keyID == "" {
		if isSSHGitURL(repository.GetString("git_url")) {
			return validation.Errors{
				"repository_key": validation.NewError("validation_repository_key_required", "An SSH key is required for SSH repositories."),
			}
		}
		return nil
	}
	key, err := app.FindRecordById("repository_keys", keyID)
	if err != nil {
		return validation.Errors{
			"repository_key": validation.NewError("validation_repository_key_missing", "Repository key was not found."),
		}
	}
	expectedType := string(git.AuthTypeBasic)
	if isSSHGitURL(repository.GetString("git_url")) {
		expectedType = string(git.AuthTypeSSH)
	}
	if key.GetString("auth_type") != expectedType {
		return validation.Errors{
			"repository_key": validation.NewError("validation_repository_key_type", fmt.Sprintf("This repository requires a %s key.", expectedType)),
		}
	}
	return nil
}

// ensureRepositorySopsKeypair generates a per-repository age keypair on
// creation (P1.5), so every repository is ready to decrypt a
// secrets.yaml/secrets.yml as soon as one shows up next to its
// wireops.yaml. The private key is encrypted at rest with SECRET_KEY, same
// as ssh_private_key/git_password; the public key is stored as plain text
// so it can be shown in the UI for `sops -e --age <key> secrets.yaml`.
func ensureRepositorySopsKeypair(repository *core.Record) error {
	// Both fields are server-managed: always generate a fresh keypair on
	// create and overwrite whatever the caller may have sent, so a
	// caller-supplied sops_age_key can never bypass encryption and
	// sops_age_public_key can never be spoofed out of sync with the private key.
	privateKey, publicKey, err := secrets.GenerateAgeKeypair()
	if err != nil {
		return fmt.Errorf("failed to generate SOPS age keypair: %w", err)
	}

	secretKey := crypto.NormalizeSecretKey(os.Getenv("SECRET_KEY"))
	encrypted, err := crypto.Encrypt([]byte(privateKey), secretKey)
	if err != nil {
		return fmt.Errorf("failed to encrypt SOPS age key: %w", err)
	}

	repository.Set("sops_age_key", encrypted)
	repository.Set("sops_age_public_key", publicKey)
	return nil
}

func validateRepositoryKeyTypeImmutable(app core.App, record *core.Record) error {
	original := record.Original()
	originalType := ""
	if original != nil {
		originalType = original.GetString("auth_type")
	}
	if originalType == "" && strings.TrimSpace(record.Id) != "" {
		persisted, err := app.FindRecordById("repository_keys", record.Id)
		if err != nil {
			return fmt.Errorf("find repository key %s: %w", record.Id, err)
		}
		originalType = persisted.GetString("auth_type")
	}
	if originalType == "" || record.GetString("auth_type") == originalType {
		return nil
	}
	return validation.Errors{
		"auth_type": validation.NewError("validation_repository_key_type_immutable", "Repository key type cannot be changed after creation."),
	}
}

// wireopsManagedStackFields are the fields sourced from a stack's
// wireops.yaml at creation time. Once a stack has config_source ==
// "wireops_file", these become immutable via the API — the only way to
// change deploy behavior is to edit the wireops.yaml file in the repo and
// recreate the stack. This is enforced server-side because the stacks
// collection's Update rule allows any authenticated user to PATCH any field.
var wireopsManagedStackFields = []string{
	"compose_path",
	"compose_file",
	"remove_orphans",
	"force_pull",
	"deploy_timeout_seconds",
	"sync_interval_seconds",
	"wait_running_jobs",
	"wait_running_jobs_timeout_seconds",
	"worker_tags",
	"wireops_file_path",
	"config_source",
}

func validateWireopsFieldsImmutable(app core.App, record *core.Record) error {
	original := record.Original()
	if original == nil && strings.TrimSpace(record.Id) != "" {
		persisted, err := app.FindRecordById("stacks", record.Id)
		if err != nil {
			return fmt.Errorf("find stack %s: %w", record.Id, err)
		}
		original = persisted
	}
	if original == nil || original.GetString("config_source") != "wireops_file" {
		return nil
	}

	errs := validation.Errors{}
	for _, field := range wireopsManagedStackFields {
		if !reflect.DeepEqual(original.Get(field), record.Get(field)) {
			errs[field] = validation.NewError("validation_wireops_field_immutable", "This field is managed by wireops.yaml and cannot be edited from the UI.")
		}
	}
	if len(errs) > 0 {
		return errs
	}
	return nil
}

func loadRepositoryTransportAuth(app core.App, repoID string) (transport.AuthMethod, bool, error) {
	credential, err := git.LoadRepositoryCredential(app, repoID)
	if err != nil {
		return nil, false, err
	}
	if credential.AuthType == git.AuthTypeNone {
		return nil, false, nil
	}
	auth, err := git.ResolveTransportAuth(*credential)
	return auth, true, err
}

func triggerRepositoryBackgroundClone(app core.App, repoID, gitURL, branch string) {
	if branch == "" {
		branch = "main"
	}

	go func() {
		auth, hasCred, err := loadRepositoryTransportAuth(app, repoID)
		if err != nil {
			log.Printf("[hooks] failed to resolve git auth for repo %s", repoID)
			return
		}

		if !hasCred && isSSHGitURL(gitURL) {
			log.Printf("[hooks] background clone deferred for repo %s: waiting for SSH credentials", repoID)
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		_, err = git.CloneOrFetchContext(ctx, repoID, gitURL, branch, auth, config.GetReposWorkspace())
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
				log.Printf("[hooks] background clone timed out for repo %s", repoID)
			} else {
				log.Printf("[hooks] background clone failed for repo %s", repoID)
			}
		} else {
			log.Printf("[hooks] background clone success for repo %s", repoID)
		}
	}()
}

func Register(app core.App, scheduler *sync.Scheduler, jobSched *jobscheduler.Scheduler, logBroker *logstream.Broker) {
	secretKey := crypto.NormalizeSecretKey(os.Getenv("SECRET_KEY"))
	registerAuditHooks(app)

	// Fan out sync_logs writes to the live tail broker (GET /api/custom/stacks/{id}/stream).
	publishSyncLogEvent := func(e *core.RecordEvent) error {
		logBroker.Publish(e.Record.GetString("stack"), logstream.Event{
			RecordID: e.Record.Id,
			Output:   e.Record.GetString("output"),
			Status:   e.Record.GetString("status"),
		})
		return e.Next()
	}
	app.OnRecordAfterCreateSuccess("sync_logs").BindFunc(publishSyncLogEvent)
	app.OnRecordAfterUpdateSuccess("sync_logs").BindFunc(publishSyncLogEvent)

	// OIDC_REQUIRE_EMAIL_VERIFIED: secure by default — reject when email_verified is absent or false.
	// Set explicitly to "false" to opt out (e.g. IdPs that omit the claim or treat it like Authentik by default).
	requireEmailVerified := os.Getenv("OIDC_REQUIRE_EMAIL_VERIFIED") != "false"

	// When verification is not required, or when PocketBase's OIDC client leaves AuthUser.Email blank
	// (e.g. unverified email stripped), we recover the address from raw claims so record creation does not
	// fail with "email: cannot be blank".
	app.OnRecordAuthWithOAuth2Request("sso_users").BindFunc(HandleSSOAuthRequest(requireEmailVerified))

	// OIDC client secret must not be persisted; only OIDC_CLIENT_SECRET is used at runtime.
	app.OnCollectionUpdateExecute("sso_users").BindFunc(func(e *core.CollectionEvent) error {
		if e.Type != core.ModelEventTypeUpdate {
			return e.Next()
		}
		oidc.ClearPersistedClientSecret(e.Collection)
		return e.Next()
	})

	// Encrypt reusable repository key fields on create/update.
	app.OnRecordCreate("repository_keys").BindFunc(func(e *core.RecordEvent) error {
		if err := encryptSensitiveFields(e.Record, secretKey); err != nil {
			log.Printf("[hooks] failed to encrypt repository key %s: %v", e.Record.Id, err)
			return err
		}
		return e.Next()
	})

	app.OnRecordUpdate("repository_keys").BindFunc(func(e *core.RecordEvent) error {
		if err := validateRepositoryKeyTypeImmutable(e.App, e.Record); err != nil {
			return err
		}
		if err := encryptSensitiveFields(e.Record, secretKey); err != nil {
			log.Printf("[hooks] failed to encrypt repository key %s: %v", e.Record.Id, err)
			return err
		}
		return e.Next()
	})

	app.OnRecordAfterUpdateSuccess("repository_keys").BindFunc(func(e *core.RecordEvent) error {
		original := e.Record.Original()
		credentialChanged := original == nil
		for _, field := range []string{"auth_type", "ssh_private_key", "ssh_passphrase", "ssh_known_host", "git_username", "git_password"} {
			if original != nil && e.Record.GetString(field) != original.GetString(field) {
				credentialChanged = true
				break
			}
		}
		if !credentialChanged {
			return e.Next()
		}
		repositories, err := e.App.FindAllRecords("repositories", dbx.HashExp{"repository_key": e.Record.Id})
		if err != nil {
			return fmt.Errorf("list repositories using key %s: %w", e.Record.Id, err)
		}
		for _, repository := range repositories {
			triggerRepositoryBackgroundClone(app, repository.Id, repository.GetString("git_url"), repository.GetString("branch"))
		}
		return e.Next()
	})

	app.OnRecordDelete("repository_keys").BindFunc(func(e *core.RecordEvent) error {
		repositories, err := e.App.FindAllRecords("repositories", dbx.HashExp{"repository_key": e.Record.Id})
		if err != nil {
			return fmt.Errorf("list repositories using key %s: %w", e.Record.Id, err)
		}
		if len(repositories) > 0 {
			return validation.Errors{
				"repository_key": validation.NewError("validation_repository_key_in_use", fmt.Sprintf("Key is used by %d repository(s).", len(repositories))),
			}
		}
		return e.Next()
	})

	// Dynamically hide credential fields on record enrich for standard API requests
	app.OnRecordEnrich("repository_keys").BindFunc(func(e *core.RecordEnrichEvent) error {
		isSuperuser := false
		if e.RequestInfo != nil && e.RequestInfo.Auth != nil {
			authCol := e.RequestInfo.Auth.Collection()
			if authCol != nil && authCol.Name == core.CollectionNameSuperusers {
				isSuperuser = true
			}
		}
		if !isSuperuser {
			e.Record.Hide("ssh_private_key", "ssh_passphrase", "git_password")
		}
		return e.Next()
	})

	app.OnRecordCreate("worker_policies").BindFunc(func(e *core.RecordEvent) error {
		records, err := app.FindAllRecords("worker_policies")
		if err != nil {
			return fmt.Errorf("failed to check existing worker policies: %w", err)
		}
		if len(records) > 0 {
			return fmt.Errorf("worker_policies is a singleton collection; a record already exists")
		}
		return e.Next()
	})

	validateAssignedWorker := func(rec *core.Record) error {
		workerID := rec.GetString("worker")
		if workerID == "" {
			return fmt.Errorf("worker is required")
		}
		if _, err := app.FindRecordById("workers", workerID); err != nil {
			return fmt.Errorf("worker not found")
		}
		return nil
	}

	app.OnRecordCreate("stacks").BindFunc(func(e *core.RecordEvent) error {
		if err := validateAssignedWorker(e.Record); err != nil {
			return err
		}
		if err := encryptField(e.Record, "webhook_secret", secretKey); err != nil {
			return err
		}
		return e.Next()
	})

	app.OnRecordUpdate("stacks").BindFunc(func(e *core.RecordEvent) error {
		if err := validateAssignedWorker(e.Record); err != nil {
			return err
		}
		if err := validateWireopsFieldsImmutable(e.App, e.Record); err != nil {
			return err
		}
		preserveMaskedFieldValue(e.Record, "webhook_secret")
		if err := encryptField(e.Record, "webhook_secret", secretKey); err != nil {
			return err
		}
		return e.Next()
	})

	// Repository hooks
	app.OnRecordCreate("repositories").BindFunc(func(e *core.RecordEvent) error {
		if err := validateRepositoryKeyAssignment(e.App, e.Record); err != nil {
			return err
		}
		if err := ensureRepositorySopsKeypair(e.Record); err != nil {
			return err
		}
		return e.Next()
	})

	app.OnRecordUpdate("repositories").BindFunc(func(e *core.RecordEvent) error {
		if err := validateRepositoryKeyAssignment(e.App, e.Record); err != nil {
			return err
		}
		return e.Next()
	})

	app.OnRecordAfterCreateSuccess("repositories").BindFunc(func(e *core.RecordEvent) error {
		repoID := e.Record.Id
		gitURL := e.Record.GetString("git_url")
		branch := e.Record.GetString("branch")
		triggerRepositoryBackgroundClone(app, repoID, gitURL, branch)
		return e.Next()
	})

	app.OnRecordDelete("repositories").BindFunc(func(e *core.RecordEvent) error {
		records, err := app.FindAllRecords("stacks", dbx.HashExp{"repository": e.Record.Id})
		if err == nil && len(records) > 0 {
			return fmt.Errorf("cannot delete repository because it has %d associated stack(s)", len(records))
		}
		return e.Next()
	})

	app.OnRecordAfterDeleteSuccess("repositories").BindFunc(func(e *core.RecordEvent) error {
		repoDir := filepath.Join(config.GetReposWorkspace(), e.Record.Id)
		if err := os.RemoveAll(repoDir); err != nil {
			log.Printf("[hooks] failed to remove repo directory %s: %v", repoDir, err)
		}
		return e.Next()
	})

	app.OnRecordEnrich("repositories").BindFunc(func(e *core.RecordEnrichEvent) error {
		repoDir := filepath.Join(config.GetReposWorkspace(), e.Record.Id)
		_, err := os.Stat(filepath.Join(repoDir, ".git"))
		e.Record.Set("is_cloned", err == nil)
		e.Record.WithCustomData(true)
		return e.Next()
	})

	// Stacks trigger scheduler registration
	app.OnRecordAfterCreateSuccess("stacks").BindFunc(func(e *core.RecordEvent) error {
		scheduler.RegisterStack(e.Record)
		// "manual" here, not "webhook": this fires for every stack creation
		// (UI create, wireops.yaml import, local import), none of which are
		// an actual git webhook delivery — "webhook" is reserved for real
		// POST /api/custom/webhook/{id} deliveries (stack_routes.go).
		scheduler.TriggerSync(e.Record.Id, "manual", 0, "system")
		return e.Next()
	})

	app.OnRecordAfterUpdateSuccess("stacks").BindFunc(func(e *core.RecordEvent) error {
		scheduler.RegisterStack(e.Record)
		return e.Next()
	})

	app.OnRecordAfterDeleteSuccess("stacks").BindFunc(func(e *core.RecordEvent) error {
		scheduler.UnregisterStack(e.Record.Id)
		return e.Next()
	})

	app.OnRecordEnrich("stacks").BindFunc(func(e *core.RecordEnrichEvent) error {
		if e.Record.GetString("webhook_secret") != "" {
			e.Record.Set("webhook_secret", "••••••••")
		}

		var composeContent []byte

		// Prefer rendered revision content stored in the record (especially for local stacks
		// where the original file may only be present on the worker).
		if rendered := e.Record.GetString("rendered_revision"); rendered != "" {
			composeContent = []byte(rendered)
		} else {
			var composeFile string
			if e.Record.GetString("source_type") == "local" {
				if importPath := e.Record.GetString("import_path"); importPath != "" {
					composeFile = importPath
				}
			} else {
				repoID := e.Record.GetString("repository")
				base := filepath.Join(config.GetReposWorkspace(), repoID)

				composePath := e.Record.GetString("compose_path")
				composeFileName := e.Record.GetString("compose_file")

				// Validate compose_path and compose_file to prevent traversal
				if err := safepath.ValidateComposePath(composePath); err != nil {
					composePath = "" // Fallback to root
				}
				if err := safepath.ValidateComposeFile(composeFileName); err != nil {
					composeFileName = "docker-compose.yml" // Fallback to default
				} else if composeFileName == "" {
					composeFileName = "docker-compose.yml"
				}

				dir := base
				if composePath != "" && composePath != "." {
					dir = filepath.Join(base, filepath.Clean(composePath))
				}
				composeFile = filepath.Join(dir, composeFileName)
			}

			if composeFile != "" {
				if b, err := os.ReadFile(composeFile); err == nil {
					composeContent = b
				}
			}
		}

		if len(composeContent) > 0 {
			containers := extractContainersFromCompose(composeContent)
			// Create a transient property that the PocketBase API will serialize
			e.Record.Set("containers_list", containers)
			e.Record.WithCustomData(true)
		}

		return e.Next()
	})

	// Encrypt stack env var values on create/update only when secret=true
	app.OnRecordCreate("stack_env_vars").BindFunc(func(e *core.RecordEvent) error {
		if err := prepareEnvSecretRecord(e.Record, secretKey); err != nil {
			return err
		}
		return e.Next()
	})

	app.OnRecordUpdate("stack_env_vars").BindFunc(func(e *core.RecordEvent) error {
		if err := preventEnvSecretDowngrade(e.Record); err != nil {
			return err
		}
		if err := preventEnvSecretProviderChange(e.Record); err != nil {
			return err
		}
		if err := prepareEnvSecretRecord(e.Record, secretKey); err != nil {
			return err
		}
		return e.Next()
	})

	// Mask secret env var values on API responses
	app.OnRecordEnrich("stack_env_vars").BindFunc(func(e *core.RecordEnrichEvent) error {
		if e.Record.GetBool("secret") && isInternalSecretProvider(e.Record.GetString("secret_provider")) {
			e.Record.Set("value", "")
		}
		return e.Next()
	})

	app.OnRecordCreate("global_env_vars").BindFunc(func(e *core.RecordEvent) error {
		if err := prepareEnvSecretRecord(e.Record, secretKey); err != nil {
			return err
		}
		return e.Next()
	})

	app.OnRecordUpdate("global_env_vars").BindFunc(func(e *core.RecordEvent) error {
		if err := preventEnvSecretDowngrade(e.Record); err != nil {
			return err
		}
		if err := preventEnvSecretProviderChange(e.Record); err != nil {
			return err
		}
		if err := prepareEnvSecretRecord(e.Record, secretKey); err != nil {
			return err
		}
		return e.Next()
	})

	app.OnRecordDelete("global_env_vars").BindFunc(func(e *core.RecordEvent) error {
		stackCount, err := countRecordsIfCollectionExists(e.App, "stack_global_env_vars", dbx.HashExp{"global_env_var": e.Record.Id})
		if err != nil {
			return err
		}
		jobCount, err := countRecordsIfCollectionExists(e.App, "job_global_env_vars", dbx.HashExp{"global_env_var": e.Record.Id})
		if err != nil {
			return err
		}
		if stackCount+jobCount > 0 {
			return fmt.Errorf("cannot delete global variable because it is associated with %d stack(s) and %d job(s)", stackCount, jobCount)
		}
		return e.Next()
	})

	app.OnRecordEnrich("global_env_vars").BindFunc(func(e *core.RecordEnrichEvent) error {
		if e.Record.GetBool("secret") && isInternalSecretProvider(e.Record.GetString("secret_provider")) {
			e.Record.Set("value", "")
		}
		return e.Next()
	})

	validateJobRecord := func(rec *core.Record) error {
		name := rec.GetString("name")
		if name == "" {
			return validation.Errors{
				"name": validation.NewError("validation_required", "Name is required."),
			}
		}
		nameRegex := regexp.MustCompile(`^[a-zA-Z0-9\p{L}_ -]+$`)
		if !nameRegex.MatchString(name) {
			return validation.Errors{
				"name": validation.NewError("validation_invalid_format", "Name can only contain alphanumeric characters, spaces, underscores, and hyphens."),
			}
		}
		return nil
	}

	app.OnRecordCreate("scheduled_jobs").BindFunc(func(e *core.RecordEvent) error {
		if err := validateJobRecord(e.Record); err != nil {
			return err
		}
		return e.Next()
	})

	app.OnRecordUpdate("scheduled_jobs").BindFunc(func(e *core.RecordEvent) error {
		if err := validateJobRecord(e.Record); err != nil {
			return err
		}
		return e.Next()
	})

	// Scheduled jobs trigger job scheduler registration
	app.OnRecordAfterCreateSuccess("scheduled_jobs").BindFunc(func(e *core.RecordEvent) error {
		jobSched.RegisterJob(e.Record.Id)
		return e.Next()
	})

	app.OnRecordAfterUpdateSuccess("scheduled_jobs").BindFunc(func(e *core.RecordEvent) error {
		jobSched.RegisterJob(e.Record.Id)
		return e.Next()
	})

	// Cascade delete: remove job_runs and job_env_vars before deleting the job
	app.OnRecordDelete("scheduled_jobs").BindFunc(func(e *core.RecordEvent) error {
		jobID := e.Record.Id
		runs, err := app.FindAllRecords("job_runs", dbx.HashExp{"job": jobID})
		if err == nil {
			for _, r := range runs {
				if err := app.Delete(r); err != nil {
					return fmt.Errorf("failed to delete job_runs %s for job %s: %w", r.Id, jobID, err)
				}
			}
		} else {
			return fmt.Errorf("failed to list job_runs for job %s: %w", jobID, err)
		}
		envVars, err := app.FindAllRecords("job_env_vars", dbx.HashExp{"job": jobID})
		if err == nil {
			for _, r := range envVars {
				if err := app.Delete(r); err != nil {
					return fmt.Errorf("failed to delete job_env_vars %s for job %s: %w", r.Id, jobID, err)
				}
			}
		} else {
			return fmt.Errorf("failed to list job_env_vars for job %s: %w", jobID, err)
		}
		globalBindings, err := findAllRecordsIfCollectionExists(app, "job_global_env_vars", dbx.HashExp{"job": jobID})
		if err != nil {
			return fmt.Errorf("failed to list job_global_env_vars for job %s: %w", jobID, err)
		}
		for _, r := range globalBindings {
			if err := app.Delete(r); err != nil {
				return fmt.Errorf("failed to delete job_global_env_vars %s for job %s: %w", r.Id, jobID, err)
			}
		}
		return e.Next()
	})

	app.OnRecordAfterDeleteSuccess("scheduled_jobs").BindFunc(func(e *core.RecordEvent) error {
		jobSched.UnregisterJob(e.Record.Id)
		return e.Next()
	})

	// Prevent deleting a repository that still has jobs referencing it
	app.OnRecordDelete("repositories").BindFunc(func(e *core.RecordEvent) error {
		jobs, err := app.FindAllRecords("scheduled_jobs", dbx.HashExp{"repository": e.Record.Id})
		if err == nil && len(jobs) > 0 {
			return fmt.Errorf("cannot delete repository because it has %d associated job(s)", len(jobs))
		}
		return e.Next()
	})

	// When a repository is updated (git-pulled), re-sync all jobs pointing to it
	// so the scheduler picks up any changes to cron expressions or other yaml fields.
	app.OnRecordAfterUpdateSuccess("repositories").BindFunc(func(e *core.RecordEvent) error {
		jobSched.SyncJobsForRepo(e.Record.Id)
		original := e.Record.Original()
		if original != nil && (e.Record.GetString("git_url") != original.GetString("git_url") ||
			e.Record.GetString("branch") != original.GetString("branch") ||
			e.Record.GetString("repository_key") != original.GetString("repository_key")) {
			triggerRepositoryBackgroundClone(app, e.Record.Id, e.Record.GetString("git_url"), e.Record.GetString("branch"))
		}
		return e.Next()
	})

	// Encrypt job env var values on create/update when secret=true
	app.OnRecordCreate("job_env_vars").BindFunc(func(e *core.RecordEvent) error {
		if err := prepareEnvSecretRecord(e.Record, secretKey); err != nil {
			return err
		}
		return e.Next()
	})

	app.OnRecordUpdate("job_env_vars").BindFunc(func(e *core.RecordEvent) error {
		if err := preventEnvSecretDowngrade(e.Record); err != nil {
			return err
		}
		if err := preventEnvSecretProviderChange(e.Record); err != nil {
			return err
		}
		if err := prepareEnvSecretRecord(e.Record, secretKey); err != nil {
			return err
		}
		return e.Next()
	})

	// Mask secret job env var values on API responses
	app.OnRecordEnrich("job_env_vars").BindFunc(func(e *core.RecordEnrichEvent) error {
		if e.Record.GetBool("secret") && isInternalSecretProvider(e.Record.GetString("secret_provider")) {
			e.Record.Set("value", "")
		}
		return e.Next()
	})

	// Override password reset email to point to custom frontend route
	passwordResetHandler := func(e *core.MailerRecordEvent) error {
		token, _ := e.Meta["token"].(string)
		if token == "" {
			return e.Next()
		}
		actionURL := config.GetAppURL() + "/reset-password?token=" + token
		e.Message.Subject = "Reset your wireops password"
		e.Message.HTML = buildPasswordResetEmailHTML(actionURL)
		e.Message.Text = "Reset your wireops password by visiting: " + actionURL
		return e.Next()
	}
	app.OnMailerRecordPasswordResetSend("_superusers").BindFunc(passwordResetHandler)
	app.OnMailerRecordPasswordResetSend("users").BindFunc(passwordResetHandler)

	app.OnRecordRequestPasswordResetRequest("users").BindFunc(func(e *core.RecordRequestPasswordResetRequestEvent) error {
		if e.Record != nil && e.Record.GetBool("is_sso") {
			// Return a generic success to prevent email enumeration, but do not send anything.
			return e.NoContent(204)
		}
		return e.Next()
	})

	app.OnRecordConfirmPasswordResetRequest("users").BindFunc(func(e *core.RecordConfirmPasswordResetRequestEvent) error {
		if e.Record != nil && e.Record.GetBool("is_sso") {
			// Explicitly reject confirmation for SSO users.
			return e.JSON(400, map[string]string{"error": "SSO accounts cannot reset local password"})
		}
		return e.Next()
	})
}

func registerAuditHooks(app core.App) {
	auditedCollections := map[string]string{
		core.CollectionNameSuperusers: "user",
		"app_settings":                "app_settings",
		"invites":                     "invite",
		"job_env_vars":                "job_env_var",
		"repositories":                "repository",
		"repository_keys":             "repository_key",
		"scheduled_jobs":              "scheduled_job",
		"stack_env_vars":              "stack_env_var",
		"integrations":                "integration",
		"stacks":                      "stack",
		"service_accounts":            "service_account",
		"sso_group_roles":             "sso_group_role",
		"users":                       "user",
		"worker_policies":             "worker_policy",
		"workers":                     "worker",
	}

	app.OnRecordCreateRequest().BindFunc(func(e *core.RecordRequestEvent) error {
		if err := blockAuditLogMutation(e.Collection.Name); err != nil {
			return err
		}
		resourceType, ok := auditedCollections[e.Collection.Name]
		if !ok || shouldSkipRequestAudit(e.RequestEvent) {
			return e.Next()
		}
		metadata := audit.RequestMetadata(e.RequestEvent)
		err := e.Next()
		status, code := auditStatus(err)
		audit.RecordRequest(app, e.RequestEvent, audit.Event{
			Action:       resourceType + ".create",
			ResourceType: resourceType,
			ResourceID:   e.Record.Id,
			Metadata:     metadata,
			Status:       status,
			ErrorCode:    code,
		})
		return err
	})

	app.OnRecordUpdateRequest().BindFunc(func(e *core.RecordRequestEvent) error {
		if err := blockAuditLogMutation(e.Collection.Name); err != nil {
			return err
		}
		resourceType, ok := auditedCollections[e.Collection.Name]
		if !ok || shouldSkipRequestAudit(e.RequestEvent) {
			return e.Next()
		}
		if e.Collection.Name == "users" {
			if roleChanged(e.Record) && !requestHasAdminRole(e.RequestEvent) {
				return apis.NewForbiddenError("only admins can change user roles", nil)
			}
			if e.Record.GetBool("protected") {
				if roleChanged(e.Record) && e.Record.GetString("role") != rbac.RoleAdmin {
					return apis.NewForbiddenError("the initial admin cannot be demoted", nil)
				}
				if disabledChanged(e.Record) && e.Record.GetBool("disabled") {
					return apis.NewForbiddenError("the initial admin cannot be disabled", nil)
				}
			}
			wasActiveAdmin := e.Record.Original().GetString("role") == rbac.RoleAdmin && !e.Record.Original().GetBool("disabled")
			isDisablingAdmin := disabledChanged(e.Record) && e.Record.GetBool("disabled") && wasActiveAdmin
			isDemotingAdmin := roleChanged(e.Record) && e.Record.GetString("role") != rbac.RoleAdmin && wasActiveAdmin

			if isDisablingAdmin || isDemotingAdmin {
				if count, err := countActiveAdminsFromApp(app); err == nil && count <= 1 {
					return apis.NewForbiddenError("cannot remove the last active admin", nil)
				}
			}
		}
		metadata := mergeMetadata(
			audit.RequestMetadata(e.RequestEvent),
			recordChangeMetadata(e.Record),
		)
		err := e.Next()
		status, code := auditStatus(err)
		audit.RecordRequest(app, e.RequestEvent, audit.Event{
			Action:       resourceType + ".update",
			ResourceType: resourceType,
			ResourceID:   e.Record.Id,
			Metadata:     metadata,
			Status:       status,
			ErrorCode:    code,
		})
		if err == nil && (e.Collection.Name == "users" || e.Collection.Name == "service_accounts") && roleChanged(e.Record) {
			audit.RecordRequest(app, e.RequestEvent, audit.Event{
				Action:       resourceType + ".role_changed",
				ResourceType: resourceType,
				ResourceID:   e.Record.Id,
				Metadata: map[string]any{
					"old_role": e.Record.Original().GetString("role"),
					"new_role": e.Record.GetString("role"),
				},
				Status: audit.StatusSuccess,
			})
		}
		return err
	})

	app.OnRecordDeleteRequest().BindFunc(func(e *core.RecordRequestEvent) error {
		if err := blockAuditLogMutation(e.Collection.Name); err != nil {
			return err
		}
		if e.Collection.Name == "users" {
			return apis.NewForbiddenError("users cannot be deleted; deactivate instead", nil)
		}
		resourceType, ok := auditedCollections[e.Collection.Name]
		if !ok || shouldSkipRequestAudit(e.RequestEvent) {
			return e.Next()
		}
		resourceID := e.Record.Id
		metadata := mergeMetadata(
			audit.RequestMetadata(e.RequestEvent),
			recordChangeMetadata(e.Record),
		)
		err := e.Next()
		status, code := auditStatus(err)
		audit.RecordRequest(app, e.RequestEvent, audit.Event{
			Action:       resourceType + ".delete",
			ResourceType: resourceType,
			ResourceID:   resourceID,
			Metadata:     metadata,
			Status:       status,
			ErrorCode:    code,
		})
		return err
	})
}

func shouldSkipRequestAudit(req *core.RequestEvent) bool {
	return req != nil && strings.HasPrefix(req.Request.URL.Path, "/api/custom/")
}

func blockAuditLogMutation(collectionName string) error {
	if collectionName == "audit_logs" {
		return fmt.Errorf("audit logs are append-only")
	}
	return nil
}

func auditStatus(err error) (string, string) {
	if err == nil {
		return audit.StatusSuccess, ""
	}
	return audit.StatusError, "write_failed"
}

func recordChangeMetadata(record *core.Record) map[string]any {
	if record == nil {
		return nil
	}

	changedFields := make([]string, 0, len(record.Collection().Fields))
	sensitiveFields := make([]string, 0)

	for _, field := range record.Collection().Fields {
		name := field.GetName()
		current, currentErr := field.PrepareValue(record, record.GetRaw(name))
		original, originalErr := field.PrepareValue(record.Original(), record.Original().GetRaw(name))
		if currentErr != nil || originalErr != nil {
			continue
		}
		if valuesEqual(current, original) {
			continue
		}

		changedFields = append(changedFields, name)
		if isSensitiveAuditField(name) {
			sensitiveFields = append(sensitiveFields, name)
		}
	}

	if len(changedFields) == 0 {
		return nil
	}

	slices.Sort(changedFields)
	slices.Sort(sensitiveFields)

	metadata := map[string]any{
		"record_changed_fields": changedFields,
	}
	if len(sensitiveFields) > 0 {
		metadata["record_sensitive_fields"] = sensitiveFields
	}

	return metadata
}

func mergeMetadata(parts ...map[string]any) map[string]any {
	merged := map[string]any{}
	for _, part := range parts {
		for key, value := range part {
			merged[key] = value
		}
	}

	if len(merged) == 0 {
		return nil
	}

	return merged
}

func roleChanged(record *core.Record) bool {
	if record == nil || record.Original() == nil {
		return false
	}
	return record.GetString("role") != record.Original().GetString("role")
}

func disabledChanged(record *core.Record) bool {
	if record == nil || record.Original() == nil {
		return false
	}
	return record.GetBool("disabled") != record.Original().GetBool("disabled")
}

func countActiveAdminsFromApp(app core.App) (int, error) {
	records, err := app.FindAllRecords("users",
		dbx.HashExp{"role": rbac.RoleAdmin, "disabled": false},
	)
	if err != nil {
		return 0, err
	}
	return len(records), nil
}

func requestHasAdminRole(req *core.RequestEvent) bool {
	if req == nil || req.Auth == nil {
		return false
	}
	if req.Auth.IsSuperuser() {
		return true
	}
	return rbac.NormalizeRole(req.Auth.GetString("role")) == rbac.RoleAdmin
}

func valuesEqual(a, b any) bool {
	if aStr, ok := a.(string); ok {
		if bBytes, ok := b.([]byte); ok {
			return aStr == string(bBytes)
		}
	}
	if aBytes, ok := a.([]byte); ok {
		if bStr, ok := b.(string); ok {
			return string(aBytes) == bStr
		}
	}
	return reflect.DeepEqual(a, b)
}

func isSensitiveAuditField(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return false
	}

	for _, word := range []string{
		"password",
		"secret",
		"token",
		"private_key",
		"ssh_private_key",
		"ssh_passphrase",
		"git_password",
	} {
		if strings.Contains(name, word) {
			return true
		}
	}

	return false
}

func buildPasswordResetEmailHTML(actionURL string) string {
	escapedURL := html.EscapeString(actionURL)
	return fmt.Sprintf(`<!DOCTYPE html>
<html><head><meta charset="UTF-8"></head>
<body style="font-family:sans-serif;background:#0f1117;color:#e1e4e8;padding:40px 20px;margin:0">
  <div style="max-width:480px;margin:0 auto;background:#1a1d24;border:1px solid #2d333b;border-radius:12px;padding:32px">
    <div style="text-align:center;margin-bottom:24px">
      <span style="font-size:24px;font-weight:900;letter-spacing:4px;color:#ffd700">wireops</span>
    </div>
    <h2 style="margin:0 0 12px;font-size:18px">Reset your password</h2>
    <p style="color:#8b949e;font-size:14px;margin:0 0 24px">
      Click the button below to reset your wireops password. This link expires in 30 minutes.
    </p>
    <div style="text-align:center;margin-bottom:24px">
      <a href="%s" style="display:inline-block;background:#ffd700;color:#000;font-weight:700;font-size:14px;padding:12px 32px;border-radius:8px;text-decoration:none">
        Reset Password
      </a>
    </div>
    <p style="color:#484f58;font-size:12px;margin:0;text-align:center">
      If you didn't request a password reset, you can safely ignore this email.
    </p>
  </div>
</body></html>`, escapedURL)
}

func encryptSensitiveFields(record *core.Record, key []byte) error {
	if len(key) != 32 {
		return fmt.Errorf("SECRET_KEY must be exactly 32 bytes (got %d)", len(key))
	}
	for _, field := range []string{"ssh_private_key", "ssh_passphrase", "git_password"} {
		if err := encryptField(record, field, key); err != nil {
			return err
		}
	}
	return nil
}

func encryptField(record *core.Record, field string, key []byte) error {
	if len(key) != 32 {
		return fmt.Errorf("SECRET_KEY must be exactly 32 bytes (got %d)", len(key))
	}
	val := record.GetString(field)
	if val == "" || val == "••••••••" {
		return nil
	}
	if crypto.IsEncrypted(val) {
		return nil
	}
	encrypted, err := crypto.Encrypt([]byte(val), key)
	if err != nil {
		return fmt.Errorf("failed to encrypt field %s: %w", field, err)
	}
	record.Set(field, encrypted)
	return nil
}

func prepareEnvSecretRecord(record *core.Record, key []byte) error {
	if err := normalizeEnvVarKey(record); err != nil {
		return err
	}

	provider := record.GetString("secret_provider")
	if provider != "" {
		if err := validateSecretProvider(provider); err != nil {
			return err
		}
	} else if record.GetBool("secret") && record.Collection().Fields.GetByName("secret_provider") != nil {
		record.Set("secret_provider", "internal")
	}
	// Only the "internal" provider's "value" is the confidential payload
	// (AES-GCM ciphertext, decrypted at Resolve time). vault/infisical (and
	// any other external provider) store a locator string in "value" —
	// e.g. "mount/data/path#field" — that points at where the secret lives
	// but isn't the secret itself, so it must not be encrypted, blanked, or
	// masked like a real secret.
	if record.GetBool("secret") {
		if isInternalSecretProvider(record.GetString("secret_provider")) {
			preserveMaskedSecretValue(record, "value")
			if err := encryptField(record, "value", key); err != nil {
				return err
			}
		} else if err := secrets.ValidateReference(record.GetString("secret_provider"), record.GetString("value")); err != nil {
			return validation.Errors{
				"value": validation.NewError("validation_invalid_secret_reference", err.Error()),
			}
		}
	}
	return nil
}

// isInternalSecretProvider reports whether provider stores its "value" as
// confidential AES-GCM ciphertext (empty defaults to "internal" for
// collections/records predating the secret_provider field).
func isInternalSecretProvider(provider string) bool {
	return provider == "" || provider == "internal"
}

func normalizeEnvVarKey(record *core.Record) error {
	key := strings.TrimSpace(record.GetString("key"))
	if key == "" {
		return validation.Errors{
			"key": validation.NewError("validation_required", "Key is required."),
		}
	}
	record.Set("key", key)
	return nil
}

// preserveMaskedFieldValue restores field to its previously stored value when
// the incoming value is blank or the masked placeholder, so clients that
// round-trip the masked value (or omit the field) never overwrite it with
// an empty/placeholder string. Unlike preserveMaskedSecretValue, this does
// not require a "secret" boolean flag on the record.
func preserveMaskedFieldValue(record *core.Record, field string) {
	val := record.GetString(field)
	if val != "" && val != "••••••••" {
		return
	}
	original := record.Original()
	if original == nil {
		return
	}
	originalValue := original.GetString(field)
	if originalValue == "" {
		return
	}
	record.Set(field, originalValue)
}

func preserveMaskedSecretValue(record *core.Record, field string) {
	val := record.GetString(field)
	if val != "" && val != "••••••••" {
		return
	}
	original := record.Original()
	if original == nil || !original.GetBool("secret") {
		return
	}
	originalValue := original.GetString(field)
	if originalValue == "" {
		return
	}
	record.Set(field, originalValue)
}

func preventEnvSecretDowngrade(record *core.Record) error {
	original := record.Original()
	if original == nil {
		return nil
	}
	if original.GetBool("secret") && !record.GetBool("secret") {
		return validation.Errors{
			"secret": validation.NewError("validation_secret_downgrade", "Secrets cannot be converted to plain text."),
		}
	}
	return nil
}

// preventEnvSecretProviderChange locks secret_provider once a var is stored
// as a secret: switching a secret from e.g. "internal" to "vault" would
// silently reinterpret its stored value (an encrypted blob vs. a plaintext
// reference), so once created the provider is immutable.
func preventEnvSecretProviderChange(record *core.Record) error {
	original := record.Original()
	if original == nil || !original.GetBool("secret") {
		return nil
	}
	originalProvider := original.GetString("secret_provider")
	if originalProvider == "" {
		originalProvider = "internal"
	}
	newProvider := record.GetString("secret_provider")
	if newProvider == "" {
		newProvider = "internal"
	}
	if originalProvider != newProvider {
		return validation.Errors{
			"secret_provider": validation.NewError("validation_secret_provider_locked", "The secret provider cannot be changed once a secret is created."),
		}
	}
	return nil
}

func findAllRecordsIfCollectionExists(app core.App, collection string, exprs ...dbx.Expression) ([]*core.Record, error) {
	if _, err := app.FindCollectionByNameOrId(collection); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return app.FindAllRecords(collection, exprs...)
}

func countRecordsIfCollectionExists(app core.App, collection string, exprs ...dbx.Expression) (int, error) {
	records, err := findAllRecordsIfCollectionExists(app, collection, exprs...)
	if err != nil {
		return 0, err
	}
	return len(records), nil
}

// validateSecretProvider returns an error when provider is not in
// secrets.ValidProviders. It prevents unimplemented backends (vault,
// infisical) from being persisted, which would cause guaranteed
// deploy-time failures when their Resolve() is called.
func validateSecretProvider(provider string) error {
	for _, valid := range secrets.ValidProviders {
		if provider == valid {
			return nil
		}
	}
	return fmt.Errorf(
		"unknown secret provider %q; currently supported providers: %v",
		provider, secrets.ValidProviders,
	)
}

type ContainerInfo struct {
	Name       string `json:"name"`
	IsFallback bool   `json:"is_fallback"`
	Slug       string `json:"slug,omitempty"`
}

func extractContainersFromCompose(yamlData []byte) []ContainerInfo {
	var composeMap map[string]interface{}
	if err := yaml.Unmarshal(yamlData, &composeMap); err != nil {
		return nil
	}

	servicesRaw, ok := composeMap["services"]
	if !ok || servicesRaw == nil {
		return nil
	}

	services, ok := servicesRaw.(map[string]interface{})
	if !ok {
		return nil
	}

	var results []ContainerInfo
	for svcName, svcRaw := range services {
		svc, ok := svcRaw.(map[string]interface{})
		if !ok {
			continue
		}

		info := ContainerInfo{
			Name:       svcName,
			IsFallback: true,
		}

		if cName, ok := svc["container_name"].(string); ok && cName != "" {
			info.Name = cName
			info.IsFallback = false
		}

		// Extract slug from labels, annotations, deploy.labels, deploy.annotations
		slugFound := false

		checkSlug := func(meta map[string]interface{}) {
			if slugFound {
				return
			}
			if val, ok := meta["customization.image.slug"].(string); ok && val != "" {
				info.Slug = val
				slugFound = true
			}
		}

		checkSlug(sync.NormalizeToMap(svc["labels"]))
		checkSlug(sync.NormalizeToMap(svc["annotations"]))

		if deployRaw, ok := svc["deploy"]; ok {
			if deploy, ok := deployRaw.(map[string]interface{}); ok {
				checkSlug(sync.NormalizeToMap(deploy["labels"]))
				checkSlug(sync.NormalizeToMap(deploy["annotations"]))
			}
		}

		results = append(results, info)
	}

	// Alphabetical sort by Name
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].Name < results[i].Name {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	return results
}

// maskEmailForLog masks an email for safe logging (e.g., "user@example.com" -> "u***@example.com")
func maskEmailForLog(email string) string {
	if email == "" {
		return "[empty]"
	}
	atIdx := -1
	for i, c := range email {
		if c == '@' {
			atIdx = i
			break
		}
	}
	if atIdx == -1 {
		return "[invalid]"
	}
	local := email[:atIdx]
	domain := email[atIdx+1:]
	if len(local) <= 1 {
		return local + "***@" + domain
	}
	return string(local[0]) + "***@" + domain
}

func HandleSSOAuthRequest(requireEmailVerified bool) func(e *core.RecordAuthWithOAuth2RequestEvent) error {
	return func(e *core.RecordAuthWithOAuth2RequestEvent) error {
		if e.OAuth2User == nil {
			return e.Next()
		}

		// Require a true email_verified claim unless OIDC_REQUIRE_EMAIL_VERIFIED=false
		emailVerified, hasVerified := e.OAuth2User.RawUser["email_verified"].(bool)
		if requireEmailVerified && (!hasVerified || !emailVerified) {
			rawEmail, _ := e.OAuth2User.RawUser["email"].(string)
			log.Printf("[oidc] login rejected: email not verified for %s", maskEmailForLog(rawEmail))
			return fmt.Errorf("email address must be verified by the identity provider")
		}

		// Recover email from raw claims if PocketBase left it blank
		if e.OAuth2User.Email == "" {
			if raw, ok := e.OAuth2User.RawUser["email"].(string); ok && raw != "" {
				log.Printf("[oidc] email_verified missing/false — using raw email claim: %s", maskEmailForLog(raw))
				e.OAuth2User.Email = raw
			}
		}
		if e.OAuth2User.Email == "" {
			return fmt.Errorf("auth: provider did not return email claim; ensure email scope/claim is present")
		}

		resolvedRole, err := resolveSSORoleFromGroups(e.App, e.OAuth2User.RawUser)
		if err != nil {
			log.Printf("[oidc] login rejected: no matching SSO group role for %s: %v", maskEmailForLog(e.OAuth2User.Email), err)
			return err
		}

		if err := e.Next(); err != nil {
			return err
		}

		if e.Record != nil {
			e.Record.Set("role", resolvedRole)
			e.Record.Set("elevate_consumed", false)
			e.Record.Set("elevate_consumed_at", nil)

			if e.Record.Id != "" {
				if err := e.App.Save(e.Record); err != nil {
					log.Printf("[oidc] warning: failed to reset elevate state after successful auth: %v", err)
				}
			}
		}

		return nil
	}
}

func resolveSSORoleFromGroups(app core.App, rawClaims map[string]any) (string, error) {
	claimName := ssoGroupsClaimName(app)
	groups := extractSSOGroups(rawClaims[claimName])
	if len(groups) == 0 {
		return "", fmt.Errorf("no groups found in claim %q", claimName)
	}

	mappings, err := app.FindAllRecords("sso_group_roles")
	if err != nil {
		return "", err
	}
	groupSet := make(map[string]struct{}, len(groups))
	for _, group := range groups {
		groupSet[group] = struct{}{}
	}

	var matchedRoles []string
	for _, mapping := range mappings {
		if _, ok := groupSet[mapping.GetString("group")]; ok {
			matchedRoles = append(matchedRoles, mapping.GetString("role"))
		}
	}
	if len(matchedRoles) == 0 {
		return "", fmt.Errorf("no configured role mapping matched claim %q", claimName)
	}
	return rbac.HighestRole(matchedRoles...), nil
}

func ssoGroupsClaimName(app core.App) string {
	records, err := app.FindAllRecords("app_settings")
	if err == nil && len(records) > 0 {
		if claim := strings.TrimSpace(records[0].GetString("sso_groups_claim")); claim != "" {
			return claim
		}
	}
	return "groups"
}

func extractSSOGroups(raw any) []string {
	seen := map[string]struct{}{}
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value != "" {
			seen[value] = struct{}{}
		}
	}

	switch v := raw.(type) {
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				add(s)
			}
		}
	case []string:
		for _, item := range v {
			add(item)
		}
	case string:
		for _, item := range strings.FieldsFunc(v, func(r rune) bool {
			return r == ',' || r == ' '
		}) {
			add(item)
		}
	}

	groups := make([]string, 0, len(seen))
	for group := range seen {
		groups = append(groups, group)
	}
	return groups
}
