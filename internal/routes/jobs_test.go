package routes

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/pocketbase/pocketbase/core"
)

func createTestRepository(t *testing.T, app core.App, name string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("repositories")
	if err != nil {
		t.Fatalf("find repositories collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("name", name)
	rec.Set("git_url", "https://example.com/"+name+".git")
	if err := app.Save(rec); err != nil {
		t.Fatalf("save repository: %v", err)
	}
	return rec
}

func createTestScheduledJob(t *testing.T, app core.App, repoID, name, status string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("scheduled_jobs")
	if err != nil {
		t.Fatalf("find scheduled_jobs collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("repository", repoID)
	rec.Set("job_file", "jobs/"+name+".yaml")
	rec.Set("name", name)
	rec.Set("status", status)
	rec.Set("enabled", true)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save scheduled_job: %v", err)
	}
	return rec
}

func decodeJobListResponse(t *testing.T, body []byte) jobListResponse {
	t.Helper()
	var resp jobListResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode response: %v\nbody: %s", err, body)
	}
	return resp
}

func TestListJobsPaginates(t *testing.T) {
	app := newSetupTestApp(t)
	repo := createTestRepository(t, app, "repo-a")
	createTestScheduledJob(t, app, repo.Id, "job-1", "active")
	createTestScheduledJob(t, app, repo.Id, "job-2", "active")
	createTestScheduledJob(t, app, repo.Id, "job-3", "active")

	rec := callHandler(t, app, http.MethodGet, "/api/custom/jobs?page=1&per_page=2", nil, handleListJobs(app))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeJobListResponse(t, rec.Body.Bytes())
	if resp.TotalItems != 3 {
		t.Fatalf("expected total_items 3, got %d", resp.TotalItems)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("expected 2 items on page 1, got %d", len(resp.Items))
	}

	rec = callHandler(t, app, http.MethodGet, "/api/custom/jobs?page=2&per_page=2", nil, handleListJobs(app))
	resp = decodeJobListResponse(t, rec.Body.Bytes())
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 item on page 2, got %d", len(resp.Items))
	}
}

func TestListJobsFiltersByStatus(t *testing.T) {
	app := newSetupTestApp(t)
	repo := createTestRepository(t, app, "repo-a")
	createTestScheduledJob(t, app, repo.Id, "job-active", "active")
	createTestScheduledJob(t, app, repo.Id, "job-error", "error")

	rec := callHandler(t, app, http.MethodGet, "/api/custom/jobs?status=error", nil, handleListJobs(app))
	resp := decodeJobListResponse(t, rec.Body.Bytes())
	if resp.TotalItems != 1 {
		t.Fatalf("expected 1 matching job, got %d", resp.TotalItems)
	}
	if len(resp.Items) != 1 || resp.Items[0].Name != "job-error" {
		t.Fatalf("expected only job-error, got %+v", resp.Items)
	}
}

func TestListJobsFiltersBySearch(t *testing.T) {
	app := newSetupTestApp(t)
	repo := createTestRepository(t, app, "repo-a")
	createTestScheduledJob(t, app, repo.Id, "payments-sync", "active")
	createTestScheduledJob(t, app, repo.Id, "backup-cleanup", "active")

	rec := callHandler(t, app, http.MethodGet, "/api/custom/jobs?search=payments", nil, handleListJobs(app))
	resp := decodeJobListResponse(t, rec.Body.Bytes())
	if resp.TotalItems != 1 || resp.Items[0].Name != "payments-sync" {
		t.Fatalf("expected only payments-sync, got %+v", resp.Items)
	}
}

func TestListJobsFiltersByRepository(t *testing.T) {
	app := newSetupTestApp(t)
	repoA := createTestRepository(t, app, "repo-a")
	repoB := createTestRepository(t, app, "repo-b")
	createTestScheduledJob(t, app, repoA.Id, "job-a", "active")
	createTestScheduledJob(t, app, repoB.Id, "job-b", "active")

	rec := callHandler(t, app, http.MethodGet, "/api/custom/jobs?repository="+repoB.Id, nil, handleListJobs(app))
	resp := decodeJobListResponse(t, rec.Body.Bytes())
	if resp.TotalItems != 1 || resp.Items[0].Name != "job-b" {
		t.Fatalf("expected only job-b, got %+v", resp.Items)
	}
}

func TestListJobGroupsWhenUnparseable(t *testing.T) {
	app := newSetupTestApp(t)
	repo := createTestRepository(t, app, "repo-a")
	// job_file points nowhere on disk, so ParseJobFile fails for every job -
	// this still exercises the endpoint's response shape without needing a
	// real repo checkout on disk.
	createTestScheduledJob(t, app, repo.Id, "job-1", "active")

	rec := callHandler(t, app, http.MethodGet, "/api/custom/jobs/groups", nil, handleListJobGroups(app))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Groups       []string `json:"groups"`
		HasUngrouped bool     `json:"has_ungrouped"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.HasUngrouped {
		t.Fatal("expected has_ungrouped to be true for unparseable job files")
	}
	if len(resp.Groups) != 0 {
		t.Fatalf("expected no groups, got %v", resp.Groups)
	}
}
