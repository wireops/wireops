package audit

import (
	"encoding/json"
	"log"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	ActorAnonymous = "anonymous"
	ActorAgent     = "agent"
	ActorSystem    = "system"
	ActorUser      = "user"
	ActorWorker    = "worker"

	OriginAPI     = "api"
	OriginAPIKey  = "api_key"
	OriginSetup   = "setup"
	OriginSystem  = "system"
	OriginUI      = "ui"
	OriginWebhook = "webhook"
	OriginWorker  = "worker"

	StatusSuccess = "success"
	StatusError   = "error"

	DefaultAuditRetentionDays  = 30
	DefaultJobRunRetentionDays = 7

	headerAuditActorID   = "X-Wireops-Actor-Id"
	headerAuditOrigin    = "X-Wireops-Origin"
	headerAuditWorkerID  = "X-Wireops-Worker-Id"
	headerAuditRequestID = "X-Request-Id"
)

type Event struct {
	ActorType    string
	ActorID      string
	Action       string
	ResourceType string
	ResourceID   string
	Origin       string
	Status       string
	ErrorCode    string
	Metadata     map[string]any
}

func RecordRequest(app core.App, req *core.RequestEvent, ev Event) {
	if req != nil && req.Auth != nil {
		ev.ActorType = ActorUser
		if req.Auth.Collection() != nil && req.Auth.Collection().Name == "service_accounts" {
			ev.ActorType = ActorAgent
		}
		ev.ActorID = req.Auth.Id
	}

	if ev.Origin == "" {
		ev.Origin = RequestOrigin(req)
	}

	if ev.ActorType == "" {
		isVerified := isInternalOrVerified(req)
		switch ev.Origin {
		case OriginWorker:
			if isVerified {
				ev.ActorType = ActorWorker
				if req != nil && req.Request != nil {
					ev.ActorID = strings.TrimSpace(req.Request.Header.Get(headerAuditWorkerID))
				}
			}
		case OriginSystem:
			if isVerified {
				ev.ActorType = ActorSystem
			}
		}

		if ev.ActorType == "" {
			ev.ActorType = ActorAnonymous
			if req != nil && req.Request != nil {
				ev.ActorID = strings.TrimSpace(req.Request.Header.Get(headerAuditActorID))
			}
		}
	}

	Record(app, ev)
}

func isInternalOrVerified(req *core.RequestEvent) bool {
	if req == nil || req.Request == nil {
		return true
	}
	if req.HasSuperuserAuth() || req.Auth != nil {
		return true
	}
	return false
}

func RecordSystem(app core.App, action, resourceType, resourceID, status, errorCode string) {
	Record(app, Event{
		ActorType:    ActorSystem,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Origin:       OriginSystem,
		Status:       status,
		ErrorCode:    errorCode,
	})
}

func Record(app core.App, ev Event) {
	if ev.Action == "" || ev.ResourceType == "" {
		return
	}
	if ev.ActorType == "" {
		ev.ActorType = ActorSystem
	}
	if ev.Origin == "" {
		ev.Origin = OriginSystem
	}
	if ev.Status == "" {
		ev.Status = StatusSuccess
	}

	col, err := app.FindCollectionByNameOrId("audit_logs")
	if err != nil {
		return
	}

	rec := core.NewRecord(col)
	rec.Set("actor_type", ev.ActorType)
	rec.Set("actor_id", ev.ActorID)
	rec.Set("action", ev.Action)
	rec.Set("resource_type", ev.ResourceType)
	rec.Set("resource_id", ev.ResourceID)
	rec.Set("origin", ev.Origin)
	rec.Set("status", ev.Status)
	rec.Set("error_code", ev.ErrorCode)
	rec.Set("expires_at", time.Now().AddDate(0, 0, AuditRetentionDays(app)))
	if len(ev.Metadata) > 0 {
		rec.Set("metadata_json", ev.Metadata)
	}

	if err := app.Save(rec); err != nil {
		log.Printf("[audit] failed to record %s for %s/%s: %v", ev.Action, ev.ResourceType, ev.ResourceID, err)
	}
}

func AuditRetentionDays(app core.App) int {
	return retentionDays(app, "audit_retention_days", DefaultAuditRetentionDays)
}

func JobRunRetentionDays(app core.App) int {
	return retentionDays(app, "job_run_retention_days", DefaultJobRunRetentionDays)
}

func retentionDays(app core.App, field string, fallback int) int {
	records, err := app.FindAllRecords("app_settings")
	if err != nil || len(records) == 0 {
		return fallback
	}
	days := records[0].GetInt(field)
	if days <= 0 {
		return fallback
	}
	return days
}

