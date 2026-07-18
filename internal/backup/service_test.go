package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"testing"

	"github.com/pocketbase/pocketbase/tests"
	"github.com/pocketbase/pocketbase/tools/filesystem"
)

// buildTestZip returns bytes for a real, structurally-valid zip archive
// containing one small file, since validateZipMagic now parses the actual
// central directory rather than just checking the leading 4-byte signature.
func buildTestZip(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("data.txt")
	if err != nil {
		t.Fatalf("failed to add zip entry: %v", err)
	}
	if _, err := w.Write([]byte("test")); err != nil {
		t.Fatalf("failed to write zip entry: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("failed to close zip writer: %v", err)
	}
	return buf.Bytes()
}

func TestCreateListDelete(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	defer app.Cleanup()

	const name = "wireops_service_test.zip"
	if err := Create(context.Background(), app, name); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	list, err := List(app)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	found := false
	for _, info := range list {
		if info.Key == name {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected backup %q in list, got %+v", name, list)
	}

	if err := Delete(app, name); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	list, err = List(app)
	if err != nil {
		t.Fatalf("List after delete failed: %v", err)
	}
	for _, info := range list {
		if info.Key == name {
			t.Fatalf("expected backup %q to be removed, still present in %+v", name, list)
		}
	}
}

func TestRestoreRequiresConfirmation(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	defer app.Cleanup()

	if err := Restore(context.Background(), app, "some_backup.zip", false); err == nil {
		t.Fatal("expected error when confirm=false, got nil")
	}
}

func TestUploadRejectsNonZipContent(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	defer app.Cleanup()

	file, err := filesystem.NewFileFromBytes([]byte("not a zip file, just plain text"), "fake_backup.zip")
	if err != nil {
		t.Fatalf("failed to build test file: %v", err)
	}

	if err := Upload(app, file); err == nil {
		t.Fatal("expected error for non-zip content, got nil")
	}
}

func TestUploadAcceptsValidZip(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	defer app.Cleanup()

	zipBytes := buildTestZip(t)
	file, err := filesystem.NewFileFromBytes(zipBytes, "uploaded_backup.zip")
	if err != nil {
		t.Fatalf("failed to build test file: %v", err)
	}

	if err := Upload(app, file); err != nil {
		t.Fatalf("Upload failed: %v", err)
	}

	list, err := List(app)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	found := false
	for _, info := range list {
		if info.Key == "uploaded_backup.zip" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected uploaded backup in list, got %+v", list)
	}
}

func TestUploadRejectsOversizedFile(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	defer app.Cleanup()

	t.Setenv("BACKUP_UPLOAD_MAX_MB", "1")

	zipBytes := append([]byte{0x50, 0x4b, 0x03, 0x04}, make([]byte, 2*1024*1024)...)
	file, err := filesystem.NewFileFromBytes(zipBytes, "too_big.zip")
	if err != nil {
		t.Fatalf("failed to build test file: %v", err)
	}

	if err := Upload(app, file); err == nil {
		t.Fatal("expected error for oversized file, got nil")
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	defer app.Cleanup()

	s := GetSettings(app)
	s.Cron = "0 3 * * *"
	s.CronMaxKeep = 5
	if err := SaveSettings(app, s); err != nil {
		t.Fatalf("SaveSettings failed: %v", err)
	}

	got := GetSettings(app)
	if got.Cron != "0 3 * * *" || got.CronMaxKeep != 5 {
		t.Fatalf("settings did not persist: got %+v", got)
	}
}
