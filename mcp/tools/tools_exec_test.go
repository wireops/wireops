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
	"time"

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

// execServer fakes the four terminal routes exec_in_container drives. It
// behaves like the real pty: the input it is sent is echoed straight back into
// the transcript, and the command's output (plus the end-of-command marker the
// tool appends, resolved with exitCode) only follows after echoDelay. That gap
// is what a quiet-period-based wait would mistake for the end of the command.
type execServer struct {
	mu         sync.Mutex
	calls      []string
	inputs     []string
	transcript string
	// transcriptStatus, when non-zero, is returned instead of the body.
	transcriptStatus int
	// echoDelay is how long after the input the command's output appears; the
	// echo itself is immediate.
	echoDelay time.Duration
	// exitCode is reported by the marker line. A negative value suppresses the
	// marker entirely, standing in for a command that never finishes.
	exitCode int
	srv      *httptest.Server
}

func newExecServer(banner, output string) *execServer {
	es := &execServer{transcript: asciicast(banner)}
	es.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		es.mu.Lock()
		defer es.mu.Unlock()
		es.calls = append(es.calls, r.Method+" "+r.URL.Path)

		switch {
		case r.URL.Path == "/api/custom/stacks/stack0000000001/services":
			writeFixture(w, `[{"service_name":"web","container_id":"c-web"}]`)
		case strings.HasSuffix(r.URL.Path, "/exec"):
			// This fake stands in for a worker too old for the one-shot exec
			// command, which is exactly the case the pty path below still
			// has to cover.
			w.WriteHeader(http.StatusConflict)
			writeFixture(w, `{"error":"the assigned worker does not support one-shot exec","required_capability":"exec_one_shot_v1"}`)
		case strings.HasSuffix(r.URL.Path, "/terminal/sessions"):
			writeFixture(w, `{"session_id":"sess-1"}`)
		case strings.HasSuffix(r.URL.Path, "/input"):
			raw, _ := io.ReadAll(r.Body)
			var body struct {
				Data string `json:"data"`
			}
			_ = json.Unmarshal(raw, &body)
			es.inputs = append(es.inputs, body.Data)

			// The pty echoes every line it is given, right away.
			echo := strings.ReplaceAll(body.Data, "\n", "\r\n")
			es.transcript = asciicast(banner, echo)

			marker := execMarkerPrefix + "sess-1"
			tail := output
			if es.exitCode >= 0 {
				tail += fmt.Sprintf("\n%s:%d\n", marker, es.exitCode)
			}
			if es.echoDelay > 0 {
				go func(delay time.Duration) {
					time.Sleep(delay)
					es.mu.Lock()
					defer es.mu.Unlock()
					es.transcript = asciicast(banner, echo, tail)
				}(es.echoDelay)
			} else {
				es.transcript = asciicast(banner, echo, tail)
			}
			writeFixture(w, `{"status":"ok"}`)
		case strings.HasSuffix(r.URL.Path, "/transcript"):
			if es.transcriptStatus != 0 {
				w.WriteHeader(es.transcriptStatus)
				writeFixture(w, `{"error":"transcript not available"}`)
				return
			}
			writeFixture(w, es.transcript)
		default:
			writeFixture(w, `{}`)
		}
	}))
	return es
}

func (e *execServer) close() { e.srv.Close() }

// slowOutput makes the command's output appear only after d; the echo of the
// input stays immediate, which is the gap that matters.
func (e *execServer) slowOutput(d time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.echoDelay = d
}

// exits sets the status the end-of-command marker reports. A negative code
// suppresses the marker entirely, standing in for a command that never
// finishes.
func (e *execServer) exits(code int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.exitCode = code
}
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
		StackID:     "stack0000000001",
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
	if result["exit_code"] != 0 {
		t.Fatalf("expected exit_code 0, got %v", result["exit_code"])
	}
	if result["container_id"] != "c-web" || result["service_name"] != "web" {
		t.Fatalf("expected the resolved container reported back, got %v", result)
	}

	inputs := es.sentInputs()
	if len(inputs) != 1 {
		t.Fatalf("expected exactly one input, got %v", inputs)
	}
	if !strings.HasPrefix(inputs[0], "ls /\n") {
		t.Fatalf("expected the command sent first, with a trailing newline, got %q", inputs[0])
	}
	// The marker has to be a printf *argument*: inline in the format string,
	// the echoed input line would carry "<marker>:" too and be mistaken for
	// the end of the output.
	if !strings.Contains(inputs[0], "'"+execMarkerPrefix+"sess-1'") {
		t.Fatalf("expected the marker passed as a quoted argument, got %q", inputs[0])
	}
}

