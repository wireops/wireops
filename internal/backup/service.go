// Package backup wraps PocketBase's built-in core.App backup primitives
// (create/list/delete/restore, S3-compatible remote storage, cron autobackup
// + retention) behind wireops's own RBAC instead of PocketBase's superuser
// auth, so wireops admins who aren't PocketBase superusers can manage
// backups without reaching the native /_/ dashboard or /api/backups API.
//
// No storage, retention, or scheduling logic lives here — it all defers to
// core.App / app.Settings().Backups, which PocketBase already implements.
package backup

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"

	"github.com/wireops/wireops/internal/config"
	"github.com/wireops/wireops/internal/crypto"
	"github.com/wireops/wireops/internal/notify"
	"github.com/wireops/wireops/internal/safepath"
)

// seekerReaderAt adapts an io.ReadSeeker (what filesystem.File.Reader.Open
// gives us) into an io.ReaderAt for archive/zip, which needs random access
// to read the central directory. Only used sequentially by
// validateZipMagic, so the shared seek position under a mutex is safe.
type seekerReaderAt struct {
	mu sync.Mutex
	rs io.ReadSeeker
}

func (s *seekerReaderAt) ReadAt(p []byte, off int64) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.rs.Seek(off, io.SeekStart); err != nil {
		return 0, err
	}
	return io.ReadFull(s.rs, p)
}

// validateZipMagic opens the uploaded file as an actual zip archive (central
// directory + all local file headers), not just a 4-byte signature check —
// a truncated/corrupted archive can start with a valid local file header and
// still fail structurally, which a magic-byte check alone would miss and
// only surface later, silently, in Restore's background goroutine.
func validateZipMagic(file *filesystem.File) error {
	reader, err := file.Reader.Open()
	if err != nil {
		return fmt.Errorf("failed to read uploaded file: %w", err)
	}
	defer reader.Close()

	zr, err := zip.NewReader(&seekerReaderAt{rs: reader}, file.Size)
	if err != nil {
		return errors.New("uploaded file is not a valid zip archive")
	}
	if len(zr.File) == 0 {
		return errors.New("uploaded file is an empty zip archive")
	}
	return nil
}

func secretKeyFromEnv() []byte {
	return crypto.NormalizeSecretKey(os.Getenv("SECRET_KEY"))
}

