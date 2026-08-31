package routes

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
	"github.com/pocketbase/pocketbase/tools/search"

	"github.com/wireops/wireops/internal/config"
	"github.com/wireops/wireops/internal/job"
	"github.com/wireops/wireops/internal/jobscheduler"
	"github.com/wireops/wireops/internal/rbac"
	"github.com/wireops/wireops/internal/safepath"
)

// jobListItem is the enriched job record returned by the list endpoint.
// Each item embeds the parsed job.yaml definition so the UI never has to
// issue per-job requests.
type jobRunSummary struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Created string `json:"created"`
}

type jobListItem struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	Description     string          `json:"description"`
	JobFile         string          `json:"job_file"`
	Enabled         bool            `json:"enabled"`
	Status          string          `json:"status"`
	LastRunAt       string          `json:"last_run_at"`
	Created         string          `json:"created"`
	Updated         string          `json:"updated"`
	Repository      jobRepoInfo     `json:"repository"`
	Definition      *job.Definition `json:"definition"`
	DefinitionError string          `json:"definition_error,omitempty"`
	Errors          []string        `json:"errors,omitempty"`
	StalledReason   string          `json:"stalled_reason,omitempty"`
	RecentRuns      []jobRunSummary `json:"recent_runs"`
}

type jobRepoInfo struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	GitURL string `json:"git_url"`
}

func validationErrors(err error) []string {
	var valErr *job.ValidationError
	if errors.As(err, &valErr) {
		return valErr.Errors
	}
	return []string{err.Error()}
}

// resolveJobParams extracts the job ID from the request, finds the scheduled job,
// and resolves repository workspace, repository ID, and job file path.
func resolveJobParams(app core.App, e *core.RequestEvent) (*core.Record, string, string, string, error) {
	id := e.Request.PathValue("id")
	if id == "" {
		return nil, "", "", "", e.JSON(http.StatusBadRequest, map[string]string{"error": "missing id"})
	}

	rec, err := app.FindRecordById("scheduled_jobs", id)
	if err != nil {
		return nil, "", "", "", e.JSON(http.StatusNotFound, map[string]string{"error": "job not found"})
	}

	repoWorkspace := config.GetReposWorkspace()
	repoID := rec.GetString("repository")
	jobFile := rec.GetString("job_file")
	return rec, repoWorkspace, repoID, jobFile, nil
}

type jobListResponse struct {
	Items      []jobListItem `json:"items"`
	TotalItems int           `json:"total_items"`
}

// buildJobsFilter turns the list endpoint's page/status/repository/search
// query params into a PocketBase filter string + bind params. status and
// repository are real scheduled_jobs columns so they can be pushed to the
// DB; group can't be (it only exists inside the parsed job.yaml, not a
// stored column) so it isn't handled here - see the /api/custom/jobs/groups
// endpoint below for that facet.
func buildJobsFilter(e *core.RequestEvent) (string, dbx.Params) {
	q := e.Request.URL.Query()
	clauses := []string{}
	params := dbx.Params{}

	if status := q.Get("status"); status != "" && status != "all" {
		clauses = append(clauses, "status = {:status}")
		params["status"] = status
	}
	if repository := q.Get("repository"); repository != "" && repository != "all" {
		clauses = append(clauses, "repository = {:repository}")
		params["repository"] = repository
	}
	if search := strings.TrimSpace(q.Get("search")); search != "" {
		clauses = append(clauses, "name ~ {:search}")
		params["search"] = search
	}

	return strings.Join(clauses, " && "), params
}