func TestExecInContainerWaitsThroughASilentStart(t *testing.T) {
	// A command that prints nothing for a while before its first byte: the
	// echo of the input has already grown the transcript, so a wait that
	// settled on "nothing new for a moment" would return the echo alone and
	// report the command as finished.
	es := newExecServer("/ # ", "42G\t/\n")
	defer es.close()
	es.slowOutput(1500 * time.Millisecond)

	_, out, err := execInContainer(es.client())(ctxWithKey(), nil, models.ExecInContainerInput{
		StackID:        "stack0000000001",
		ServiceName:    "web",
		Command:        "du -sh /",
		TimeoutSeconds: 10,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result := out.(map[string]any)
	if got := result["output"]; got != "42G\t/\n" {
		t.Fatalf("expected the slow output to be waited for, got %q", got)
	}
	if result["timed_out"] != false {
		t.Fatalf("expected timed_out false, got %v", result)
	}
}

func TestExecInContainerReportsExitCode(t *testing.T) {
	es := newExecServer("/ # ", "cat: /nope: No such file or directory\n")
	defer es.close()
	es.exits(1)

	_, out, err := execInContainer(es.client())(ctxWithKey(), nil, models.ExecInContainerInput{
		StackID:     "stack0000000001",
		ServiceName: "web",
		Command:     "cat /nope",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result := out.(map[string]any)
	if result["exit_code"] != 1 {
		t.Fatalf("expected exit_code 1, got %v", result["exit_code"])
	}
	// The marker line itself is not part of the command's output.
	if got, _ := result["output"].(string); strings.Contains(got, execMarkerPrefix) {
		t.Fatalf("expected the marker stripped from the output, got %q", got)
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
		StackID:     "stack0000000001",
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
	// A command that never finishes: the marker never arrives, so only the
	// deadline ends the wait.
	es.exits(-1)

	_, out, err := execInContainer(es.client())(ctxWithKey(), nil, models.ExecInContainerInput{
		StackID:        "stack0000000001",
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
	// No marker means no exit status — 0 would read as "succeeded".
	if result["exit_code"] != nil {
		t.Fatalf("expected exit_code nil on timeout, got %v", result["exit_code"])
	}
	if !containsCall(es.seen(), "POST /api/custom/terminal/sessions/sess-1/close") {
		t.Fatalf("expected the session closed after the timeout, calls: %v", es.seen())
	}
}

func TestExecInContainerRequiresCommand(t *testing.T) {
	es := newExecServer("/ # ", "")
	defer es.close()

	_, _, err := execInContainer(es.client())(ctxWithKey(), nil, models.ExecInContainerInput{
		StackID:     "stack0000000001",
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
		StackID: "stack0000000001",
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

// execRouteServer fakes a worker that does support one-shot exec: the
// /exec route answers with separated streams and a real exit code.
func execRouteServer(t *testing.T, result string, status int) (*httptest.Server, *[]string, *[]map[string]any) {
	t.Helper()

	var calls []string
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch {
		case r.URL.Path == "/api/custom/stacks/stack0000000001/services":
			writeFixture(w, `[{"service_name":"web","container_id":"c-web"}]`)
		case strings.HasSuffix(r.URL.Path, "/exec"):
			raw, _ := io.ReadAll(r.Body)
			var body map[string]any
			_ = json.Unmarshal(raw, &body)
			bodies = append(bodies, body)
			if status != 0 {
				w.WriteHeader(status)
			}
			writeFixture(w, result)
		default:
			writeFixture(w, `{}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &calls, &bodies
}

func TestExecInContainerUsesTheExecRouteAndSeparatesStreams(t *testing.T) {
	srv, calls, bodies := execRouteServer(t,
		`{"stdout":"ok\n","stderr":"warn\n","exit_code":3,"timed_out":false}`, 0)

	handler := execInContainer(client.New(srv.URL))
	_, out, err := handler(ctxWithKey(), nil, models.ExecInContainerInput{
		StackID: "stack0000000001", ServiceName: "web", Command: "cat /etc/hosts",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result := out.(map[string]any)

	// The whole point of the new command: two streams and the runtime's own
	// exit code, not one interleaved blob and a parsed marker.
	if result["stdout"] != "ok\n" || result["stderr"] != "warn\n" {
		t.Errorf("expected separated streams, got %#v", result)
	}
	if result["exit_code"] != 3 {
		t.Errorf("exit_code = %v, want 3", result["exit_code"])
	}
	if _, present := result["session_id"]; present {
		t.Errorf("the exec route opens no terminal session: %#v", result)
	}
	if notice, present := result["_notice"]; present {
		t.Errorf("a clean run needs no notice, got %v", notice)
	}

	// No pty session is opened at all on this path.
	for _, call := range *calls {
		if strings.Contains(call, "/terminal/sessions") {
			t.Errorf("expected no terminal session, saw %v", *calls)
		}
	}
	// A command goes through a shell, so pipes and redirection work.
	cmd, _ := (*bodies)[0]["cmd"].([]any)
	if len(cmd) != 3 || cmd[0] != "/bin/sh" || cmd[1] != "-c" || cmd[2] != "cat /etc/hosts" {
		t.Errorf("unexpected argv %#v", cmd)
	}
}

func TestExecInContainerSendsArgvWithoutAShell(t *testing.T) {
	srv, _, bodies := execRouteServer(t, `{"stdout":"","stderr":"","exit_code":0}`, 0)

	handler := execInContainer(client.New(srv.URL))
	if _, _, err := handler(ctxWithKey(), nil, models.ExecInContainerInput{
		StackID: "stack0000000001", ServiceName: "web", Argv: []string{"cat", "/etc/hosts"},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cmd, _ := (*bodies)[0]["cmd"].([]any)
	if len(cmd) != 2 || cmd[0] != "cat" || cmd[1] != "/etc/hosts" {
		t.Errorf("expected the argv passed through verbatim, got %#v", cmd)
	}
}

func TestExecInContainerRejectsBothCommandAndArgv(t *testing.T) {
	srv, _, _ := execRouteServer(t, `{}`, 0)

	handler := execInContainer(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.ExecInContainerInput{
		StackID: "stack0000000001", ServiceName: "web", Command: "ls", Argv: []string{"ls"},
	})
	if err == nil {
		t.Fatal("expected an error when both are given")
	}
}

func TestExecInContainerReportsATimedOutRun(t *testing.T) {
	srv, _, _ := execRouteServer(t,
		`{"stdout":"partial","stderr":"","exit_code":-1,"timed_out":true}`, 0)

	handler := execInContainer(client.New(srv.URL))
	_, out, err := handler(ctxWithKey(), nil, models.ExecInContainerInput{
		StackID: "stack0000000001", ServiceName: "web", Command: "sleep 600",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result := out.(map[string]any)
	if result["timed_out"] != true {
		t.Errorf("expected timed_out, got %#v", result)
	}
	// A run with no exit status reports null, never 0.
	if result["exit_code"] != nil {
		t.Errorf("exit_code = %v, want nil", result["exit_code"])
	}
	if notice, _ := result["_notice"].(string); !strings.Contains(notice, "did not finish") {
		t.Errorf("expected a timeout notice, got %q", notice)
	}
}

func TestExecInContainerNoticesTruncation(t *testing.T) {
	srv, _, _ := execRouteServer(t,
		`{"stdout":"lots","stderr":"","exit_code":0,"stdout_truncated":true}`, 0)

	handler := execInContainer(client.New(srv.URL))
	_, out, err := handler(ctxWithKey(), nil, models.ExecInContainerInput{
		StackID: "stack0000000001", ServiceName: "web", Command: "cat /big",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if notice, _ := out.(map[string]any)["_notice"].(string); !strings.Contains(notice, "truncated") {
		t.Errorf("expected a truncation notice, got %q", notice)
	}
}

func TestExecInContainerSurfacesAnExecRouteError(t *testing.T) {
	// Anything but a 409 is the server's answer, not a capability gap, so it
	// must not silently fall back to the pty path.
	srv, calls, _ := execRouteServer(t, `{"error":"container does not belong to stack"}`, http.StatusForbidden)

	handler := execInContainer(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.ExecInContainerInput{
		StackID: "stack0000000001", ServiceName: "web", Command: "ls",
	})
	if err == nil {
		t.Fatal("expected the 403 to surface")
	}
	for _, call := range *calls {
		if strings.Contains(call, "/terminal/sessions") {
			t.Errorf("a 403 must not fall back to a pty session, saw %v", *calls)
		}
	}
}

func TestExecInContainerSurfacesAConflictThatIsNotACapabilityGap(t *testing.T) {
	// resolveStackAndWorker also answers 409 when the deployed project name
	// cannot be resolved. Falling back on the status alone would open a pty
	// session over that and hide the real error behind a worse exec.
	srv, calls, _ := execRouteServer(t,
		`{"error":"stack has no deployed project name yet"}`, http.StatusConflict)

	handler := execInContainer(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.ExecInContainerInput{
		StackID: "stack0000000001", ServiceName: "web", Command: "ls",
	})
	if err == nil {
		t.Fatal("expected the 409 to surface")
	}
	if !strings.Contains(err.Error(), "deployed project name") {
		t.Errorf("expected the server's own error, got %v", err)
	}
	for _, call := range *calls {
		if strings.Contains(call, "/terminal/sessions") {
			t.Errorf("only a missing-capability 409 may fall back to a pty, saw %v", *calls)
		}
	}
}

func TestExecInContainerFallbackExplainsItself(t *testing.T) {
	// newExecServer answers /exec with 409, standing in for an old worker.
	es := newExecServer("# ", "hello\n")
	defer es.close()

	handler := execInContainer(client.New(es.srv.URL))
	_, out, err := handler(ctxWithKey(), nil, models.ExecInContainerInput{
		StackID: "stack0000000001", ServiceName: "web", Command: "echo hello",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result := out.(map[string]any)
	notice, _ := result["_notice"].(string)
	// The fallback's limits are real and have to be stated, or the caller
	// reads an interleaved stream as stdout.
	for _, want := range []string{"does not support one-shot exec", "interleaved", "POSIX"} {
		if !strings.Contains(notice, want) {
			t.Errorf("notice %q should mention %q", notice, want)
		}
	}
	if result["output"] == nil {
		t.Errorf("the fallback still reports output: %#v", result)
	}
}

func TestExecArgvRendering(t *testing.T) {
	cases := []struct {
		name string
		in   models.ExecInContainerInput
		want []string
	}{
		{name: "command through the default shell", in: models.ExecInContainerInput{Command: "ls -la"}, want: []string{"/bin/sh", "-c", "ls -la"}},
		{name: "command through a chosen shell", in: models.ExecInContainerInput{Command: "ls", Shell: []string{"/bin/bash"}}, want: []string{"/bin/bash", "-c", "ls"}},
		{name: "argv verbatim", in: models.ExecInContainerInput{Argv: []string{"ps", "aux"}}, want: []string{"ps", "aux"}},
		{name: "argv ignores shell", in: models.ExecInContainerInput{Argv: []string{"ps"}, Shell: []string{"/bin/bash"}}, want: []string{"ps"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := execArgv(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("execArgv = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("execArgv = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestShellQuoteEscapesASingleQuote(t *testing.T) {
	// The fallback has to rebuild a shell line from an argv, so an argument
	// containing a quote must not break out of it.
	if got := shellQuote(`it's`); got != `'it'\''s'` {
		t.Fatalf("shellQuote = %s", got)
	}
}
