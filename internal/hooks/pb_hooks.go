package hooks

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/go-git/go-git/v5/plumbing/transport"
	gogithttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	gogitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/jfxdev/wireops/internal/config"
	"github.com/jfxdev/wireops/internal/crypto"
	"github.com/jfxdev/wireops/internal/git"
	"github.com/jfxdev/wireops/internal/jobscheduler"
	"github.com/jfxdev/wireops/internal/sync"
)

func Register(app core.App, scheduler *sync.Scheduler, jobSched *jobscheduler.Scheduler) {
	secretKey := []byte(os.Getenv("SECRET_KEY"))

	// Encrypt credential fields on create/update
	app.OnRecordCreate("repository_keys").BindFunc(func(e *core.RecordEvent) error {
		encryptSensitiveFields(e.Record, secretKey)
		return e.Next()
	})

	app.OnRecordUpdate("repository_keys").BindFunc(func(e *core.RecordEvent) error {
		encryptSensitiveFields(e.Record, secretKey)
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
						}
					}
					if enc := rec.GetString("ssh_passphrase"); enc != "" {
						if dec, err := crypto.Decrypt(enc, secretKey); err == nil {
							cred.SSHPassphrase = dec
						}
					}
					cred.SSHKnownHost = rec.GetString("ssh_known_host")
				case git.AuthTypeBasic:
					cred.GitUsername = rec.GetString("git_username")
					if enc := rec.GetString("git_password"); enc != "" {
						if dec, err := crypto.Decrypt(enc, secretKey); err == nil {
							cred.GitPassword = string(dec)
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

			if _, err := git.CloneOrFetch(repoID, gitURL, branch, auth, workspace); err != nil {
				log.Printf("[hooks] background clone failed for repo %s: %v", repoID, err)
			} else {
				log.Printf("[hooks] background clone success for repo %s", repoID)
			}
		}()

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

	// Encrypt stack env var values on create/update only when secret=true
	app.OnRecordCreate("stack_env_vars").BindFunc(func(e *core.RecordEvent) error {
		if e.Record.GetBool("secret") {
			encryptField(e.Record, "value", secretKey)
		}
		return e.Next()
	})

	app.OnRecordUpdate("stack_env_vars").BindFunc(func(e *core.RecordEvent) error {
		if e.Record.GetBool("secret") {
			encryptField(e.Record, "value", secretKey)
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
				_ = app.Delete(r)
			}
		}
		envVars, err := app.FindAllRecords("job_env_vars", dbx.HashExp{"job": jobID})
		if err == nil {
			for _, r := range envVars {
				_ = app.Delete(r)
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
		return e.Next()
	})

	// Encrypt job env var values on create/update when secret=true
	app.OnRecordCreate("job_env_vars").BindFunc(func(e *core.RecordEvent) error {
		if e.Record.GetBool("secret") {
			encryptField(e.Record, "value", secretKey)
		}
		return e.Next()
	})

	app.OnRecordUpdate("job_env_vars").BindFunc(func(e *core.RecordEvent) error {
		if e.Record.GetBool("secret") {
			encryptField(e.Record, "value", secretKey)
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
</body></html>`, actionURL)
}

func encryptSensitiveFields(record *core.Record, key []byte) {
	if len(key) != 32 {
		log.Printf("[hooks] WARNING: SECRET_KEY is not 32 bytes, skipping encryption")
		return
	}
	for _, field := range []string{"ssh_private_key", "ssh_passphrase", "git_password"} {
		encryptField(record, field, key)
	}
}

func encryptField(record *core.Record, field string, key []byte) {
	if len(key) != 32 {
		return
	}
	val := record.GetString(field)
	if val == "" || val == "••••••••" {
		return
	}
	if crypto.IsEncrypted(val) {
		return
	}
	encrypted, err := crypto.Encrypt([]byte(val), key)
	if err != nil {
		log.Printf("[hooks] failed to encrypt field %s: %v", field, err)
		return
	}
	record.Set(field, encrypted)
}