// countRecordsByPBFilter counts records matching a PocketBase filter
// expression (the same "field ~ {:x}" syntax FindRecordsByFilter accepts)
// without fetching the matching rows themselves - app.CountRecords only
// takes raw dbx expressions, which don't understand PocketBase filter
// operators like "~", so the filter has to be resolved the same way
// FindRecordsByFilter resolves it internally.
func countRecordsByPBFilter(app core.App, collectionNameOrId string, filter string, params dbx.Params) (int64, error) {
	collection, err := app.FindCollectionByNameOrId(collectionNameOrId)
	if err != nil {
		return 0, err
	}

	q := app.RecordQuery(collection).Select("count(*)")

	if filter != "" {
		resolver := core.NewRecordFieldResolver(app, collection, nil, true)
		expr, err := search.FilterData(filter).BuildExpr(resolver, params)
		if err != nil {
			return 0, err
		}
		q.AndWhere(expr)
		if err := resolver.UpdateQuery(q); err != nil {
			return 0, err
		}
	}

	var total int64
	if err := q.Row(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func parsePageParam(v string, def int) int {
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return def
	}
	return n
}

// handleListJobs lists scheduled jobs with their definitions resolved
// server-side, paged and filtered at the DB level before the (relatively
// expensive) per-job job.yaml parse + repo/run lookups run - those only
// happen for the page actually being returned, not the whole collection
// like before.
func handleListJobs(app core.App) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		page := parsePageParam(e.Request.URL.Query().Get("page"), 1)
		perPage := parsePageParam(e.Request.URL.Query().Get("per_page"), 24)
		if perPage > 200 {
			perPage = 200
		}

		filter, params := buildJobsFilter(e)

		total, err := countRecordsByPBFilter(app, "scheduled_jobs", filter, params)
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		records, err := app.FindRecordsByFilter("scheduled_jobs", filter, "name", perPage, (page-1)*perPage, params)
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		repoWorkspace := config.GetReposWorkspace()
		items := make([]jobListItem, 0, len(records))

		for _, rec := range records {
			repoID := rec.GetString("repository")

			var repoInfo jobRepoInfo
			if repoRec, rerr := app.FindRecordById("repositories", repoID); rerr == nil {
				repoInfo = jobRepoInfo{
					ID:     repoRec.Id,
					Name:   repoRec.GetString("name"),
					GitURL: repoRec.GetString("git_url"),
				}
			}

			item := jobListItem{
				ID:          rec.Id,
				Name:        rec.GetString("name"),
				Description: rec.GetString("description"),
				JobFile:     rec.GetString("job_file"),
				Enabled:     rec.GetBool("enabled"),
				Status:      rec.GetString("status"),
				LastRunAt:   rec.GetDateTime("last_run_at").String(),
				Created:     rec.GetDateTime("created").String(),
				Updated:     rec.GetDateTime("updated").String(),
				Repository:  repoInfo,
			}

			if item.Status == "stalled" {
				runs, err := app.FindRecordsByFilter("job_runs", "job = {:jobId} && status = 'stalled'", "-created", 1, 0, dbx.Params{"jobId": rec.Id})
				if err == nil && len(runs) > 0 {
					item.StalledReason = runs[0].GetString("output")
				}
			}

			// Fetch recent runs
			item.RecentRuns = []jobRunSummary{}
			recentRuns, rerr := app.FindRecordsByFilter("job_runs", "job = {:jobId}", "-created", 5, 0, dbx.Params{"jobId": rec.Id})
			if rerr == nil {
				for _, run := range recentRuns {
					item.RecentRuns = append(item.RecentRuns, jobRunSummary{
						ID:      run.Id,
						Status:  run.GetString("status"),
						Created: run.GetDateTime("created").String(),
					})
				}
			}

			def, derr := job.ParseJobFile(repoWorkspace, repoID, item.JobFile)
			if derr != nil {
				item.DefinitionError = derr.Error()
				item.Errors = validationErrors(derr)
			} else {
				item.Definition = def
			}

			items = append(items, item)
		}

		return e.JSON(http.StatusOK, jobListResponse{Items: items, TotalItems: int(total)})
	}
}

// handleListJobGroups is a lightweight companion to handleListJobs,
// returning only the group parsed out of each job's job.yaml. group isn't a
// stored column (see buildJobsFilter's comment) so it can't be
// filtered/paged at the DB level; the UI uses this to populate the group
// filter dropdown without paying for the repo/run enrichment the main list
// does.
func handleListJobGroups(app core.App) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		records, err := app.FindAllRecords("scheduled_jobs")
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		repoWorkspace := config.GetReposWorkspace()
		seen := map[string]bool{}
		groups := []string{}
		hasUngrouped := false

		for _, rec := range records {
			def, derr := job.ParseJobFile(repoWorkspace, rec.GetString("repository"), rec.GetString("job_file"))
			group := ""
			if derr == nil && def != nil {
				group = def.Group
			}
			if group == "" {
				hasUngrouped = true
				continue
			}
			if !seen[group] {
				seen[group] = true
				groups = append(groups, group)
			}
		}

		return e.JSON(http.StatusOK, map[string]any{
			"groups":        groups,
			"has_ungrouped": hasUngrouped,
		})
	}
}

