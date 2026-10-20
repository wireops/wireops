package tools

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/wireops/wireops/mcp/client"
	"github.com/wireops/wireops/mcp/models"
)

// asciicast renders an asciicast-v2 transcript: the header line the server
// writes on session open, followed by one base64-encoded output event per
// chunk.
func asciicast(chunks ...string) string {
	var sb strings.Builder
	sb.WriteString(`{"version":2,"width":120,"height":24}` + "\n")
	for i, chunk := range chunks {
		line, _ := json.Marshal([]any{float64(i), "o", base64.StdEncoding.EncodeToString([]byte(chunk))})
		sb.Write(line)
		sb.WriteString("\n")
	}
	return sb.String()
}

// execServer fakes the four terminal routes exec_in_container drives. The
// transcript starts as the shell banner and gains the command's output once
// input has been sent, which is what the quiet-period loop polls for.
type execServer struct {
	mu         sync.Mutex
	calls      []string
	inputs     []string
	transcript string
	// transcriptStatus, when non-zero, is returned instead of the body.
	transcriptStatus int
	srv              *httptest.Server
}

func newExecServer(banner, output string) *execServer {
	es := &execServer{transcript: asciicast(banner)}
	es.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		es.mu.Lock()
		defer es.mu.Unlock()
		es.calls = append(es.calls, r.Method+" "+r.URL.Path)

		switch {
		case r.URL.Path == "/api/custom/stacks/stack1/services":
			_, _ = w.Write([]byte(`[{"service_name":"web","container_id":"c-web"}]`))
		case strings.HasSuffix(r.URL.Path, "/terminal/sessions"):
			_, _ = w.Write([]byte(`{"session_id":"sess-1"}`))
		case strings.HasSuffix(r.URL.Path, "/input"):
			raw, _ := io.ReadAll(r.Body)
			var body struct {
				Data string `json:"data"`
			}
			_ = json.Unmarshal(raw, &body)
			es.inputs = append(es.inputs, body.Data)
			es.transcript = asciicast(banner, output)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case strings.HasSuffix(r.URL.Path, "/transcript"):
			if es.transcriptStatus != 0 {
				w.WriteHeader(es.transcriptStatus)
				_, _ = w.Write([]byte(`{"error":"transcript not available"}`))
				return
			}
			_, _ = w.Write([]byte(es.transcript))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	return es
}

func (e *execServer) close()                 { e.srv.Close() }
func (e *execServer) client() *client.Client { return client.New(e.srv.URL) }

func (e *execServer) seen() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.calls...)
}

func (e *execServer) sentInputs() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.inputs...)
}

func containsCall(calls []string, want string) bool {
	for _, c := range calls {
		if c == want {
			return true
		}
	}
	return false
}

func TestExecInContainerReturnsCommandOutput(t *testing.T) {
	es := newExecServer("/ # ", "bin\netc\nusr\n")
	defer es.close()

	_, out, err := execInContainer(es.client())(ctxWithKey(), nil, models.ExecInContainerInput{
		StackID:     "stack1",
		ServiceName: "web",
		Command:     "ls /",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("unexpected output type %T", out)
	}
	// The shell banner captured before the command was sent must not be
	// reported as the command's output.
	if got := result["output"]; got != "bin\netc\nusr\n" {
		t.Fatalf("unexpected output %q", got)
	}
	if result["timed_out"] != false {
		t.Fatalf("expected timed_out false, got %v", result["timed_out"])
	}
	if result["container_id"] != "c-web" || result["service_name"] != "web" {
		t.Fatalf("expected the resolved container reported back, got %v", result)
	}

	if inputs := es.sentInputs(); len(inputs) != 1 || inputs[0] != "ls /\n" {
		t.Fatalf("expected the command sent with a trailing newline, got %v", inputs)
	}
}

func TestExecInContainerAlwaysClosesTheSession(t *testing.T) {
	es := newExecServer("/ # ", "ok\n")
	defer es.close()

	// A transcript that never becomes readable is the interesting failure:
	// the pty is already open, so it still has to be closed.
	es.mu.Lock()
	es.transcriptStatus = http.StatusInternalServerError
	es.mu.Unlock()

	_, _, err := execInContainer(es.client())(ctxWithKey(), nil, models.ExecInContainerInput{
		StackID:     "stack1",
		ServiceName: "web",
		Command:     "ls /",
	})
	if err == nil {
		t.Fatal("expected the transcript failure to surface")
	}
	if !containsCall(es.seen(), "POST /api/custom/terminal/sessions/sess-1/close") {
		t.Fatalf("expected the session to be closed anyway, calls: %v", es.seen())
	}
}

func TestExecInContainerTimesOutWithoutOutput(t *testing.T) {
	es := newExecServer("/ # ", "")
	defer es.close()

	_, out, err := execInContainer(es.client())(ctxWithKey(), nil, models.ExecInContainerInput{
		StackID:        "stack1",
		ServiceName:    "web",
		Command:        "sleep 600",
		TimeoutSeconds: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result := out.(map[string]any)
	if result["timed_out"] != true {
		t.Fatalf("expected timed_out true, got %v", result)
	}
	if !containsCall(es.seen(), "POST /api/custom/terminal/sessions/sess-1/close") {
		t.Fatalf("expected the session closed after the timeout, calls: %v", es.seen())
	}
}

func TestExecInContainerRequiresCommand(t *testing.T) {
	es := newExecServer("/ # ", "")
	defer es.close()

	_, _, err := execInContainer(es.client())(ctxWithKey(), nil, models.ExecInContainerInput{
		StackID:     "stack1",
		ServiceName: "web",
		Command:     "   ",
	})
	if err == nil {
		t.Fatal("expected a blank command to fail")
	}
	if len(es.seen()) != 0 {
		t.Fatalf("expected no session to be opened, calls: %v", es.seen())
	}
}

func TestExecInContainerMissingAPIKey(t *testing.T) {
	es := newExecServer("/ # ", "")
	defer es.close()

	_, _, err := execInContainer(es.client())(t.Context(), nil, models.ExecInContainerInput{
		StackID: "stack1",
		Command: "ls",
	})
	if err == nil {
		t.Fatal("expected error when no API key on context")
	}
}

func TestDecodeAsciicastSkipsHeaderAndMalformedLines(t *testing.T) {
	raw := asciicast("hello ") + `[1.0,"i","aGk="]` + "\n" + `[2.0,"o","!!!not-base64!!!"]` + "\n" + "{tor" + "\n"
	raw += fmt.Sprintf("[3.0,\"o\",%q]\n", base64.StdEncoding.EncodeToString([]byte("world")))

	if got := decodeAsciicast([]byte(raw)); got != "hello world" {
		t.Fatalf("unexpected decode result %q", got)
	}
}
