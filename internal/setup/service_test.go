package setup

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/wireops/wireops/internal/audit"
	_ "github.com/wireops/wireops/pb_migrations"
)

func newSetupServiceTestApp(t *testing.T) *tests.TestApp {
	t.Helper()

	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}

	ensureTestUsersRoleField(t, app)
	clearAllSuperusers(t, app)
	clearAllUsers(t, app)
	t.Cleanup(func() { app.Cleanup() })
	return app
}

func TestCreateInitialAdminCreatesAlignedUserAndSuperuser(t *testing.T) {
	app := newSetupServiceTestApp(t)
	service := NewService(app)

	if err := service.CreateInitialAdmin("first@example.com", "securepassword"); err != nil {
		t.Fatalf("expected bootstrap to succeed, got %v", err)
	}

	user, err := app.FindAuthRecordByEmail("users", "first@example.com")
	if err != nil {
		t.Fatalf("expected user to exist, got %v", err)
	}
	superuser, err := app.FindAuthRecordByEmail(core.CollectionNameSuperusers, "first@example.com")
	if err != nil {
		t.Fatalf("expected superuser to exist, got %v", err)
	}

	if user.GetString("role") != "admin" {
		t.Fatalf("expected admin role, got %q", user.GetString("role"))
	}
	if !user.GetBool("verified") {
		t.Fatal("expected initial admin to be verified")
	}
	if !user.GetBool("protected") {
		t.Fatal("expected initial admin to be protected")
	}
	if superuser.GetString("password:hash") != user.GetString("password:hash") {
		t.Fatal("expected user and superuser password hashes to match")
	}
	if superuser.GetString("tokenKey") != user.GetString("tokenKey") {
		t.Fatal("expected user and superuser token keys to match")
	}
	if !superuser.ValidatePassword("securepassword") {
		t.Fatal("expected superuser record to authenticate with the original plaintext password")
	}

	assertAuditEvent(t, app, "setup.bootstrap_started", "success", "", "f***@example.com")
	assertAuditEvent(t, app, "setup.bootstrap_completed", "success", "", "f***@example.com")
}

func TestCreateInitialAdminRollsBackOnSuperuserFailure(t *testing.T) {
	app := newSetupServiceTestApp(t)
	service := NewService(app)
	hookErr := errors.New("forced hook failure")
	service.afterUserCreate = func(*core.Record) error {
		return hookErr
	}

	err := service.CreateInitialAdmin("rollback@example.com", "securepassword")
	if !errors.Is(err, ErrBootstrapFailed) {
		t.Fatalf("expected bootstrap failure sentinel, got %v", err)
	}

	if _, userErr := app.FindAuthRecordByEmail("users", "rollback@example.com"); userErr == nil {
		t.Fatal("expected user creation to be rolled back")
	}
	if _, superErr := app.FindAuthRecordByEmail(core.CollectionNameSuperusers, "rollback@example.com"); superErr == nil {
		t.Fatal("expected superuser creation to be rolled back")
	}

	assertAuditEvent(t, app, "setup.bootstrap_started", "success", "", "r***@example.com")
	assertAuditEvent(t, app, "setup.bootstrap_failed", "error", "bootstrap_failed", "r***@example.com")
}

func TestCreateInitialAdminRejectsWhenAlreadyConfigured(t *testing.T) {
	app := newSetupServiceTestApp(t)
	service := NewService(app)

	user := createTestUser(t, app, "existing@example.com", "password123", "admin")
	if user == nil {
		t.Fatal("expected fixture admin user")
	}

	err := service.CreateInitialAdmin("second@example.com", "securepassword")
	if !errors.Is(err, ErrSetupAlreadyDone) {
		t.Fatalf("expected setup already done error, got %v", err)
	}

	if _, findErr := app.FindAuthRecordByEmail("users", "second@example.com"); findErr == nil {
		t.Fatal("expected second user not to be created")
	}

	assertAuditEvent(t, app, "setup.bootstrap_started", "success", "", "s***@example.com")
	assertAuditEvent(t, app, "setup.bootstrap_rejected", "error", "already_configured", "s***@example.com")
}

func TestCreateInitialAdminFailsWhenStatusCannotBeDetermined(t *testing.T) {
	app := newSetupServiceTestApp(t)
	service := NewService(app)

	if _, err := app.DB().NewQuery("DROP TABLE users").Execute(); err != nil {
		t.Fatalf("failed to drop users table: %v", err)
	}

	err := service.CreateInitialAdmin("broken@example.com", "securepassword")
	if !errors.Is(err, ErrBootstrapFailed) {
		t.Fatalf("expected bootstrap failure sentinel, got %v", err)
	}

	assertAuditEvent(t, app, "setup.bootstrap_started", "success", "", "b***@example.com")
	assertAuditEvent(t, app, "setup.bootstrap_failed", "error", "bootstrap_failed", "b***@example.com")
}

