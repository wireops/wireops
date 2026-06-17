package routes

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/pocketbase/pocketbase/core"

	"github.com/wireops/wireops/pkg/dbcheck"
)

func TestDatabaseConsistencyTestOnlyRouteDoesNotRequireAuth(t *testing.T) {
	app := newSetupTestApp(t)

	// Delete a required collection to make the database inconsistent for this test
	col, err := app.FindCollectionByNameOrId("invites")
	if err == nil && col != nil {
		if err := app.Delete(col); err != nil {
			t.Fatalf("failed to delete invites collection: %v", err)
		}
	}

	rec := callHandler(t, app, http.MethodGet, "/api/custom/db/consistency", nil, testOnlyDatabaseConsistencyHandler(app))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 for incomplete test database, got %d", rec.Code)
	}

	var result dbcheck.Result
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result.OK {
		t.Fatal("expected incomplete test database to be reported as not ok")
	}
	if result.IssueCount == 0 {
		t.Fatal("expected at least one consistency issue")
	}
}

func testOnlyDatabaseConsistencyHandler(app core.App) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		result := dbcheck.Validate(app)
		status := http.StatusOK
		if !result.OK {
			status = http.StatusServiceUnavailable
		}
		return e.JSON(status, result)
	}
}
