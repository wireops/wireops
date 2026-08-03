package routes

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/wireops/wireops/internal/audit"
	"github.com/wireops/wireops/internal/protocol"
	"github.com/wireops/wireops/internal/rbac"
	"github.com/wireops/wireops/internal/termsession"
)

// terminalIdleTimeout closes a session that has received no input for this
// long — a forgotten, still-open shell is a standing liability (see
// docs/TERMINAL.md phase-3 hardening). Activity is tracked only on input,
// not output, so a long-running foreground command (e.g. `tail -f`) doesn't
// count as "idle" even though the operator isn't typing.
const terminalIdleTimeout = 15 * time.Minute

// terminalIdleSweepInterval is how often the sweeper checks for idle sessions.
const terminalIdleSweepInterval = time.Minute

// maxConcurrentSessionsPerWorker bounds how many terminal sessions can be
// open at once against a single worker, overridable via
// TERMINAL_MAX_SESSIONS_PER_WORKER — a worker host has finite pty/exec
// capacity and this is the cheapest guard against accidental (or malicious)
// exhaustion.
func maxConcurrentSessionsPerWorker() int {
	if v := strings.TrimSpace(os.Getenv("TERMINAL_MAX_SESSIONS_PER_WORKER")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 5
}

// terminalSender is the subset of the worker dispatcher a terminal session
// needs: fire-and-forget sends over the already-open worker WebSocket, no
// request/response correlation like Dispatch (a terminal session outlives
// any single command-result round trip). The concrete *worker.WorkerServer
// passed into routes.Register always satisfies this; it's asserted lazily
// per-request rather than widening sync.WorkerDispatcher, so unrelated test
// mocks for that interface don't need to grow a method they'll never use.
type terminalSender interface {
	SendMessage(workerID string, msgType protocol.MessageType, payload interface{}) error
}

// terminalSessionMeta is the routing/ownership record kept for an open
// session between the open call and its later input/resize/close calls.
// In-memory only, single wireops server instance — matches this feature's
// phase-1 scope (see docs/TERMINAL.md).
type terminalSessionMeta struct {
	workerID     string
	actorID      string
	stackID      string
	lastActivity time.Time
}

var (
	terminalSessionsMu sync.Mutex
	terminalSessions   = map[string]terminalSessionMeta{}
)

func rememberTerminalSession(sessionID string, meta terminalSessionMeta) {
	meta.lastActivity = time.Now()
	terminalSessionsMu.Lock()
	defer terminalSessionsMu.Unlock()
	terminalSessions[sessionID] = meta
}

// reserveTerminalSession atomically checks workerID's session count against
// limit and, if under it, inserts meta under sessionID in the same critical
// section — the count-check and the insert must not be two separate locked
// sections, or two concurrent opens against a worker at limit-1 can both
// pass the check before either inserts, exceeding the cap.
func reserveTerminalSession(sessionID, workerID string, limit int, meta terminalSessionMeta) bool {
	terminalSessionsMu.Lock()
	defer terminalSessionsMu.Unlock()
	n := 0
	for _, m := range terminalSessions {
		if m.workerID == workerID {
			n++
		}
	}
	if n >= limit {
		return false
	}
	meta.lastActivity = time.Now()
	terminalSessions[sessionID] = meta
	return true
}

func lookupTerminalSession(sessionID string) (terminalSessionMeta, bool) {
	terminalSessionsMu.Lock()
	defer terminalSessionsMu.Unlock()
	meta, ok := terminalSessions[sessionID]
	return meta, ok
}

func forgetTerminalSession(sessionID string) {
	terminalSessionsMu.Lock()
	defer terminalSessionsMu.Unlock()
	delete(terminalSessions, sessionID)
}

// touchTerminalSession bumps sessionID's last-activity timestamp so the idle
// sweeper doesn't close it. No-op for an unknown session.
func touchTerminalSession(sessionID string) {
	terminalSessionsMu.Lock()
	defer terminalSessionsMu.Unlock()
	if meta, ok := terminalSessions[sessionID]; ok {
		meta.lastActivity = time.Now()
		terminalSessions[sessionID] = meta
	}
}

// countTerminalSessionsForWorker returns how many sessions are currently
// tracked against workerID, for the concurrent-session cap.
func countTerminalSessionsForWorker(workerID string) int {
	terminalSessionsMu.Lock()
	defer terminalSessionsMu.Unlock()
	n := 0
	for _, meta := range terminalSessions {
		if meta.workerID == workerID {
			n++
		}
	}
	return n
}

// idleTerminalSessions returns sessions whose last activity is older than
// terminalIdleTimeout, for the sweeper to close.
func idleTerminalSessions() map[string]terminalSessionMeta {
	terminalSessionsMu.Lock()
	defer terminalSessionsMu.Unlock()
	cutoff := time.Now().Add(-terminalIdleTimeout)
	idle := make(map[string]terminalSessionMeta)
	for id, meta := range terminalSessions {
		if meta.lastActivity.Before(cutoff) {
			idle[id] = meta
		}
	}
	return idle
}

// allTerminalSessions returns a snapshot of every tracked session, for the
// admin kill-switch listing.
func allTerminalSessions() map[string]terminalSessionMeta {
	terminalSessionsMu.Lock()
	defer terminalSessionsMu.Unlock()
	out := make(map[string]terminalSessionMeta, len(terminalSessions))
	for id, meta := range terminalSessions {
		out[id] = meta
	}
	return out
}

var terminalSweeperOnce sync.Once

// startTerminalIdleSweeper periodically closes sessions idle past
// terminalIdleTimeout. Idempotent — only the first call actually starts the
// background goroutine, since terminalSessions is process-global state and
// registerTerminalRoutes could in principle run more than once in tests.
func startTerminalIdleSweeper(app core.App, sender terminalSender, hasSender bool) {
	terminalSweeperOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(terminalIdleSweepInterval)
			defer ticker.Stop()
			for range ticker.C {
				for sessionID, meta := range idleTerminalSessions() {
					log.Printf("[terminal] closing idle session %s (worker=%s, idle>%s)", sessionID, meta.workerID, terminalIdleTimeout)
					if hasSender {
						_ = sender.SendMessage(meta.workerID, protocol.MsgTerminalClose, protocol.TerminalCloseCommand{SessionID: sessionID})
					}
					forgetTerminalSession(sessionID)
					audit.RecordSystem(app, "terminal.idle_close", "stack", meta.stackID, audit.StatusSuccess, "")
				}
			}
		}()
	})
}

