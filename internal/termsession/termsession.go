// Package termsession records interactive terminal sessions (phase 2 of the
// audited web terminal, see docs/TERMINAL.md): a terminal_sessions row per
// session plus an asciicast-v2-style transcript file on disk. Retention is
// short by default (30 days) and deliberate — a transcript captures
// everything typed and shown in the session, secrets included.
package termsession

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"

	"github.com/wireops/wireops/internal/config"
)

const DefaultRetentionDays = 30

const collectionName = "terminal_sessions"

// sessionStart tracks each open session's start time in memory so
// AppendTranscript can compute asciicast-v2 relative timestamps without a
// DB round trip per chunk. Cleared on Close.
var (
	startMu sync.Mutex
	starts  = map[string]time.Time{}
)

// openFiles holds each active session's transcript file handle, opened once
// in CreateRecord and closed in CloseRecord, so AppendTranscript — called
// once per output chunk, potentially many times a second for a busy session
// — appends to an already-open handle instead of paying an open+close
// syscall pair on every chunk.
var (
	filesMu sync.Mutex
	files   = map[string]*os.File{}
)

func StorageDir() string {
	return config.GetTerminalSessionsStoragePath()
}

// transcriptPath maps sessionID to its .cast file under StorageDir().
// sessionID is meant to be an opaque identifier (uuid.NewString(), assigned
// once in internal/routes/terminal.go's open handler) but filepath.Base
// defensively collapses it to a single path component regardless — any
// separators or ".." a caller somehow passed in would resolve to at most
// the last segment, never escaping StorageDir().
func transcriptPath(sessionID string) string {
	return filepath.Join(StorageDir(), filepath.Base(sessionID)+".cast")
}

// RetentionDays returns the configured transcript retention window, falling
// back to DefaultRetentionDays if unset or invalid — same pattern as
// internal/audit.AuditRetentionDays.
func RetentionDays(app core.App) int {
	records, err := app.FindAllRecords("app_settings")
	if err != nil || len(records) == 0 {
		return DefaultRetentionDays
	}
	days := records[0].GetInt("terminal_retention_days")
	if days <= 0 {
		return DefaultRetentionDays
	}
	return days
}

// CreateRecordParams carries a new session's identifying data. Name/email
// fields are denormalized copies of their relation targets (stack, user,
// worker) at session-open time — the terminal_sessions row is a compliance
// record shown in the Sessions tab of Audit settings, and must keep
// something meaningful to display even after the stack, user, or worker it
// pointed to is deleted (see pb_migrations/67_terminal_sessions_denormalize.go).
type CreateRecordParams struct {
	SessionID string

	StackID   string
	StackName string

	UserID    string
	UserEmail string

	WorkerID       string
	WorkerHostname string

	ContainerID   string
	ContainerName string
	ServiceName   string

	Rows, Cols uint
}

