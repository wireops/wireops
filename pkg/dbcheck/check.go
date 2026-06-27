package dbcheck

import (
	"fmt"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"github.com/wireops/wireops/internal/safepath"
)

type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

type Issue struct {
	Severity   Severity `json:"severity"`
	Code       string   `json:"code"`
	Collection string   `json:"collection"`
	Field      string   `json:"field,omitempty"`
	Count      int      `json:"count"`
	Message    string   `json:"message"`
}

type Result struct {
	OK          bool           `json:"ok"`
	CheckedAt   string         `json:"checked_at"`
	Collections map[string]int `json:"collections"`
	IssueCount  int            `json:"issue_count"`
	Issues      []Issue        `json:"issues"`
}

type collectionSnapshot struct {
	records []*core.Record
	ids     map[string]bool
}

type relationCheck struct {
	collection string
	field      string
	target     string
	required   bool
}

var expectedCollections = []string{
	"repositories",
	"repository_keys",
	"workers",
	"worker_tokens",
	"worker_commands",
	"stacks",
	"stack_env_vars",
	"stack_services",
	"stack_revisions",
	"stack_pending_reconciles",
	"sync_logs",
	"scheduled_jobs",
	"job_env_vars",
	"job_runs",
	"worker_policies",
	"integrations",
	"invites",
}

var relationChecks = []relationCheck{
	{collection: "repositories", field: "repository_key", target: "repository_keys"},
	{collection: "stacks", field: "repository", target: "repositories"},
	{collection: "stacks", field: "worker", target: "workers"},
	{collection: "stack_env_vars", field: "stack", target: "stacks", required: true},
	{collection: "stack_services", field: "stack", target: "stacks", required: true},
	{collection: "stack_revisions", field: "stack", target: "stacks", required: true},
	{collection: "stack_pending_reconciles", field: "stack", target: "stacks", required: true},
	{collection: "sync_logs", field: "stack", target: "stacks", required: true},
	{collection: "scheduled_jobs", field: "repository", target: "repositories", required: true},
	{collection: "job_env_vars", field: "job", target: "scheduled_jobs", required: true},
	{collection: "job_runs", field: "job", target: "scheduled_jobs", required: true},
	{collection: "job_runs", field: "worker", target: "workers"},
	{collection: "worker_tokens", field: "worker", target: "workers"},
	{collection: "worker_commands", field: "worker", target: "workers", required: true},
}

func Validate(app core.App) Result {
	result := Result{
		OK:          true,
		CheckedAt:   time.Now().UTC().Format(time.RFC3339),
		Collections: map[string]int{},
		Issues:      []Issue{},
	}

	snapshots := map[string]collectionSnapshot{}
	for _, name := range expectedCollections {
		records, err := app.FindAllRecords(name)
		if err != nil {
			result.addIssue(SeverityError, "collection_missing", name, "", 1, "collection is missing or cannot be queried")
			continue
		}
		ids := make(map[string]bool, len(records))
		for _, rec := range records {
			ids[rec.Id] = true
		}
		snapshots[name] = collectionSnapshot{records: records, ids: ids}
		result.Collections[name] = len(records)
	}

	result.checkRelations(snapshots)
	result.checkRequiredFields(snapshots)
	result.checkSafePaths(snapshots)
	result.checkSingletons(snapshots)
	result.checkDuplicates(snapshots)

	result.IssueCount = len(result.Issues)
	for _, issue := range result.Issues {
		if issue.Severity == SeverityError {
			result.OK = false
			break
		}
	}

	return result
}

func (r *Result) checkRelations(snapshots map[string]collectionSnapshot) {
	for _, check := range relationChecks {
		source, ok := snapshots[check.collection]
		if !ok {
			continue
		}
		target, ok := snapshots[check.target]
		if !ok {
			continue
		}

		missing := 0
		broken := 0
		for _, rec := range source.records {
			value := rec.GetString(check.field)
			if value == "" {
				if check.required {
					missing++
				}
				continue
			}
			if !target.ids[value] {
				broken++
			}
		}

		if missing > 0 {
			r.addIssue(SeverityError, "required_relation_missing", check.collection, check.field, missing, fmt.Sprintf("%s.%s is empty", check.collection, check.field))
		}
		if broken > 0 {
			r.addIssue(SeverityError, "relation_target_missing", check.collection, check.field, broken, fmt.Sprintf("%s.%s points to missing %s records", check.collection, check.field, check.target))
		}
	}
}