func PurgeExpired(app core.App) error {
	now := time.Now()
	if _, err := app.DB().NewQuery("DELETE FROM audit_logs WHERE expires_at < {:now}").
		Bind(dbx.Params{"now": now}).Execute(); err != nil {
		return err
	}
	if _, err := app.DB().NewQuery("DELETE FROM job_runs WHERE expires_at < {:now}").
		Bind(dbx.Params{"now": now}).Execute(); err != nil {
		return err
	}
	return nil
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

func CustomRouteMiddleware(app core.App) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		ev, ok := MatchCustomRoute(e.Request.Method, e.Request.URL.Path)
		if !ok {
			return e.Next()
		}

		ev.Origin = RequestOrigin(e)
		ev.Metadata = RequestMetadata(e)

		recorder := &statusRecorder{ResponseWriter: e.Response}
		e.Response = recorder
		err := e.Next()

		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		if err != nil || status >= 400 {
			ev.Status = StatusError
			if status >= 400 {
				ev.ErrorCode = strconv.Itoa(status)
			} else {
				ev.ErrorCode = "500"
			}
		} else {
			ev.Status = StatusSuccess
		}

		RecordRequest(app, e, ev)
		return err
	}
}

func MatchCustomRoute(method, path string) (Event, bool) {
	if method != http.MethodPost && method != http.MethodPut && method != http.MethodPatch && method != http.MethodDelete {
		return Event{}, false
	}

	parts := splitPath(path)
	if len(parts) < 3 || parts[0] != "api" || parts[1] != "custom" {
		return Event{}, false
	}
	p := parts[2:]

	if len(p) == 3 && p[0] == "stacks" && method == http.MethodPost {
		switch p[2] {
		case "sync":
			return custom("stack.sync", "stack", p[1]), true
		case "rollback":
			return custom("stack.rollback", "stack", p[1]), true
		case "force-redeploy":
			return custom("stack.force_redeploy", "stack", p[1]), true
		case "transfer":
			return custom("stack.transfer", "stack", p[1]), true
		}
	}
	if len(p) == 2 && p[0] == "stacks" && method == http.MethodDelete {
		return custom("stack.delete", "stack", p[1]), true
	}
	if len(p) == 4 && p[0] == "stacks" && p[2] == "container" && method == http.MethodPost {
		switch p[3] {
		case "stop":
			return custom("container.stop", "stack", p[1]), true
		case "restart":
			return custom("container.restart", "stack", p[1]), true
		}
	}
	if len(p) == 2 && p[0] == "webhook" && method == http.MethodPost {
		return custom("stack.webhook_sync", "stack", p[1]), true
	}
	if len(p) == 2 && p[0] == "stacks" && p[1] == "import" && method == http.MethodPost {
		return custom("stack.import", "stack", ""), true
	}
	if len(p) == 2 && p[0] == "orphans" && method == http.MethodDelete {
		return custom("orphan.purge", "orphan", p[1]), true
	}
	if len(p) == 2 && p[0] == "credentials" && method == http.MethodPost {
		switch p[1] {
		case "test":
			return custom("credential.test", "credential", ""), true
		case "keyscan":
			return custom("credential.keyscan", "credential", ""), true
		}
	}
	if len(p) == 1 && p[0] == "backups" && method == http.MethodPost {
		return custom("backup.create", "backup", "local"), true
	}

	if len(p) == 3 && p[0] == "jobs" && p[2] == "run" && method == http.MethodPost {
		return custom("job.run", "scheduled_job", p[1]), true
	}
	if len(p) == 3 && p[0] == "job-runs" {
		if p[2] == "cancel" && method == http.MethodPost {
			return custom("job_run.cancel", "job_run", p[1]), true
		}
	}
	if len(p) == 2 && p[0] == "job-runs" && method == http.MethodDelete {
		return custom("job_run.delete", "job_run", p[1]), true
	}

	if len(p) == 2 && p[0] == "worker" && p[1] == "tokens" && method == http.MethodPost {
		return custom("worker_token.create", "worker_token", ""), true
	}
	if len(p) == 3 && p[0] == "workers" && p[2] == "revoke" && method == http.MethodPost {
		return custom("worker.revoke", "worker", p[1]), true
	}
	if len(p) == 3 && p[0] == "workers" && p[2] == "policy" {
		if method == http.MethodPut {
			return custom("worker_policy.update", "worker_policy", p[1]), true
		}
		if method == http.MethodDelete {
			return custom("worker_policy.reset", "worker_policy", p[1]), true
		}
	}

	if len(p) == 2 && p[0] == "settings" && p[1] == "worker-policy" && method == http.MethodPut {
		return custom("settings.worker_policy.update", "worker_policy", "global"), true
	}
	if len(p) == 2 && p[0] == "settings" && p[1] == "app-settings" && method == http.MethodPut {
		return custom("settings.app.update", "app_settings", "global"), true
	}
	if len(p) == 2 && p[0] == "settings" && p[1] == "sso-groups-claim" && method == http.MethodPut {
		return custom("settings.sso_claim.update", "app_settings", "global"), true
	}
	if len(p) == 2 && p[0] == "users" && p[1] == "invite" && method == http.MethodPost {
		return custom("user.invite", "user", ""), true
	}
	if len(p) == 2 && p[0] == "users" && method == http.MethodPut {
		return custom("user.update", "user", p[1]), true
	}
	if len(p) == 3 && p[0] == "users" && p[1] == "invite" && p[2] == "accept" && method == http.MethodPost {
		return custom("user.invite_accept", "user", ""), true
	}
	if len(p) == 2 && p[0] == "auth" && p[1] == "elevate" && method == http.MethodPost {
		return custom("auth.elevate", "auth", ""), true
	}
	if len(p) == 1 && p[0] == "setup" && method == http.MethodPost {
		return custom("setup.create_admin", "setup", "initial"), true
	}

	if len(p) == 2 && p[0] == "integrations" {
		if method == http.MethodPut {
			return custom("integration.update", "integration", p[1]), true
		}
		if method == http.MethodDelete {
			return custom("integration.delete", "integration", p[1]), true
		}
	}
	if len(p) == 3 && p[0] == "integrations" && p[2] == "test" && method == http.MethodPost {
		return custom("integration.test", "integration", p[1]), true
	}
	if len(p) == 1 && p[0] == "service-accounts" && method == http.MethodPost {
		return custom("service_account.create", "service_account", ""), true
	}
	if len(p) == 2 && p[0] == "service-accounts" {
		switch method {
		case http.MethodPut:
			return custom("service_account.update", "service_account", p[1]), true
		case http.MethodDelete:
			return custom("service_account.delete", "service_account", p[1]), true
		}
	}
	if len(p) == 1 && p[0] == "sso-group-roles" && method == http.MethodPost {
		return custom("sso_group_role.create", "sso_group_role", ""), true
	}
	if len(p) == 2 && p[0] == "sso-group-roles" {
		switch method {
		case http.MethodPut:
			return custom("sso_group_role.update", "sso_group_role", p[1]), true
		case http.MethodDelete:
			return custom("sso_group_role.delete", "sso_group_role", p[1]), true
		}
	}

	return Event{}, false
}

