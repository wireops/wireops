package routes

import (
	"net/http"
	"path/filepath"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/jfxdev/wireops/internal/job"
	"github.com/jfxdev/wireops/internal/jobscheduler"
)

// jobListItem is the enriched job record returned by the list endpoint.
// Each item embeds the parsed job.yaml definition so the UI never has to
// issue per-job requests.
type jobListItem struct {
	ID              string          `json:"id"`
	JobFile         string          `json:"job_file"`
	Enabled         bool            `json:"enabled"`
	Status          string          `json:"status"`
	LastRunAt       string          `json:"last_run_at"`
	Created         string          `json:"created"`
	Updated         string          `json:"updated"`
	Repository      jobRepoInfo     `json:"repository"`
	Definition      *job.Definition `json:"definition"`
	DefinitionError string          `json:"definition_error,omitempty"`
}

type jobRepoInfo struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	GitURL string `json:"git_url"`
}

// RegisterJobRoutes mounts custom REST endpoints for scheduled jobs.
func RegisterJobRoutes(r *router.Router[*core.RequestEvent], app core.App, sched *jobscheduler.Scheduler) {
	// List all scheduled jobs with their definitions resolved server-side.
	r.GET("/api/custom/jobs", func(e *core.RequestEvent) error {
		records, err := app.FindAllRecords("scheduled_jobs")
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		repoWorkspace := filepath.Join(app.DataDir(), "repositories")
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
				ID:         rec.Id,
				JobFile:    rec.GetString("job_file"),
				Enabled:    rec.GetBool("enabled"),
				Status:     rec.GetString("status"),
				LastRunAt:  rec.GetDateTime("last_run_at").String(),
				Created:    rec.GetDateTime("created").String(),
				Updated:    rec.GetDateTime("updated").String(),
				Repository: repoInfo,
			}

			def, derr := job.ParseJobFile(repoWorkspace, repoID, item.JobFile)
			if derr != nil {
				item.DefinitionError = derr.Error()
			} else {
				item.Definition = def
			}

			items = append(items, item)
		}

		return e.JSON(http.StatusOK, items)
	})

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
	})

	// Trigger a manual run immediately.
	r.POST("/api/custom/jobs/{id}/run", func(e *core.RequestEvent) error {
		id := e.Request.PathValue("id")
		if id == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "missing id"})
		}
		if _, err := app.FindRecordById("scheduled_jobs", id); err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "job not found"})
		}
		sched.TriggerManual(id)
		return e.JSON(http.StatusOK, map[string]string{"status": "triggered"})
	})

	// Return the parsed job.yaml definition for a single scheduled job.
	r.GET("/api/custom/jobs/{id}/definition", func(e *core.RequestEvent) error {
		id := e.Request.PathValue("id")
		if id == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "missing id"})
		}

		rec, err := app.FindRecordById("scheduled_jobs", id)
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "job not found"})
		}

		repoWorkspace := filepath.Join(app.DataDir(), "repositories")
		repoID := rec.GetString("repository")
		jobFile := rec.GetString("job_file")

		def, err := job.ParseJobFile(repoWorkspace, repoID, jobFile)
		if err != nil {
			return e.JSON(http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		}

		return e.JSON(http.StatusOK, def)
	})
}
