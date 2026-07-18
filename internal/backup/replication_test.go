package backup

import (
	"context"
	"testing"
)

func TestReplicationKeepsLocalCopyAndFlagsRemoteInList(t *testing.T) {
	app := newS3TestApp(t)
	server := newFakeS3Server()
	defer server.Close()

	saveS3Integration(t, app, true, fakeS3Config(t, server, "wireops-backups"))

	const name = "wireops_replication_test.zip"
	if err := Create(context.Background(), app, name); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	attempted, err := MirrorLocalBackupToRemote(context.Background(), app, name)
	if !attempted {
		t.Fatal("expected a mirror attempt with remote storage enabled")
	}
	if err != nil {
		t.Fatalf("MirrorLocalBackupToRemote failed: %v", err)
	}

	// Local copy must still be there — mirroring replicates, it doesn't move.
	fsys, fsErr := app.NewBackupsFilesystem()
	if fsErr != nil {
		t.Fatalf("failed to load local backups filesystem: %v", fsErr)
	}
	exists, existsErr := fsys.Exists(name)
	fsys.Close()
	if existsErr != nil {
		t.Fatalf("failed to check local backup: %v", existsErr)
	}
	if !exists {
		t.Fatal("expected the local copy to survive a successful mirror")
	}

	if server.objectCount() == 0 {
		t.Fatal("expected the fake S3 server to have received the mirrored object")
	}

	list, err := List(app)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	var found *Info
	for i := range list {
		if list[i].Key == name {
			found = &list[i]
		}
	}
	if found == nil {
		t.Fatalf("expected %q in list, got %+v", name, list)
	}
	if !found.Remote {
		t.Fatalf("expected %q to be flagged Remote after a successful mirror, got %+v", name, found)
	}
}

func TestReplicationDeleteRemovesBothCopies(t *testing.T) {
	app := newS3TestApp(t)
	server := newFakeS3Server()
	defer server.Close()

	saveS3Integration(t, app, true, fakeS3Config(t, server, "wireops-backups"))

	const name = "wireops_replication_delete_test.zip"
	if err := Create(context.Background(), app, name); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if _, err := MirrorLocalBackupToRemote(context.Background(), app, name); err != nil {
		t.Fatalf("MirrorLocalBackupToRemote failed: %v", err)
	}

	if err := Delete(app, name); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	fsys, fsErr := app.NewBackupsFilesystem()
	if fsErr != nil {
		t.Fatalf("failed to load local backups filesystem: %v", fsErr)
	}
	exists, existsErr := fsys.Exists(name)
	fsys.Close()
	if existsErr != nil {
		t.Fatalf("failed to check local backup: %v", existsErr)
	}
	if exists {
		t.Fatal("expected the local copy to be removed by Delete")
	}

	list, err := List(app)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	for _, info := range list {
		if info.Key == name {
			t.Fatalf("expected %q to be fully removed, still present in %+v", name, list)
		}
	}
}

func TestSyncLocalFetchesRemoteOnlyBackup(t *testing.T) {
	app := newS3TestApp(t)
	server := newFakeS3Server()
	defer server.Close()

	saveS3Integration(t, app, true, fakeS3Config(t, server, "wireops-backups"))

	const name = "wireops_sync_local_test.zip"
	if err := Create(context.Background(), app, name); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if _, err := MirrorLocalBackupToRemote(context.Background(), app, name); err != nil {
		t.Fatalf("MirrorLocalBackupToRemote failed: %v", err)
	}

	// Simulate a remote-only backup: local copy removed independently of
	// backup.Delete (e.g. manual disk cleanup, or an object uploaded
	// straight into the bucket and never created locally).
	fsys, fsErr := app.NewBackupsFilesystem()
	if fsErr != nil {
		t.Fatalf("failed to load local backups filesystem: %v", fsErr)
	}
	if err := fsys.Delete(name); err != nil {
		t.Fatalf("failed to remove local copy: %v", err)
	}
	fsys.Close()

	list, err := List(app)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	var before *Info
	for i := range list {
		if list[i].Key == name {
			before = &list[i]
		}
	}
	if before == nil || before.Local || !before.Remote {
		t.Fatalf("expected %q to be remote-only before sync, got %+v", name, before)
	}

	if err := SyncLocal(context.Background(), app, name); err != nil {
		t.Fatalf("SyncLocal failed: %v", err)
	}

	fsys, fsErr = app.NewBackupsFilesystem()
	if fsErr != nil {
		t.Fatalf("failed to load local backups filesystem: %v", fsErr)
	}
	exists, existsErr := fsys.Exists(name)
	fsys.Close()
	if existsErr != nil {
		t.Fatalf("failed to check local backup: %v", existsErr)
	}
	if !exists {
		t.Fatal("expected SyncLocal to have restored the local copy")
	}
}

func TestSyncLocalNoopWhenAlreadyLocal(t *testing.T) {
	app := newS3TestApp(t)

	const name = "wireops_sync_local_noop_test.zip"
	if err := Create(context.Background(), app, name); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if err := SyncLocal(context.Background(), app, name); err != nil {
		t.Fatalf("expected no error for an already-local backup, got %v", err)
	}
}

func TestSyncLocalErrorsWhenRemoteDisabledAndNotLocal(t *testing.T) {
	app := newS3TestApp(t)

	if err := SyncLocal(context.Background(), app, "does_not_exist.zip"); err == nil {
		t.Fatal("expected an error when the backup isn't local and remote storage isn't enabled")
	}
}
