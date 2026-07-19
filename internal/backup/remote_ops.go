package backup

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"

	"github.com/wireops/wireops/internal/backup/remote"
	"github.com/wireops/wireops/internal/safepath"
)

// PutRemote uploads r (size bytes) under key to the configured S3
// integration, encrypting content first unless explicitly disabled (see
// remoteEncryptContent) — used both by Upload (operator-provided files) and
// MirrorLocalBackupToRemote (locally-created backups).
func PutRemote(ctx context.Context, app core.App, key string, r io.Reader, size int64) error {
	storage, err := buildRemoteStorage(app)
	if err != nil {
		return fmt.Errorf("failed to load remote backup storage: %w", err)
	}
	defer storage.Close()

	if err := storage.EnsurePrefix(ctx); err != nil {
		app.Logger().Warn("failed to ensure remote backup prefix", "error", err.Error())
	}

	if !remoteEncryptContent(app) {
		return storage.Put(ctx, key, r, size, nil)
	}

	km, err := buildRemoteKeyManager(app)
	if err != nil {
		return fmt.Errorf("failed to load KMS key manager: %w", err)
	}
	return remote.EncryptedPut(ctx, storage, key, r, secretKeyFromEnv(), km)
}

// GetRemote downloads key from the configured S3 integration, reversing
// whatever content encryption was applied at upload time.
//
// Which decryption (if any) to apply is decided from the object's own
// metadata (see remote.EncryptedGet), not from the current encrypt_content
// setting — an object stays encrypted (or plain) as it was written even if
// encrypt_content is later toggled, so decrypting on the current setting
// would either fail or silently return ciphertext.
func GetRemote(ctx context.Context, app core.App, key string) (io.ReadCloser, error) {
	storage, err := buildRemoteStorage(app)
	if err != nil {
		return nil, fmt.Errorf("failed to load remote backup storage: %w", err)
	}

	km, err := buildRemoteKeyManager(app)
	if err != nil {
		return nil, fmt.Errorf("failed to load KMS key manager: %w", err)
	}
	body, err := remote.EncryptedGet(ctx, storage, key, secretKeyFromEnv(), km)
	if err != nil {
		return nil, fmt.Errorf("failed to download remote backup: %w", err)
	}
	return body, nil
}

// MirrorLocalBackupToRemote replicates a local backup to remote storage (if
// enabled) — the local copy is never removed; remote storage is an
// additional copy, not a replacement. wireops's own S3 client does the
// upload instead of PocketBase's native one (see internal/backup/s3_integration.go).
//
// Bound to app.OnBackupCreate() (see internal/hooks), so it runs for both
// manual creation (backup.Create) and PocketBase's cron autobackup job —
// both call app.CreateBackup internally, and that hook fires either way.
// Also called directly from Upload for operator-provided files.
//
// attempted reports whether a mirror was actually attempted (remote storage
// enabled) — callers use it to decide whether to audit-log/notify, since a
// disabled remote is the normal, silent no-op case, not worth recording.
func MirrorLocalBackupToRemote(ctx context.Context, app core.App, name string) (attempted bool, err error) {
	if name == "" {
		return false, nil
	}
	enabled, err := remoteEnabled(app)
	if err != nil {
		return false, fmt.Errorf("failed to check remote backup storage: %w", err)
	}
	if !enabled {
		return false, nil
	}

	fsys, err := app.NewBackupsFilesystem()
	if err != nil {
		return true, fmt.Errorf("failed to load local backups filesystem: %w", err)
	}
	defer fsys.Close()

	reader, err := fsys.GetReader(name)
	if err != nil {
		return true, fmt.Errorf("failed to open local backup %q: %w", name, err)
	}
	defer reader.Close()

	if err := PutRemote(ctx, app, name, reader, reader.Size()); err != nil {
		return true, fmt.Errorf("failed to mirror backup %q to remote storage: %w", name, err)
	}
	return true, nil
}

// downloadRemoteToLocal fetches key from remote storage into fsys (the
// local backups filesystem) — used by Restore when the requested backup
// isn't present locally (the normal case once remote storage is enabled;
// see MirrorLocalBackupToRemote).
func downloadRemoteToLocal(ctx context.Context, app core.App, fsys *filesystem.System, key string) error {
	body, err := GetRemote(ctx, app, key)
	if err != nil {
		return err
	}
	defer body.Close()

	data, err := io.ReadAll(body)
	if err != nil {
		return fmt.Errorf("failed to read remote backup: %w", err)
	}
	return fsys.Upload(data, key)
}

// SyncLocal pulls a remote-only backup (uploaded straight into the bucket,
// or whose local copy was removed independently) down onto local disk, so
// it can be restored — core.App.RestoreBackup always reads locally, it
// never has a remote fallback of its own. A no-op if key is already local.
func SyncLocal(ctx context.Context, app core.App, key string) error {
	if err := safepath.ValidateBackupKey(key); err != nil {
		return err
	}

	fsys, err := app.NewBackupsFilesystem()
	if err != nil {
		return fmt.Errorf("failed to load local backups filesystem: %w", err)
	}
	defer fsys.Close()

	localExists, err := fsys.Exists(key)
	if err != nil {
		return fmt.Errorf("failed to check for existing local backup: %w", err)
	}
	if localExists {
		return nil
	}

	enabled, err := remoteEnabled(app)
	if err != nil {
		return fmt.Errorf("failed to check remote backup storage: %w", err)
	}
	if !enabled {
		return errors.New("remote backup storage is not enabled")
	}

	if err := downloadRemoteToLocal(ctx, app, fsys, key); err != nil {
		return fmt.Errorf("failed to fetch backup %q from remote storage: %w", key, err)
	}
	return nil
}