func (r *Result) checkRequiredFields(snapshots map[string]collectionSnapshot) {
	required := map[string][]string{
		"repositories":   {"name", "git_url"},
		"workers":        {"hostname", "fingerprint"},
		"stacks":         {"name"},
		"stack_env_vars": {"key"},
		"scheduled_jobs": {"name", "job_file"},
		"job_env_vars":   {"key"},
	}

	for collection, fields := range required {
		snapshot, ok := snapshots[collection]
		if !ok {
			continue
		}
		for _, field := range fields {
			missing := 0
			for _, rec := range snapshot.records {
				if rec.GetString(field) == "" {
					missing++
				}
			}
			if missing > 0 {
				r.addIssue(SeverityError, "required_field_missing", collection, field, missing, fmt.Sprintf("%s.%s is empty", collection, field))
			}
		}
	}

	if stacks, ok := snapshots["stacks"]; ok {
		missingGitRepo := 0
		for _, rec := range stacks.records {
			if rec.GetString("source_type") == "git" && rec.GetString("repository") == "" {
				missingGitRepo++
			}
		}
		if missingGitRepo > 0 {
			r.addIssue(SeverityError, "git_stack_repository_missing", "stacks", "repository", missingGitRepo, "git stacks must reference a repository")
		}
	}
}

func (r *Result) checkSafePaths(snapshots map[string]collectionSnapshot) {
	if stacks, ok := snapshots["stacks"]; ok {
		invalidComposePath := 0
		invalidComposeFile := 0
		for _, rec := range stacks.records {
			if err := safepath.ValidateComposePath(rec.GetString("compose_path")); err != nil {
				invalidComposePath++
			}
			if err := safepath.ValidateComposeFile(rec.GetString("compose_file")); err != nil {
				invalidComposeFile++
			}
		}
		if invalidComposePath > 0 {
			r.addIssue(SeverityError, "unsafe_path", "stacks", "compose_path", invalidComposePath, "stack compose_path contains an unsafe path")
		}
		if invalidComposeFile > 0 {
			r.addIssue(SeverityError, "unsafe_path", "stacks", "compose_file", invalidComposeFile, "stack compose_file contains an unsafe path")
		}
	}

	if jobs, ok := snapshots["scheduled_jobs"]; ok {
		invalid := 0
		for _, rec := range jobs.records {
			jobFile := strings.TrimSpace(rec.GetString("job_file"))
			if jobFile == "" {
				continue
			}
			if _, err := safepath.CleanRelativePath(jobFile); err != nil {
				invalid++
			}
		}
		if invalid > 0 {
			r.addIssue(SeverityError, "unsafe_path", "scheduled_jobs", "job_file", invalid, "scheduled job file contains an unsafe path")
		}
	}

	if revisions, ok := snapshots["stack_revisions"]; ok {
		invalid := 0
		for _, rec := range revisions.records {
			if err := safepath.ValidateComposePath(rec.GetString("compose_path")); err != nil {
				invalid++
			}
		}
		if invalid > 0 {
			r.addIssue(SeverityError, "unsafe_path", "stack_revisions", "compose_path", invalid, "stack revision compose_path contains an unsafe path")
		}
	}
}

func (r *Result) checkSingletons(snapshots map[string]collectionSnapshot) {
	for _, collection := range []string{"worker_policies"} {
		snapshot, ok := snapshots[collection]
		if !ok {
			continue
		}
		if len(snapshot.records) > 1 {
			r.addIssue(SeverityWarning, "singleton_has_multiple_records", collection, "", len(snapshot.records), fmt.Sprintf("%s should contain at most one record", collection))
		}
	}
}

func (r *Result) checkDuplicates(snapshots map[string]collectionSnapshot) {
	checkDuplicateField := func(collection, field string, severity Severity) {
		snapshot, ok := snapshots[collection]
		if !ok {
			return
		}
		counts := map[string]int{}
		for _, rec := range snapshot.records {
			value := rec.GetString(field)
			if value != "" {
				counts[value]++
			}
		}
		duplicateValues := 0
		for _, count := range counts {
			if count > 1 {
				duplicateValues++
			}
		}
		if duplicateValues > 0 {
			r.addIssue(severity, "duplicate_field_value", collection, field, duplicateValues, fmt.Sprintf("%s.%s has duplicate values", collection, field))
		}
	}

	checkDuplicateField("workers", "fingerprint", SeverityError)
	checkDuplicateField("integrations", "slug", SeverityError)
	checkDuplicateField("worker_tokens", "token_hash", SeverityError)
	checkDuplicateField("worker_commands", "command_id", SeverityError)
}

func (r *Result) addIssue(severity Severity, code, collection, field string, count int, message string) {
	r.Issues = append(r.Issues, Issue{
		Severity:   severity,
		Code:       code,
		Collection: collection,
		Field:      field,
		Count:      count,
		Message:    message,
	})
}