// authorizeTerminalSession loads sessionID's routing record and confirms the
// requesting actor is the one who opened it — a session is a private pty
// into a specific container, not something another operator should be able
// to attach input to or tear down just because they share the operator role.
func (rr routeRegistrar) authorizeTerminalSession(e *core.RequestEvent, sessionID string) (terminalSessionMeta, bool) {
	meta, ok := lookupTerminalSession(sessionID)
	if !ok {
		_ = e.JSON(http.StatusNotFound, map[string]string{"error": "unknown or expired terminal session"})
		return terminalSessionMeta{}, false
	}
	_, _, actorID := rbac.ResolveActor(e)
	if actorID == "" || actorID != meta.actorID {
		_ = e.JSON(http.StatusForbidden, map[string]string{"error": "terminal session belongs to a different user"})
		return terminalSessionMeta{}, false
	}
	return meta, true
}

// authorizeTerminalSessionForClose is authorizeTerminalSession plus a kill
// switch: an actor with CapManageSecurity (admin-tier) can close any
// session, not just their own — for a runaway or forgotten shell that its
// owner isn't around to end. Only close is admin-overridable; input/resize/
// stream still require ownership, so an admin can shut a session down but
// can't silently ride along on someone else's terminal.
func (rr routeRegistrar) authorizeTerminalSessionForClose(e *core.RequestEvent, sessionID string) (terminalSessionMeta, bool) {
	meta, ok := lookupTerminalSession(sessionID)
	if !ok {
		_ = e.JSON(http.StatusNotFound, map[string]string{"error": "unknown or expired terminal session"})
		return terminalSessionMeta{}, false
	}
	_, _, actorID := rbac.ResolveActor(e)
	if actorID != "" && actorID == meta.actorID {
		return meta, true
	}
	if rbac.Can(e, rbac.CapManageSecurity) {
		return meta, true
	}
	_ = e.JSON(http.StatusForbidden, map[string]string{"error": "terminal session belongs to a different user"})
	return terminalSessionMeta{}, false
}