func TestCreateInitialAdminConcurrentAttempts(t *testing.T) {
	app := newSetupServiceTestApp(t)
	service := NewService(app)

	var successCount int32
	var alreadyDoneCount int32
	var unexpectedErr atomic.Value

	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		email := "race" + string(rune('0'+i)) + "@example.com"
		wg.Add(1)
		go func(email string) {
			defer wg.Done()
			<-start

			err := service.CreateInitialAdmin(email, "securepassword")
			switch {
			case err == nil:
				atomic.AddInt32(&successCount, 1)
			case errors.Is(err, ErrSetupAlreadyDone):
				atomic.AddInt32(&alreadyDoneCount, 1)
			default:
				unexpectedErr.Store(err)
			}
		}(email)
	}

	close(start)
	wg.Wait()

	if err, ok := unexpectedErr.Load().(error); ok {
		t.Fatalf("expected only success or setup already done, got %v", err)
	}
	if successCount != 1 {
		t.Fatalf("expected exactly one successful bootstrap, got %d", successCount)
	}
	if alreadyDoneCount != 1 {
		t.Fatalf("expected exactly one rejected bootstrap, got %d", alreadyDoneCount)
	}

	count, err := CountRealUsers(app)
	if err != nil {
		t.Fatalf("expected user count query to succeed, got %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly one real user, got %d", count)
	}
}

func clearAllSuperusers(t *testing.T, app core.App) {
	t.Helper()

	_, err := app.DB().
		NewQuery("DELETE FROM _superusers WHERE email != {:installer}").
		Bind(dbx.Params{"installer": core.DefaultInstallerEmail}).
		Execute()
	if err != nil {
		t.Fatalf("failed to clear superusers: %v", err)
	}
}

func clearAllUsers(t *testing.T, app core.App) {
	t.Helper()

	_, err := app.DB().NewQuery("DELETE FROM users").Execute()
	if err != nil {
		t.Fatalf("failed to clear users: %v", err)
	}
}

func ensureTestUsersRoleField(t *testing.T, app core.App) {
	t.Helper()

	col, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		return
	}

	changed := false
	if col.Fields.GetByName("role") == nil {
		col.Fields.Add(&core.SelectField{
			Name:      "role",
			Required:  true,
			MaxSelect: 1,
			Values:    []string{"viewer", "operator", "admin"},
		})
		changed = true
	}
	if col.Fields.GetByName("disabled") == nil {
		col.Fields.Add(&core.BoolField{Name: "disabled"})
		changed = true
	}
	if col.Fields.GetByName("protected") == nil {
		col.Fields.Add(&core.BoolField{Name: "protected", Hidden: true})
		changed = true
	}
	if changed {
		if err := app.Save(col); err != nil {
			t.Fatalf("failed to add fields to users fixture: %v", err)
		}
	}
}

func createTestUser(t *testing.T, app core.App, email, password, role string) *core.Record {
	t.Helper()

	col, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatalf("failed to find users collection: %v", err)
	}

	record := core.NewRecord(col)
	record.Set("email", email)
	record.Set("password", password)
	record.Set("role", role)
	record.Set("verified", true)
	if err := app.Save(record); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	return record
}

func assertAuditEvent(t *testing.T, app core.App, action, status, errorCode, maskedEmail string) {
	t.Helper()

	records, err := app.FindAllRecords("audit_logs", dbx.HashExp{"action": action})
	if err != nil {
		t.Fatalf("failed to query audit logs for %s: %v", action, err)
	}
	if len(records) == 0 {
		t.Fatalf("expected audit event %s to exist", action)
	}

	for _, rec := range records {
		if rec.GetString("status") != status {
			continue
		}
		if rec.GetString("error_code") != errorCode {
			continue
		}
		if rec.GetString("origin") != "setup" {
			continue
		}

		meta := audit.MetadataJSON(rec.Get("metadata_json"))
		if len(meta) == 0 {
			meta = audit.MetadataJSON(rec.GetString("metadata_json"))
		}
		if meta["email_masked"] == maskedEmail {
			return
		}
	}

	t.Fatalf("expected audit event %s with status=%q error_code=%q maskedEmail=%q", action, status, errorCode, maskedEmail)
}
