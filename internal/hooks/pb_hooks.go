package hooks

import (
	"context"
	"fmt"
	"html"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/go-git/go-git/v5/plumbing/transport"
	gogithttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	gogitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"gopkg.in/yaml.v3"

	"github.com/wireops/wireops/internal/config"
	"github.com/wireops/wireops/internal/crypto"
	"github.com/wireops/wireops/internal/git"
	"github.com/wireops/wireops/internal/jobscheduler"
	"github.com/wireops/wireops/internal/safepath"
	"github.com/wireops/wireops/internal/sync"
)

func Register(app core.App, scheduler *sync.Scheduler, jobSched *jobscheduler.Scheduler) {
	secretKey := []byte(os.Getenv("SECRET_KEY"))

	// Encrypt credential fields on create/update
	app.OnRecordCreate("repository_keys").BindFunc(func(e *core.RecordEvent) error {
		if err := encryptSensitiveFields(e.Record, secretKey); err != nil {
			return err
		}
		return e.Next()
	})

	app.OnRecordUpdate("repository_keys").BindFunc(func(e *core.RecordEvent) error {
		if err := encryptSensitiveFields(e.Record, secretKey); err != nil {
			return err
		}
		return e.Next()
	})

	// Repository hooks
	app.OnRecordAfterCreateSuccess("repositories").BindFunc(func(e *core.RecordEvent) error {
		repoID := e.Record.Id
		gitURL := e.Record.GetString("git_url")
		branch := e.Record.GetString("branch")
		if branch == "" {
			branch = "main"
		}

		workspace := filepath.Join(app.DataDir(), "repositories")

		go func() {
			// Small delay to allow nested records (like credentials) to be saved if created in a transaction
			time.Sleep(1 * time.Second)

			// Try to load auth
			var auth transport.AuthMethod
			records, err := app.FindAllRecords("repository_keys", dbx.HashExp{"repository": repoID})
			if err == nil && len(records) > 0 {
				rec := records[0]
				authType := git.AuthType(rec.GetString("auth_type"))
				cred := &git.Credential{AuthType: authType}
				switch authType {
				case git.AuthTypeSSH:
					if enc := rec.GetString("ssh_private_key"); enc != "" {
						if dec, err := crypto.Decrypt(enc, secretKey); err == nil {
							cred.SSHPrivateKey = dec
						} else {
							log.Printf("failed to decrypt ssh_private_key: %v", err)
						}
					}
					if enc := rec.GetString("ssh_passphrase"); enc != "" {
						if dec, err := crypto.Decrypt(enc, secretKey); err == nil {
							cred.SSHPassphrase = dec
						} else {
							log.Printf("failed to decrypt ssh_passphrase: %v", err)
						}
					}
					cred.SSHKnownHost = rec.GetString("ssh_known_host")
				case git.AuthTypeBasic:
					cred.GitUsername = rec.GetString("git_username")
					if enc := rec.GetString("git_password"); enc != "" {
						if dec, err := crypto.Decrypt(enc, secretKey); err == nil {
							cred.GitPassword = string(dec)
						} else {
							log.Printf("failed to decrypt git_password: %v", err)
						}
					}
				}
				if resolvedAuth, err := git.ResolveAuth(*cred); err == nil {
					switch v := resolvedAuth.(type) {
					case *gogitssh.PublicKeys:
						auth = v
					case *gogithttp.BasicAuth:
						auth = v
					}
				}
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			done := make(chan error, 1)
			go func() {
				_, err := git.CloneOrFetch(repoID, gitURL, branch, auth, workspace)
				done <- err
			}()
			
			select {
			case <-ctx.Done():
				log.Printf("[hooks] background clone timed out for repo %s", repoID)
			case err := <-done:
				if err != nil {
					log.Printf("[hooks] background clone failed for repo %s: %v", repoID, err)
				} else {
					log.Printf("[hooks] background clone success for repo %s", repoID)
				}
			}
		}()

		return e.Next()
	})

	app.OnRecordDelete("repositories").BindFunc(func(e *core.RecordEvent) error {
		records, err := app.FindAllRecords("stacks", dbx.HashExp{"repository": e.Record.Id})
		if err == nil && len(records) > 0 {
			return fmt.Errorf("cannot delete repository because it has %d associated stack(s)", len(records))
		}
		keys, err := app.FindAllRecords("repository_keys", dbx.HashExp{"repository": e.Record.Id})
		if err != nil {
			return fmt.Errorf("failed to list repository_keys for repository %s: %w", e.Record.Id, err)
		}
		for _, k := range keys {
			if err := app.Delete(k); err != nil {
				return fmt.Errorf("failed to delete repository_key %s: %w", k.Id, err)
			}
		}
		return e.Next()
	})

	app.OnRecordAfterDeleteSuccess("repositories").BindFunc(func(e *core.RecordEvent) error {
		repoDir := filepath.Join(app.DataDir(), "repositories", e.Record.Id)
		if err := os.RemoveAll(repoDir); err != nil {
			log.Printf("[hooks] failed to remove repo directory %s: %v", repoDir, err)
		}
		return e.Next()
	})

	app.OnRecordEnrich("repositories").BindFunc(func(e *core.RecordEnrichEvent) error {
		repoDir := filepath.Join(app.DataDir(), "repositories", e.Record.Id)
		_, err := os.Stat(filepath.Join(repoDir, ".git"))
		e.Record.Set("is_cloned", err == nil)
		e.Record.WithCustomData(true)
		return e.Next()
	})

	// Stacks trigger scheduler registration
	app.OnRecordAfterCreateSuccess("stacks").BindFunc(func(e *core.RecordEvent) error {
		scheduler.RegisterStack(e.Record)
		scheduler.TriggerSync(e.Record.Id, "webhook", 0)
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
				base := filepath.Join(app.DataDir(), "repositories", repoID)

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
		if e.Record.GetBool("secret") {
			if err := encryptField(e.Record, "value", secretKey); err != nil {
				return err
			}
		}
		return e.Next()
	})

	app.OnRecordUpdate("stack_env_vars").BindFunc(func(e *core.RecordEvent) error {
		if e.Record.GetBool("secret") {
			if err := encryptField(e.Record, "value", secretKey); err != nil {
				return err
			}
		}
		return e.Next()
	})

	// Mask secret env var values on API responses
	app.OnRecordEnrich("stack_env_vars").BindFunc(func(e *core.RecordEnrichEvent) error {
		if e.Record.GetBool("secret") {
			e.Record.Set("value", "")
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
		return e.Next()
	})

	// Encrypt job env var values on create/update when secret=true
	app.OnRecordCreate("job_env_vars").BindFunc(func(e *core.RecordEvent) error {
		if e.Record.GetBool("secret") {
			if err := encryptField(e.Record, "value", secretKey); err != nil {
				return err
			}
		}
		return e.Next()
	})

	app.OnRecordUpdate("job_env_vars").BindFunc(func(e *core.RecordEvent) error {
		if e.Record.GetBool("secret") {
			if err := encryptField(e.Record, "value", secretKey); err != nil {
				return err
			}
		}
		return e.Next()
	})

	// Mask secret job env var values on API responses
	app.OnRecordEnrich("job_env_vars").BindFunc(func(e *core.RecordEnrichEvent) error {
		if e.Record.GetBool("secret") {
			e.Record.Set("value", "")
		}
		return e.Next()
	})

	// Override password reset email to point to custom frontend route
	app.OnMailerRecordPasswordResetSend("_superusers").BindFunc(func(e *core.MailerRecordEvent) error {
		token, _ := e.Meta["token"].(string)
		if token == "" {
			return e.Next()
		}
		actionURL := config.GetAppURL() + "/reset-password?token=" + token
		e.Message.Subject = "Reset your wireops password"
		e.Message.HTML = buildPasswordResetEmailHTML(actionURL)
		e.Message.Text = "Reset your wireops password by visiting: " + actionURL
		return e.Next()
	})
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