// CreateRecord persists a new terminal_sessions row and writes the
// asciicast-v2 header line to a fresh transcript file. Best-effort: a
// failure here is logged, not fatal — recording a session's audit trail
// must never block the session itself from opening.
func CreateRecord(app core.App, p CreateRecordParams) {
	sessionID := p.SessionID
	now := time.Now()

	startMu.Lock()
	starts[sessionID] = now
	startMu.Unlock()

	if err := os.MkdirAll(StorageDir(), 0o700); err != nil {
		log.Printf("[termsession] failed to create storage dir: %v", err)
		return
	}

	header := map[string]any{
		"version":   2,
		"width":     p.Cols,
		"height":    p.Rows,
		"timestamp": now.Unix(),
	}
	line, err := json.Marshal(header)
	if err == nil {
		line = append(line, '\n')
		f, ferr := os.OpenFile(transcriptPath(sessionID), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if ferr != nil {
			log.Printf("[termsession] failed to open transcript file for %s: %v", sessionID, ferr)
		} else if _, werr := f.Write(line); werr != nil {
			log.Printf("[termsession] failed to write transcript header for %s: %v", sessionID, werr)
			f.Close()
		} else {
			filesMu.Lock()
			files[sessionID] = f
			filesMu.Unlock()
		}
	}

	col, err := app.FindCollectionByNameOrId(collectionName)
	if err != nil {
		log.Printf("[termsession] terminal_sessions collection missing: %v", err)
		return
	}
	rec := core.NewRecord(col)
	rec.Set("session_id", sessionID)
	rec.Set("stack", p.StackID)
	rec.Set("stack_name", p.StackName)
	rec.Set("user", p.UserID)
	rec.Set("user_email", p.UserEmail)
	rec.Set("worker", p.WorkerID)
	rec.Set("worker_hostname", p.WorkerHostname)
	rec.Set("container_id", p.ContainerID)
	rec.Set("container_name", p.ContainerName)
	rec.Set("service_name", p.ServiceName)
	rec.Set("transcript_path", transcriptPath(sessionID))
	rec.Set("started_at", now)
	rec.Set("status", "open")
	rec.Set("expires_at", now.AddDate(0, 0, RetentionDays(app)))
	if err := app.Save(rec); err != nil {
		log.Printf("[termsession] failed to save session record for %s: %v", sessionID, err)
	}
}

// AppendTranscript appends one asciicast-v2-style output event to
// sessionID's transcript file. Best-effort, same reasoning as CreateRecord.
//
// The event text is base64-encoded rather than a raw Go string: pty output
// isn't guaranteed to be valid UTF-8 (binary tool output, or a multi-byte
// rune split across two separate reads from the worker), and encoding/json
// silently replaces invalid UTF-8 with U+FFFD when marshaling a string —
// which would irreversibly corrupt the recorded transcript. Base64 round
// trips exactly regardless of content; the frontend replay path decodes it
// back to raw bytes before writing to xterm.
func AppendTranscript(sessionID string, data []byte) {
	startMu.Lock()
	start, ok := starts[sessionID]
	startMu.Unlock()
	if !ok {
		// No CreateRecord for this session (e.g. recording disabled, or the
		// server restarted mid-session) — nothing to append to.
		return
	}

	elapsed := time.Since(start).Seconds()
	line, err := json.Marshal([]any{elapsed, "o", base64.StdEncoding.EncodeToString(data)})
	if err != nil {
		return
	}
	line = append(line, '\n')

	// Hold filesMu across the lookup AND the write: CloseRecord can now run
	// on a different goroutine than this call (idle sweeper / manual close
	// eagerly close the record without waiting for the worker's ack) and
	// closes+deletes the same file handle under this same mutex — without
	// holding it through the write, a lookup-then-close-then-write race
	// could write to (or double-close) a file handle CloseRecord just tore
	// down.
	filesMu.Lock()
	defer filesMu.Unlock()
	f, ok := files[sessionID]
	if !ok {
		return
	}
	if _, err := f.Write(line); err != nil {
		log.Printf("[termsession] failed to write transcript chunk for %s: %v", sessionID, err)
	}
}

// CloseRecord marks a session's terminal_sessions row as closed with its
// exit code. Clears the in-memory start-time entry regardless of DB outcome
// so a stale entry can't linger.
func CloseRecord(app core.App, sessionID string, exitCode int) {
	startMu.Lock()
	delete(starts, sessionID)
	startMu.Unlock()

	filesMu.Lock()
	if f, ok := files[sessionID]; ok {
		f.Close()
		delete(files, sessionID)
	}
	filesMu.Unlock()

	rec, err := app.FindFirstRecordByFilter(collectionName, "session_id = {:id}", map[string]any{"id": sessionID})
	if err != nil {
		return
	}
	rec.Set("ended_at", time.Now())
	rec.Set("exit_code", exitCode)
	rec.Set("status", "closed")
	if err := app.Save(rec); err != nil {
		log.Printf("[termsession] failed to close session record for %s: %v", sessionID, err)
	}
}

// PurgeExpired deletes terminal_sessions rows past their retention window
// along with their transcript files. Mirrors internal/audit.PurgeExpired's
// shape but needs a file delete per row (transcripts aren't in SQLite), so
// it can't be a single DELETE statement.
func PurgeExpired(app core.App) error {
	records, err := app.FindRecordsByFilter(collectionName, "expires_at < {:now}", "", 0, 0, map[string]any{"now": types.NowDateTime()})
	if err != nil {
		return fmt.Errorf("find expired terminal sessions: %w", err)
	}

	for _, rec := range records {
		path := rec.GetString("transcript_path")
		if path != "" {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				log.Printf("[termsession] failed to remove transcript %s: %v", path, err)
			}
		}
		if err := app.Delete(rec); err != nil {
			log.Printf("[termsession] failed to delete session record %s: %v", rec.Id, err)
		}
	}
	return nil
}