// Info describes one stored backup archive.
type Info struct {
	Key      string    `json:"key"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}

// Settings mirrors the subset of app.Settings().Backups that wireops exposes
// for editing: cron schedule, retention, and optional S3 target.
type Settings struct {
	Cron        string        `json:"cron"`
	CronMaxKeep int           `json:"cron_max_keep"`
	S3          core.S3Config `json:"s3"`
}

// List returns every stored backup, most recent first.
func List(app core.App) ([]Info, error) {
	fsys, err := app.NewBackupsFilesystem()
	if err != nil {
		return nil, fmt.Errorf("failed to load backups filesystem: %w", err)
	}
	defer fsys.Close()

	objects, err := fsys.List("")
	if err != nil {
		return nil, fmt.Errorf("failed to list backups: %w", err)
	}

	out := make([]Info, len(objects))
	for i, obj := range objects {
		out[i] = Info{Key: obj.Key, Size: obj.Size, Modified: obj.ModTime}
	}
	return out, nil
}

// Create generates a new backup. name may be empty to auto-generate one.
//
// Manual creation has no automatic retention (unlike the cron job's
// cron_max_keep), so it's capped against config.GetBackupMaxCount to bound
// disk/S3 usage from repeated calls by any CapManageSettings user.
func Create(ctx context.Context, app core.App, name string) error {
	fsys, err := app.NewBackupsFilesystem()
	if err != nil {
		return fmt.Errorf("failed to load backups filesystem: %w", err)
	}
	defer fsys.Close()

	objects, err := fsys.List("")
	if err != nil {
		return fmt.Errorf("failed to list backups: %w", err)
	}
	if max := config.GetBackupMaxCount(); len(objects) >= max {
		return fmt.Errorf("maximum number of stored backups (%d) reached; delete an existing backup first", max)
	}

	if name != "" {
		exists, err := fsys.Exists(name)
		if err != nil {
			return fmt.Errorf("failed to check for existing backup: %w", err)
		}
		if exists {
			return fmt.Errorf("a backup named %q already exists", name)
		}
	}
	if err := app.CreateBackup(ctx, name); err != nil {
		return fmt.Errorf("failed to create backup: %w", err)
	}
	return nil
}

// Upload stores an operator-provided backup archive so it can later be
// restored like any server-generated one. Callers must gate this behind a
// real PocketBase superuser session (rbac.RequireSuperuser), not just a
// wireops admin role — an uploaded file becomes a future full-data-replace
// restore target, so accepting arbitrary uploads deserves a stricter bar
// than routine backup management.
func Upload(app core.App, file *filesystem.File) error {
	if file == nil {
		return errors.New("missing file")
	}
	if err := safepath.ValidateBackupKey(file.OriginalName); err != nil {
		return err
	}
	maxBytes := config.GetBackupUploadMaxBytes()
	if file.Size > maxBytes {
		return fmt.Errorf("uploaded file is %d bytes, exceeds the %d byte limit", file.Size, maxBytes)
	}
	if err := validateZipMagic(file); err != nil {
		return err
	}

	fsys, err := app.NewBackupsFilesystem()
	if err != nil {
		return fmt.Errorf("failed to load backups filesystem: %w", err)
	}
	defer fsys.Close()

	objects, err := fsys.List("")
	if err != nil {
		return fmt.Errorf("failed to list backups: %w", err)
	}
	if max := config.GetBackupMaxCount(); len(objects) >= max {
		return fmt.Errorf("maximum number of stored backups (%d) reached; delete an existing backup first", max)
	}

	if exists, err := fsys.Exists(file.OriginalName); err != nil {
		return fmt.Errorf("failed to check for existing backup: %w", err)
	} else if exists {
		return fmt.Errorf("a backup named %q already exists", file.OriginalName)
	}

	if err := fsys.UploadFile(file, file.OriginalName); err != nil {
		return fmt.Errorf("failed to upload backup: %w", err)
	}
	return nil
}

// Delete removes a stored backup by key. Fails if a backup/restore is
// currently in progress and that operation is using this exact key.
func Delete(app core.App, key string) error {
	if err := safepath.ValidateBackupKey(key); err != nil {
		return err
	}

	fsys, err := app.NewBackupsFilesystem()
	if err != nil {
		return fmt.Errorf("failed to load backups filesystem: %w", err)
	}
	defer fsys.Close()

	if v, ok := app.Store().Get(core.StoreKeyActiveBackup).(string); ok && v == key {
		return errors.New("this backup is currently being used and cannot be deleted")
	}

	if err := fsys.Delete(key); err != nil {
		return fmt.Errorf("failed to delete backup: %w", err)
	}
	return nil
}

// Restore verifies key exists and restarts the app process to restore it
// (see core.App.RestoreBackup). Before doing so it verifies the running
// SECRET_KEY still decrypts this host's own secret_key_canary row, catching
// a SECRET_KEY that was rotated on this host without re-encrypting existing
// stack secrets. It canNOT catch a restore onto a different host with a
// mismatched SECRET_KEY — that check runs against this host's pre-restore
// state, before the incoming archive's data (and its own canary) is even in
// play. That case is only caught after the restore restarts the process, by
// the same canary check in cmd/serve.go's OnServe hook, now evaluated
// against the just-restored data.
//
// confirm must be true — callers (the route layer) are responsible for
// collecting explicit operator confirmation before calling this, since it
// is destructive and irreversible without another backup.
//
// The actual restore + restart is dispatched in the background after a
// short delay, mirroring PocketBase's own native restore handler
// (apis/backup.go:backupRestore): RestoreBackup restarts the process as
// part of the same call, which would otherwise tear down the server mid
// HTTP-request and make the caller's request fail with a network error
// even though the restore itself succeeded.
func Restore(ctx context.Context, app core.App, key string, confirm bool) error {
	if !confirm {
		return errors.New("restore requires explicit confirmation")
	}
	if err := safepath.ValidateBackupKey(key); err != nil {
		return err
	}

	if err := crypto.VerifyOrSeedSecretKeyCanary(app, secretKeyFromEnv()); err != nil {
		return fmt.Errorf("refusing to restore: %w", err)
	}

	fsys, err := app.NewBackupsFilesystem()
	if err != nil {
		return fmt.Errorf("failed to load backups filesystem: %w", err)
	}
	defer fsys.Close()
	exists, err := fsys.Exists(key)
	if err != nil {
		return fmt.Errorf("failed to check for backup %q: %w", key, err)
	}
	if !exists {
		return fmt.Errorf("backup %q not found", key)
	}

	go func() {
		time.Sleep(1 * time.Second)
		restoreCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if err := app.RestoreBackup(restoreCtx, key); err != nil {
			app.Logger().Error("failed to restore backup", "key", key, "error", err.Error())
		}
	}()
	return nil
}

// maskedSecretPlaceholder is what GetSettings returns in place of a real S3
// secret key, and the sentinel SaveSettings looks for to know the caller
// left the field untouched (mirrors the integrations config pattern in
// internal/routes/routes_register.go: maskIntegrationConfig / MaskSecret).
var maskedSecretPlaceholder = notify.MaskSecret("x")

// GetSettings returns the current backup cron/retention/S3 configuration.
// The S3 secret key is masked — it must never round-trip to the client in
// plaintext, unlike every other secret-bearing settings payload in this
// codebase (see maskIntegrationConfig for the equivalent integrations-side
// pattern).
func GetSettings(app core.App) Settings {
	b := app.Settings().Backups
	s3 := b.S3
	s3.Secret = notify.MaskSecret(s3.Secret)
	return Settings{Cron: b.Cron, CronMaxKeep: b.CronMaxKeep, S3: s3}
}

// SaveSettings updates the backup cron/retention/S3 configuration.
// Saving triggers PocketBase's OnSettingsReload hook, which re-registers the
// autobackup cron entry (core.App.registerAutobackupHooks) — no custom
// scheduling code needed here.
//
// If the incoming S3 secret is the masked placeholder (the client echoed
// back what GetSettings gave it without editing it), the existing stored
// secret is preserved instead of being overwritten with the literal
// placeholder string — otherwise every settings save that doesn't touch the
// S3 secret field would silently brick S3 backups.
func SaveSettings(app core.App, s Settings) error {
	settings := app.Settings()
	if s.S3.Secret == maskedSecretPlaceholder {
		s.S3.Secret = settings.Backups.S3.Secret
	}
	settings.Backups.Cron = s.Cron
	settings.Backups.CronMaxKeep = s.CronMaxKeep
	settings.Backups.S3 = s.S3
	if err := app.Save(settings); err != nil {
		return fmt.Errorf("failed to save backup settings: %w", err)
	}
	return nil
}
