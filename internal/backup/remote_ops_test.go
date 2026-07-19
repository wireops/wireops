package backup

import (
	"context"
	"io"
	"strings"
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

// TestGetRemoteDecryptsBasedOnUploadTimeMetadataNotCurrentSetting guards
// against GetRemote picking its decryption path from the *current*
// encrypt_content setting: an object stays encrypted (or plain) as it was
// written, and toggling the setting afterward must not make GetRemote
// silently return ciphertext instead of decrypting it (see remote.EncryptedGet,
// which is metadata-driven).
func TestGetRemoteDecryptsBasedOnUploadTimeMetadataNotCurrentSetting(t *testing.T) {
	app := newS3TestApp(t)
	server := newFakeS3Server()
	defer server.Close()

	initialConfig := fakeS3Config(t, server, "wireops-backups")
	initialConfig["encrypt_content"] = true
	rec := saveS3IntegrationRecord(t, app, true, initialConfig)

	const key = "wireops_getremote_meta_test.zip"
	const plaintext = "this is the original backup content"

	if err := PutRemote(context.Background(), app, key, strings.NewReader(plaintext), int64(len(plaintext))); err != nil {
		t.Fatalf("PutRemote failed: %v", err)
	}

	// Disable encrypt_content after the upload — GetRemote must still know
	// (from the object's own metadata) that this object was encrypted, and
	// decrypt it, rather than returning raw ciphertext.
	config := fakeS3Config(t, server, "wireops-backups")
	config["encrypt_content"] = false
	rec.Set("config", config)
	if err := app.Save(rec); err != nil {
		t.Fatalf("update s3 integration config: %v", err)
	}

	body, err := GetRemote(context.Background(), app, key)
	if err != nil {
		t.Fatalf("GetRemote failed: %v", err)
	}
	defer body.Close()
	got, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read remote body: %v", err)
	}
	if string(got) != plaintext {
		t.Fatalf("expected decrypted content %q, got %q", plaintext, got)
	}
}

// TestGetRemoteReturnsPlaintextForUploadTimeUnencryptedObject is the mirror
// image of TestGetRemoteDecryptsBasedOnUploadTimeMetadataNotCurrentSetting:
// an object uploaded while encrypt_content was disabled carries no
// encryption metadata (metaEncryption == ""), so enabling the setting
// afterward must not make GetRemote try to decrypt it as ciphertext.
func TestGetRemoteReturnsPlaintextForUploadTimeUnencryptedObject(t *testing.T) {
	app := newS3TestApp(t)
	server := newFakeS3Server()
	defer server.Close()

	initialConfig := fakeS3Config(t, server, "wireops-backups")
	initialConfig["encrypt_content"] = false
	rec := saveS3IntegrationRecord(t, app, true, initialConfig)

	const key = "wireops_getremote_plain_meta_test.zip"
	const plaintext = "this is the original unencrypted backup content"

	if err := PutRemote(context.Background(), app, key, strings.NewReader(plaintext), int64(len(plaintext))); err != nil {
		t.Fatalf("PutRemote failed: %v", err)
	}

	// Enable encrypt_content after the upload — GetRemote must still know
	// (from the object's own metadata, or lack thereof) that this object
	// was never encrypted, and return it as-is rather than attempting to
	// decrypt plaintext as ciphertext.
	config := fakeS3Config(t, server, "wireops-backups")
	config["encrypt_content"] = true
	rec.Set("config", config)
	if err := app.Save(rec); err != nil {
		t.Fatalf("update s3 integration config: %v", err)
	}

	body, err := GetRemote(context.Background(), app, key)
	if err != nil {
		t.Fatalf("GetRemote failed: %v", err)
	}
	defer body.Close()
	got, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read remote body: %v", err)
	}
	if string(got) != plaintext {
		t.Fatalf("expected plaintext content %q, got %q", plaintext, got)
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