// RegisterJobRoutes mounts custom REST endpoints for scheduled jobs.
func RegisterJobRoutes(r *router.Router[*core.RequestEvent], app core.App, sched *jobscheduler.Scheduler) {
	r.GET("/api/custom/jobs", handleListJobs(app)).BindFunc(rbac.Require(rbac.CapViewJobs))
	r.GET("/api/custom/jobs/groups", handleListJobGroups(app)).BindFunc(rbac.Require(rbac.CapViewJobs))

	// Cancel a running job run (kills the container).
	r.POST("/api/custom/job-runs/{runId}/cancel", func(e *core.RequestEvent) error {
		runID := e.Request.PathValue("runId")
		if runID == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "missing run id"})
		}
		if err := sched.CancelRun(runID); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		return e.JSON(http.StatusOK, map[string]string{"status": "cancelled"})
	}).BindFunc(rbac.Require(rbac.CapManageJobs))

	// Trigger a manual run immediately.
	r.POST("/api/custom/jobs/{id}/run", func(e *core.RequestEvent) error {
		rec, repoWorkspace, repoID, jobFile, err := resolveJobParams(app, e)
		if err != nil {
			return err
		}
		if _, err := job.ParseJobFile(repoWorkspace, repoID, jobFile); err != nil {
			var valErr *job.ValidationError
			if errors.As(err, &valErr) {
				return e.JSON(http.StatusUnprocessableEntity, map[string]any{
					"error":  err.Error(),
					"errors": validationErrors(err),
				})
			}
			return e.JSON(http.StatusUnprocessableEntity, map[string]string{"error": "Validation failed: " + err.Error()})
		}
		var userID string
		if e.Auth != nil {
			userID = e.Auth.Id
		}
		sched.TriggerManual(rec.Id, userID)
		return e.JSON(http.StatusOK, map[string]string{"status": "triggered"})
	}).BindFunc(rbac.Require(rbac.CapManageJobs))

	// Return the parsed job.yaml definition for a single scheduled job.
	r.GET("/api/custom/jobs/{id}/definition", func(e *core.RequestEvent) error {
		_, repoWorkspace, repoID, jobFile, err := resolveJobParams(app, e)
		if err != nil {
			return err
		}

		def, err := job.ParseJobFile(repoWorkspace, repoID, jobFile)
		if err != nil {
			return e.JSON(http.StatusUnprocessableEntity, map[string]any{
				"error":  err.Error(),
				"errors": validationErrors(err),
			})
		}

		return e.JSON(http.StatusOK, def)
	}).BindFunc(rbac.Require(rbac.CapViewJobs))

	// Return the raw, unparsed job.yaml definition file content for a single scheduled job.
	r.GET("/api/custom/jobs/{id}/raw", func(e *core.RequestEvent) error {
		_, repoWorkspace, repoID, jobFile, err := resolveJobParams(app, e)
		if err != nil {
			return err
		}

		clean, err := safepath.CleanRelativePath(jobFile)
		if err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid job_file path"})
		}

		full := filepath.Join(repoWorkspace, repoID, clean)
		data, err := os.ReadFile(full)
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "job file not found"})
		}

		return e.JSON(http.StatusOK, map[string]string{
			"content":  string(data),
			"filename": filepath.Base(jobFile),
		})
	}).BindFunc(rbac.Require(rbac.CapViewJobs))

	// Delete a job run (only if stalled).
	r.DELETE("/api/custom/job-runs/{runId}", func(e *core.RequestEvent) error {
		runID := e.Request.PathValue("runId")
		if runID == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "missing run id"})
		}

		rec, err := app.FindRecordById("job_runs", runID)
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "job run not found"})
		}

		if rec.GetString("status") != "stalled" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "only stalled job runs can be deleted"})
		}

		if err := app.Delete(rec); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		// If we delete the latest run and there are no other stalled runs, we might want to update the job status,
		// but typically jobs transition to "stalled" via the scheduler. For now, just delete the run.
		return e.JSON(http.StatusOK, map[string]string{"status": "deleted"})
	}).BindFunc(rbac.Require(rbac.CapManageJobs))

	// Bulk-upsert a job's env vars (atomic create/update/delete), mirroring
	// POST /api/custom/stacks/{id}/env-vars/bulk in internal/routes/env_var_routes.go.
	r.POST("/api/custom/jobs/{id}/env-vars/bulk", func(e *core.RequestEvent) error {
		return bulkUpsertJobEnvVars(app, e)
	}).Bind(apis.BodyLimit(envVarsBulkMaxBytes)).BindFunc(rbac.Require(rbac.CapManageJobs))
}