func (rr routeRegistrar) registerTerminalRoutes() {
	sender, hasSender := rr.workerSvc.(terminalSender)
	startTerminalIdleSweeper(rr.app, sender, hasSender)

	rr.r.POST("/api/custom/stacks/{id}/terminal/sessions", func(e *core.RequestEvent) error {
		stackID := e.Request.PathValue("id")
		var body struct {
			ContainerID string   `json:"container_id"`
			Shell       []string `json:"shell,omitempty"`
			Rows        uint     `json:"rows,omitempty"`
			Cols        uint     `json:"cols,omitempty"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil || strings.TrimSpace(body.ContainerID) == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "container_id required"})
		}
		if !hasSender {
			return e.JSON(http.StatusServiceUnavailable, map[string]string{"error": "worker dispatcher unavailable"})
		}

		stack, projectName, workerID, ok := rr.resolveStackAndWorker(e, stackID)
		if !ok {
			return nil
		}

		sessionID := uuid.NewString()
		_, _, actorID := rbac.ResolveActor(e)

		// Denormalized display fields for the Sessions history table — resolved
		// once, at open time, so the row still reads sensibly after the stack,
		// service, user, or worker it points to is renamed or deleted (see
		// pb_migrations/67_terminal_sessions_denormalize.go).
		stackName := stack.GetString("name")
		var serviceName, containerName string
		if svc, err := rr.app.FindFirstRecordByFilter(
			"stack_services", "stack = {:stack} && container_id = {:cid}",
			map[string]any{"stack": stackID, "cid": body.ContainerID},
		); err == nil {
			serviceName = svc.GetString("service_name")
			containerName = svc.GetString("container_name")
		}
		var userEmail string
		if u, err := rr.app.FindRecordById("users", actorID); err == nil {
			userEmail = u.GetString("email")
		}
		var workerHostname string
		if w, err := rr.app.FindRecordById("workers", workerID); err == nil {
			workerHostname = w.GetString("hostname")
		}

		limit := maxConcurrentSessionsPerWorker()
		if !reserveTerminalSession(sessionID, workerID, limit, terminalSessionMeta{workerID: workerID, actorID: actorID, stackID: stackID}) {
			return e.JSON(http.StatusTooManyRequests, map[string]string{"error": fmt.Sprintf("worker already has %d open terminal sessions — close one before opening another", limit)})
		}

		if err := sender.SendMessage(workerID, protocol.MsgTerminalOpen, protocol.TerminalOpenCommand{
			CommandID:   fmt.Sprintf("terminal-open-%s", sessionID),
			SessionID:   sessionID,
			StackID:     stackID,
			ProjectName: projectName,
			ContainerID: body.ContainerID,
			Shell:       body.Shell,
			Rows:        body.Rows,
			Cols:        body.Cols,
		}); err != nil {
			forgetTerminalSession(sessionID)
			return e.JSON(http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		}

		rows, cols := body.Rows, body.Cols
		if rows == 0 {
			rows = 24
		}
		if cols == 0 {
			cols = 80
		}
		termsession.CreateRecord(rr.app, termsession.CreateRecordParams{
			SessionID:      sessionID,
			StackID:        stackID,
			StackName:      stackName,
			UserID:         actorID,
			UserEmail:      userEmail,
			WorkerID:       workerID,
			WorkerHostname: workerHostname,
			ContainerID:    body.ContainerID,
			ContainerName:  containerName,
			ServiceName:    serviceName,
			Rows:           rows,
			Cols:           cols,
		})

		audit.RecordRequest(rr.app, e, audit.Event{
			Action:       "terminal.open",
			ResourceType: "stack",
			ResourceID:   stackID,
			Metadata:     map[string]any{"session_id": sessionID, "container_id": body.ContainerID},
		})

		return e.JSON(http.StatusOK, map[string]string{"session_id": sessionID})
	}).BindFunc(rbac.Require(rbac.CapUseTerminal))

	rr.r.GET("/api/custom/terminal/sessions/{sessionId}/stream", func(e *core.RequestEvent) error {
		sessionID := e.Request.PathValue("sessionId")
		if _, ok := rr.authorizeTerminalSession(e, sessionID); !ok {
			return nil
		}

		e.Response.Header().Set("Content-Type", "text/event-stream")
		e.Response.Header().Set("Cache-Control", "no-cache")
		e.Response.Header().Set("Connection", "keep-alive")
		e.Response.WriteHeader(http.StatusOK)
		flusher, _ := e.Response.(http.Flusher)
		if flusher != nil {
			flusher.Flush()
		}

		sub, unsubscribe := rr.termBroker.Subscribe(sessionID)
		defer unsubscribe()

		ctx := e.Request.Context()
		for {
			select {
			case <-ctx.Done():
				return nil
			case ev, open := <-sub:
				if !open {
					return nil
				}
				if ev.Closed {
					forgetTerminalSession(sessionID)
					// ev.Error carries the worker-side failure reason (e.g. exec
					// create/attach errors for a bad shell path) — must be JSON
					// encoded, not Sprintf'd, since it's arbitrary Docker/OCI
					// error text that can contain quotes.
					payload, err := json.Marshal(map[string]any{"exit_code": ev.ExitCode, "error": ev.Error})
					if err != nil {
						payload = []byte(`{"exit_code":-1}`)
					}
					fmt.Fprintf(e.Response, "event: closed\ndata: %s\n\n", payload)
					if flusher != nil {
						flusher.Flush()
					}
					return nil
				}
				fmt.Fprintf(e.Response, "data: %s\n\n", base64.StdEncoding.EncodeToString(ev.Data))
				if flusher != nil {
					flusher.Flush()
				}
			}
		}
	}).BindFunc(rbac.Require(rbac.CapUseTerminal))

	rr.r.POST("/api/custom/terminal/sessions/{sessionId}/input", func(e *core.RequestEvent) error {
		sessionID := e.Request.PathValue("sessionId")
		meta, ok := rr.authorizeTerminalSession(e, sessionID)
		if !ok {
			return nil
		}
		var body struct {
			Data string `json:"data"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
		}
		if !hasSender {
			return e.JSON(http.StatusServiceUnavailable, map[string]string{"error": "worker dispatcher unavailable"})
		}
		if err := sender.SendMessage(meta.workerID, protocol.MsgTerminalInput, protocol.TerminalInputCommand{
			SessionID: sessionID,
			DataB64:   base64.StdEncoding.EncodeToString([]byte(body.Data)),
		}); err != nil {
			return e.JSON(http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		}
		touchTerminalSession(sessionID)
		return e.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}).BindFunc(rbac.Require(rbac.CapUseTerminal))

	rr.r.POST("/api/custom/terminal/sessions/{sessionId}/resize", func(e *core.RequestEvent) error {
		sessionID := e.Request.PathValue("sessionId")
		meta, ok := rr.authorizeTerminalSession(e, sessionID)
		if !ok {
			return nil
		}
		var body struct {
			Rows uint `json:"rows"`
			Cols uint `json:"cols"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil || body.Rows == 0 || body.Cols == 0 {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "rows and cols required"})
		}
		if !hasSender {
			return e.JSON(http.StatusServiceUnavailable, map[string]string{"error": "worker dispatcher unavailable"})
		}
		if err := sender.SendMessage(meta.workerID, protocol.MsgTerminalResize, protocol.TerminalResizeCommand{
			SessionID: sessionID,
			Rows:      body.Rows,
			Cols:      body.Cols,
		}); err != nil {
			return e.JSON(http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		}
		return e.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}).BindFunc(rbac.Require(rbac.CapUseTerminal))

	rr.r.POST("/api/custom/terminal/sessions/{sessionId}/close", func(e *core.RequestEvent) error {
		sessionID := e.Request.PathValue("sessionId")
		meta, ok := rr.authorizeTerminalSessionForClose(e, sessionID)
		if !ok {
			return nil
		}
		if hasSender {
			_ = sender.SendMessage(meta.workerID, protocol.MsgTerminalClose, protocol.TerminalCloseCommand{SessionID: sessionID})
		}
		forgetTerminalSession(sessionID)

		_, _, actorID := rbac.ResolveActor(e)
		action := "terminal.close"
		if actorID != meta.actorID {
			action = "terminal.admin_close"
		}
		audit.RecordRequest(rr.app, e, audit.Event{
			Action:       action,
			ResourceType: "stack",
			ResourceID:   meta.stackID,
			Metadata:     map[string]any{"session_id": sessionID, "session_owner": meta.actorID},
		})

		return e.JSON(http.StatusOK, map[string]string{"status": "closed"})
	}).BindFunc(rbac.Require(rbac.CapUseTerminal))

	// History: the Sessions tab on Audit settings — a paginated, filterable
	// listing across every user/stack, same shape as GET /api/custom/audit-logs.
	// Admin-only (CapViewAuditLogs), since it's compliance/audit surface, not
	// a per-operator "my past sessions" view.
	rr.r.GET("/api/custom/terminal-sessions", func(e *core.RequestEvent) error {
		q := e.Request.URL.Query()
		page := parsePositiveInt(q.Get("page"), 1)
		perPage := parsePositiveInt(q.Get("perPage"), 25)
		if perPage > 100 {
			perPage = 100
		}

		filter, where, params := terminalSessionFilters(q.Get("from"), q.Get("to"), q.Get("stack_name"), q.Get("user_email"), q.Get("service_name"), q.Get("status"))
		offset := (page - 1) * perPage

		records, err := rr.app.FindRecordsByFilter("terminal_sessions", filter, "-started_at", perPage, offset, params)
		if err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}

		total, err := countTerminalSessions(rr.app, where, params)
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		items := make([]map[string]any, 0, len(records))
		for _, rec := range records {
			items = append(items, map[string]any{
				"id":              rec.Id,
				"session_id":      rec.GetString("session_id"),
				"stack_name":      rec.GetString("stack_name"),
				"service_name":    rec.GetString("service_name"),
				"container_name":  rec.GetString("container_name"),
				"container_id":    rec.GetString("container_id"),
				"user_email":      rec.GetString("user_email"),
				"worker_hostname": rec.GetString("worker_hostname"),
				"started_at":      rec.GetDateTime("started_at").String(),
				"ended_at":        rec.GetDateTime("ended_at").String(),
				"exit_code":       rec.GetInt("exit_code"),
				"status":          rec.GetString("status"),
			})
		}

		return e.JSON(http.StatusOK, map[string]any{
			"page":       page,
			"perPage":    perPage,
			"totalItems": total,
			"items":      items,
		})
	}).BindFunc(rbac.Require(rbac.CapViewAuditLogs))

	rr.r.GET("/api/custom/terminal/sessions/{sessionId}/transcript", func(e *core.RequestEvent) error {
		sessionID := e.Request.PathValue("sessionId")
		_, _, actorID := rbac.ResolveActor(e)

		rec, err := rr.app.FindFirstRecordByFilter("terminal_sessions", "session_id = {:id}", map[string]any{"id": sessionID})
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "session not found"})
		}
		// Owners can replay their own sessions; CapViewAuditLogs (admin-tier)
		// can replay anyone's, same audit-visibility posture as the Sessions
		// listing above.
		if rec.GetString("user") != actorID && !rbac.Can(e, rbac.CapViewAuditLogs) {
			return e.JSON(http.StatusForbidden, map[string]string{"error": "terminal session belongs to a different user"})
		}

		data, err := os.ReadFile(rec.GetString("transcript_path"))
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "transcript not available (expired or never recorded)"})
		}

		e.Response.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = e.Response.Write(data)
		return nil
	}).BindFunc(rbac.Require(rbac.CapUseTerminal))

	// Admin kill-switch visibility: every currently open session across every
	// user, so an admin knows what they'd be closing via the close route's
	// CapManageSecurity override above.
	rr.r.GET("/api/custom/terminal/sessions/active", func(e *core.RequestEvent) error {
		sessions := allTerminalSessions()
		out := make([]map[string]any, 0, len(sessions))
		for id, meta := range sessions {
			out = append(out, map[string]any{
				"session_id":    id,
				"stack":         meta.stackID,
				"worker":        meta.workerID,
				"user":          meta.actorID,
				"last_activity": meta.lastActivity,
			})
		}
		return e.JSON(http.StatusOK, out)
	}).BindFunc(rbac.Require(rbac.CapManageSecurity))
}

// terminalSessionFilters builds the FindRecordsByFilter expression and the
// matching raw-SQL WHERE clause (for countTerminalSessions) from the Sessions
// tab's query params. Mirrors internal/routes/audit.go's auditLogFilters.
func terminalSessionFilters(from, to, stackName, userEmail, serviceName, status string) (string, string, dbx.Params) {
	var filterParts []string
	var whereParts []string
	params := dbx.Params{}

	add := func(field, op, value, name string) {
		if value == "" {
			return
		}
		filterParts = append(filterParts, field+" "+op+" {:"+name+"}")
		whereParts = append(whereParts, field+" "+op+" {:"+name+"}")
		params[name] = value
	}

	add("started_at", ">=", strings.TrimSpace(from), "from")
	toVal := strings.TrimSpace(to)
	if len(toVal) == 10 {
		if t, err := time.Parse("2006-01-02", toVal); err == nil {
			add("started_at", "<", t.AddDate(0, 0, 1).Format("2006-01-02"), "to")
		} else {
			add("started_at", "<=", toVal, "to")
		}
	} else {
		add("started_at", "<=", toVal, "to")
	}
	add("stack_name", "=", strings.TrimSpace(stackName), "stack_name")
	add("user_email", "=", strings.TrimSpace(userEmail), "user_email")
	add("service_name", "=", strings.TrimSpace(serviceName), "service_name")
	add("status", "=", strings.TrimSpace(status), "status")

	return strings.Join(filterParts, " && "), strings.Join(whereParts, " AND "), params
}

func countTerminalSessions(app core.App, where string, params dbx.Params) (int64, error) {
	query := "SELECT COUNT(*) FROM terminal_sessions"
	if where != "" {
		query += " WHERE " + where
	}
	var total int64
	err := app.DB().NewQuery(query).Bind(params).Row(&total)
	return total, err
}