func RequestOrigin(req *core.RequestEvent) string {
	if req == nil || req.Request == nil {
		return OriginSystem
	}

	if origin := normalizeOrigin(req.Request.Header.Get(headerAuditOrigin)); origin != "" {
		return origin
	}

	path := req.Request.URL.Path
	switch {
	case strings.HasPrefix(path, "/api/custom/webhook/"):
		return OriginWebhook
	case strings.HasPrefix(path, "/api/custom/setup"):
		return OriginSetup
	default:
		return OriginAPI
	}
}

func RequestMetadata(req *core.RequestEvent) map[string]any {
	if req == nil {
		return nil
	}

	info, err := req.RequestInfo()
	if err != nil {
		return nil
	}

	metadata := map[string]any{}

	if requestID := strings.TrimSpace(req.Request.Header.Get(headerAuditRequestID)); requestID != "" {
		metadata["request_id"] = requestID
	}

	bodyKeys, sensitiveKeys := sanitizeBodyKeys(info.Body)
	if len(bodyKeys) > 0 {
		metadata["changed_fields"] = bodyKeys
	}
	if len(sensitiveKeys) > 0 {
		metadata["sensitive_fields"] = sensitiveKeys
	}

	if queryKeys := sortedKeys(info.Query); len(queryKeys) > 0 {
		metadata["query_keys"] = queryKeys
	}

	if len(metadata) == 0 {
		return nil
	}

	return metadata
}

func MetadataJSON(raw any) map[string]any {
	if raw == nil {
		return nil
	}

	switch v := raw.(type) {
	case map[string]any:
		return v
	case []byte:
		var out map[string]any
		if err := json.Unmarshal(v, &out); err == nil {
			return out
		}
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(v), &out); err == nil {
			return out
		}
	}

	return nil
}

func custom(action, resourceType, resourceID string) Event {
	return Event{
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
	}
}

func splitPath(path string) []string {
	raw := strings.Split(strings.Trim(path, "/"), "/")
	parts := make([]string, 0, len(raw))
	for _, part := range raw {
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

func normalizeOrigin(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case OriginAPI:
		return OriginAPI
	case OriginAPIKey:
		return OriginAPIKey
	case OriginSetup:
		return OriginSetup
	case OriginSystem:
		return OriginSystem
	case OriginUI:
		return OriginUI
	case OriginWebhook:
		return OriginWebhook
	case OriginWorker:
		return OriginWorker
	default:
		return ""
	}
}

func sanitizeBodyKeys(body map[string]any) ([]string, []string) {
	if len(body) == 0 {
		return nil, nil
	}

	fields := make([]string, 0, len(body))
	sensitive := make([]string, 0, len(body))

	for key := range body {
		fields = append(fields, key)
		if isSensitiveKey(key) {
			sensitive = append(sensitive, key)
		}
	}

	sort.Strings(fields)
	sort.Strings(sensitive)

	return fields, sensitive
}

func sortedKeys(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}

	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	return keys
}

func isSensitiveKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		return false
	}

	sensitiveWords := []string{
		"authorization",
		"cookie",
		"credential",
		"git_password",
		"known_host",
		"passphrase",
		"password",
		"private_key",
		"secret",
		"ssh_key",
		"ssh_private_key",
		"token",
	}

	return slices.ContainsFunc(sensitiveWords, func(word string) bool {
		return strings.Contains(key, word)
	})
}
