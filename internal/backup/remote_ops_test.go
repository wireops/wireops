package backup

import (
	"context"
	"testing"
)

func TestMirrorLocalBackupToRemoteNoopWhenDisabled(t *testing.T) {
	app := newS3TestApp(t)

	if err := Create(context.Background(), app, "wireops_mirror_test.zip"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	attempted, err := MirrorLocalBackupToRemote(context.Background(), app, "wireops_mirror_test.zip")
	if err != nil {
		t.Fatalf("expected no error when remote storage is disabled, got %v", err)
	}
	if attempted {
		t.Fatal("expected no mirror attempt when remote storage is disabled")
	}
}

func TestMirrorLocalBackupToRemoteAttemptedAndFailsWithBadConfig(t *testing.T) {
	app := newS3TestApp(t)

	if err := Create(context.Background(), app, "wireops_mirror_fail_test.zip"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Enabled but missing required fields (bucket/region/access_key/secret) —
	// buildRemoteStorage/remote.New fails without any real network call, so
	// this exercises the "attempted but failed" path deterministically.
	saveS3Integration(t, app, true, map[string]any{"bucket": "b"})

	attempted, err := MirrorLocalBackupToRemote(context.Background(), app, "wireops_mirror_fail_test.zip")
	if !attempted {
		t.Fatal("expected a mirror attempt once remote storage is enabled")
	}
	if err == nil {
		t.Fatal("expected an error for an incomplete s3 integration config")
	}

	// The local copy must survive a failed mirror — deleting it happens only
	// after a successful upload. List() itself now routes through the
	// (broken) remote config, so check local disk directly instead.
	fsys, fsErr := app.NewBackupsFilesystem()
	if fsErr != nil {
		t.Fatalf("failed to load local backups filesystem: %v", fsErr)
	}
	defer fsys.Close()
	exists, existsErr := fsys.Exists("wireops_mirror_fail_test.zip")
	if existsErr != nil {
		t.Fatalf("failed to check local backup: %v", existsErr)
	}
	if !exists {
		t.Fatal("expected the local backup to survive a failed mirror attempt")
	}
}
