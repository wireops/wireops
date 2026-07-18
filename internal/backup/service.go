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

// capacityMu serializes the "list backups, check against
// config.GetBackupMaxCount, then write" sequence in Create/Upload so two
// concurrent requests on this process can't both pass the check and jointly
// exceed the configured max. This only guards a single server process —
// wireops doesn't run multiple server instances against shared backup
// storage, so a cross-instance distributed lock isn't needed here.
var capacityMu sync.Mutex

// restoreMu guards pendingRestoreKeyVal, the key of a backup that Restore
// has committed to but whose actual app.RestoreBackup call hasn't started
// yet (it runs after a short delay in a goroutine — see Restore). Without
// this, Delete's existing core.StoreKeyActiveBackup check (which
// RestoreBackup itself sets) has a window between Restore() returning and
// the goroutine firing during which the same backup could be deleted out
// from under the pending restore.
var restoreMu sync.Mutex
var pendingRestoreKeyVal string

func setPendingRestoreKey(key string) {
	restoreMu.Lock()
	pendingRestoreKeyVal = key
	restoreMu.Unlock()
}

func pendingRestoreKey() string {
	restoreMu.Lock()
	defer restoreMu.Unlock()
	return pendingRestoreKeyVal
}

// Info describes one stored backup archive. Local and Remote are
// independent, additive flags (mirroring replicates, it never moves) — a
// backup can be Local-only, Local+Remote, or (rarely) Remote-only, e.g. one
// uploaded straight into the bucket, or whose local copy was removed
// independently. Remote-only backups need SyncLocal before they can be
// restored (core.App.RestoreBackup always reads from local disk).
type Info struct {
	Key      string    `json:"key"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	Local    bool      `json:"local"`
	Remote   bool      `json:"remote"`
}

// Settings mirrors the subset of app.Settings().Backups that wireops exposes
// for editing: cron schedule and retention. Off-host remote storage is
// configured separately, as the "s3" Storage Backend integration (see
// internal/integrations/s3 and s3_integration.go) — not part of this struct.
type Settings struct {
	Cron        string `json:"cron"`
	CronMaxKeep int    `json:"cron_max_keep"`
}

// GetSettings returns the current backup cron/retention configuration.
func GetSettings(app core.App) Settings {
	b := app.Settings().Backups
	return Settings{Cron: b.Cron, CronMaxKeep: b.CronMaxKeep}
}

// SaveSettings updates the backup cron/retention configuration. Saving
// triggers PocketBase's OnSettingsReload hook, which re-registers the
// autobackup cron entry (core.App.registerAutobackupHooks) — no custom
// scheduling code needed here.
func SaveSettings(app core.App, s Settings) error {
	settings := app.Settings()
	settings.Backups.Cron = s.Cron
	settings.Backups.CronMaxKeep = s.CronMaxKeep
	if err := app.Save(settings); err != nil {
		return fmt.Errorf("failed to save backup settings: %w", err)
	}
	return nil
}

// List returns every stored backup. Local disk is always the source of
// truth; when remote storage is enabled, each entry is additionally flagged
// Remote if it's also mirrored there — mirroring replicates the local copy,
// it doesn't replace it. A backup present only in remote storage (e.g.
// uploaded directly to the bucket, or its local copy removed independently)
// is still listed, Remote-only.
func List(app core.App) ([]Info, error) {
	fsys, err := app.NewBackupsFilesystem()
	if err != nil {
		return nil, fmt.Errorf("failed to load backups filesystem: %w", err)
	}
	defer fsys.Close()

	localObjects, err := fsys.List("")
	if err != nil {
		return nil, fmt.Errorf("failed to list backups: %w", err)
	}

	out := make([]Info, len(localObjects))
	localKeys := make(map[string]bool, len(localObjects))
	for i, obj := range localObjects {
		out[i] = Info{Key: obj.Key, Size: obj.Size, Modified: obj.ModTime, Local: true}
		localKeys[obj.Key] = true
	}

	enabled, err := remoteEnabled(app)
	if err != nil {
		return nil, fmt.Errorf("failed to check remote backup storage: %w", err)
	}
	if !enabled {
		return out, nil
	}

	storage, err := buildRemoteStorage(app)
	if err != nil {
		return nil, fmt.Errorf("failed to load remote backup storage: %w", err)
	}
	defer storage.Close()

	remoteObjects, err := storage.List(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to list remote backups: %w", err)
	}
	remoteKeys := make(map[string]bool, len(remoteObjects))
	for _, obj := range remoteObjects {
		remoteKeys[obj.Key] = true
	}
	for i := range out {
		out[i].Remote = remoteKeys[out[i].Key]
	}
	for _, obj := range remoteObjects {
		if !localKeys[obj.Key] {
			out = append(out, Info{Key: obj.Key, Size: obj.Size, Modified: obj.Modified, Remote: true})
		}
	}
	return out, nil
}

// countBackups returns how many distinct backups currently exist (local ∪
// remote), for the capacity check in Create/Upload.
func countBackups(app core.App) (int, error) {
	objects, err := List(app)
	if err != nil {
		return 0, err
	}
	return len(objects), nil
}

// existsBackup reports whether key already exists, locally or remotely.
func existsBackup(app core.App, key string) (bool, error) {
	objects, err := List(app)
	if err != nil {
		return false, err
	}
	for _, obj := range objects {
		if obj.Key == key {
			return true, nil
		}
	}
	return false, nil
}

// Create generates a new backup. name may be empty to auto-generate one.
//
// Manual creation has no automatic retention (unlike the cron job's
// cron_max_keep), so it's capped against config.GetBackupMaxCount to bound
// disk/S3 usage from repeated calls by any CapManageSettings user.
func Create(ctx context.Context, app core.App, name string) error {
	capacityMu.Lock()
	defer capacityMu.Unlock()

	count, err := countBackups(app)
	if err != nil {
		return fmt.Errorf("failed to count existing backups: %w", err)
	}
	if max := config.GetBackupMaxCount(); count >= max {
		return fmt.Errorf("maximum number of stored backups (%d) reached; delete an existing backup first", max)
	}

	if name != "" {
		if err := safepath.ValidateBackupKey(name); err != nil {
			return err
		}
		exists, err := existsBackup(app, name)
		if err != nil {
			return fmt.Errorf("failed to check for existing backup: %w", err)
		}
		if exists {
			return fmt.Errorf("a backup named %q already exists", name)
		}
	}
	// Always creates locally — PocketBase's own S3 backend is never enabled
	// (see s3_integration.go); MirrorLocalBackupToRemote, bound to
	// app.OnBackupCreate(), mirrors it off-host afterward if configured.
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

	capacityMu.Lock()
	defer capacityMu.Unlock()

	count, err := countBackups(app)
	if err != nil {
		return fmt.Errorf("failed to count existing backups: %w", err)
	}
	if max := config.GetBackupMaxCount(); count >= max {
		return fmt.Errorf("maximum number of stored backups (%d) reached; delete an existing backup first", max)
	}

	if exists, err := existsBackup(app, file.OriginalName); err != nil {
		return fmt.Errorf("failed to check for existing backup: %w", err)
	} else if exists {
		return fmt.Errorf("a backup named %q already exists", file.OriginalName)
	}

	// Always lands locally first, same as a server-generated backup — then
	// mirrored to remote storage if enabled, replicating rather than
	// replacing the local copy (see MirrorLocalBackupToRemote).
	fsys, err := app.NewBackupsFilesystem()
	if err != nil {
		return fmt.Errorf("failed to load backups filesystem: %w", err)
	}
	defer fsys.Close()
	if err := fsys.UploadFile(file, file.OriginalName); err != nil {
		return fmt.Errorf("failed to upload backup: %w", err)
	}

	if _, err := MirrorLocalBackupToRemote(context.Background(), app, file.OriginalName); err != nil {
		return fmt.Errorf("uploaded locally but failed to mirror to remote storage: %w", err)
	}
	return nil
}

// Delete removes a stored backup by key from every backend that holds a
// copy (local disk and, if enabled, remote storage) — mirroring replicates,
// so deletion must clean up both sides. Fails if a backup/restore is
// currently in progress and that operation is using this exact key.
func Delete(app core.App, key string) error {
	if err := safepath.ValidateBackupKey(key); err != nil {
		return err
	}

	if v, ok := app.Store().Get(core.StoreKeyActiveBackup).(string); ok && v == key {
		return errors.New("this backup is currently being used and cannot be deleted")
	}
	if pending := pendingRestoreKey(); pending == key {
		return errors.New("this backup is currently being used and cannot be deleted")
	}

	// Shares capacityMu with Create/Upload so a delete in progress can't
	// race a concurrent count-then-write on the other side and let the
	// configured max count be exceeded or under-counted.
	capacityMu.Lock()
	defer capacityMu.Unlock()

	fsys, err := app.NewBackupsFilesystem()
	if err != nil {
		return fmt.Errorf("failed to load backups filesystem: %w", err)
	}
	defer fsys.Close()

	localExists, err := fsys.Exists(key)
	if err != nil {
		return fmt.Errorf("failed to check for existing backup: %w", err)
	}
	if localExists {
		if err := fsys.Delete(key); err != nil {
			return fmt.Errorf("failed to delete backup: %w", err)
		}
	}

	enabled, err := remoteEnabled(app)
	if err != nil {
		return fmt.Errorf("failed to check remote backup storage: %w", err)
	}
	if !enabled {
		if !localExists {
			return fmt.Errorf("backup %q not found", key)
		}
		return nil
	}

	storage, err := buildRemoteStorage(app)
	if err != nil {
		return fmt.Errorf("failed to load remote backup storage: %w", err)
	}
	defer storage.Close()
	if err := storage.Delete(context.Background(), key); err != nil {
		return fmt.Errorf("failed to delete remote backup: %w", err)
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
		// A backup can be remote-only for two reasons: its local copy was
		// removed independently after mirroring (MirrorLocalBackupToRemote
		// never deletes it itself), or it was uploaded straight into the
		// bucket and never existed locally. Either way, fetch it back from
		// remote into the local backups dir so PocketBase's own
		// RestoreBackup — which always reads locally, see internal/backup/remote_ops.go
		// doc comment — can find it.
		enabled, err := remoteEnabled(app)
		if err != nil {
			return fmt.Errorf("failed to check remote backup storage: %w", err)
		}
		if !enabled {
			return fmt.Errorf("backup %q not found", key)
		}
		if err := downloadRemoteToLocal(ctx, app, fsys, key); err != nil {
			return fmt.Errorf("failed to fetch backup %q from remote storage: %w", key, err)
		}
	}

	setPendingRestoreKey(key)
	go func() {
		defer setPendingRestoreKey("")
		time.Sleep(1 * time.Second)
		restoreCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if err := app.RestoreBackup(restoreCtx, key); err != nil {
			app.Logger().Error("failed to restore backup", "key", key, "error", err.Error())
		}
	}()
	return nil
}
