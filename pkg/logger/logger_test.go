package logger

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestParseTimestamp(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		wantTime  string
		wantMsg   string
		wantFound bool
	}{
		{
			name:      "Valid standard timestamp",
			line:      "2026/05/24 16:51:57 [WORKER] hello",
			wantTime:  "2026/05/24 16:51:57",
			wantMsg:   "[WORKER] hello",
			wantFound: true,
		},
		{
			name:      "Short string",
			line:      "2026/05/24",
			wantTime:  "",
			wantMsg:   "2026/05/24",
			wantFound: false,
		},
		{
			name:      "Invalid delimiters",
			line:      "2026-05-24 16:51:57 [WORKER] hello",
			wantTime:  "",
			wantMsg:   "2026-05-24 16:51:57 [WORKER] hello",
			wantFound: false,
		},
		{
			name:      "Non-digits",
			line:      "202a/05/24 16:51:57 [WORKER] hello",
			wantTime:  "",
			wantMsg:   "202a/05/24 16:51:57 [WORKER] hello",
			wantFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTime, gotMsg, gotFound := parseTimestamp(tt.line)
			if gotTime != tt.wantTime || gotMsg != tt.wantMsg || gotFound != tt.wantFound {
				t.Errorf("parseTimestamp() = (%q, %q, %v), want (%q, %q, %v)",
					gotTime, gotMsg, gotFound, tt.wantTime, tt.wantMsg, tt.wantFound)
			}
		})
	}
}

func TestFormatLineText(t *testing.T) {
	// Force text format and default log level for these tests
	SetFormat(FormatText)
	SetLevel(LevelDebug)

	tests := []struct {
		name     string
		line     string
		contains []string
	}{
		{
			name: "Colorizes worker category and shows INFO",
			line: "2026/05/24 16:51:57 [WORKER] connected",
			contains: []string{
				"2026-05-24 16:51:57",
				"\033[36m[INFO]\033[0m",
				"\033[1;36m[WORKER]\033[0m", // Bold Cyan
				"\033[32mconnected\033[0m",  // Green status
			},
		},
		{
			name: "Promotes executor deploy to deploy category and shows ERROR",
			line: "2026/05/24 16:51:57 [executor] deploy failed",
			contains: []string{
				"\033[31m[ERROR]\033[0m",
				"\033[1;38;5;208m[deploy]\033[0m", // Bold Orange
				"\033[31mfailed\033[0m",           // Red status
			},
		},
		{
			name: "Colorizes reconciler category and shows WARN",
			line: "2026/05/24 16:51:57 [reconciler] warning: port collision",
			contains: []string{
				"\033[33m[WARN]\033[0m",
				"\033[1;33m[reconciler]\033[0m", // Bold Yellow
				"\033[33mwarning:\033[0m",        // Yellow status
			},
		},
		{
			name: "Categorizes get_status as DEBUG",
			line: "2026/05/24 16:51:57 [executor] get_status: done",
			contains: []string{
				"\033[90m[DEBUG]\033[0m",
				"\033[1;35m[executor]\033[0m",
			},
		},
		{
			name: "Categorizes inspect as DEBUG",
			line: "2026/05/24 16:51:57 [executor] inspect: done",
			contains: []string{
				"\033[90m[DEBUG]\033[0m",
			},
		},
		{
			name: "Categorizes get_resources as DEBUG",
			line: "2026/05/24 16:51:57 [executor] get_resources: done",
			contains: []string{
				"\033[90m[DEBUG]\033[0m",
			},
		},
		{
			name: "Promotes executor run_job to job category and shows INFO",
			line: "2026/05/24 16:51:57 [executor] run_job dispatched",
			contains: []string{
				"\033[36m[INFO]\033[0m",
				"\033[1;38;5;99m[job]\033[0m",      // Bold Violet
				"dispatched",
			},
		},
		{
			name: "Categorizes CORS allowed origins as DEBUG",
			line: "2026/05/24 16:51:57 [CORS] Configured allowed origins: [http://localhost:8090]",
			contains: []string{
				"\033[90m[DEBUG]\033[0m",
			},
		},
		{
			name: "Categorizes successful GET request as DEBUG",
			line: "2026/05/24 16:51:57 [GIN] | 200 | GET /worker/ws",
			contains: []string{
				"\033[90m[DEBUG]\033[0m",
			},
		},
		{
			name: "Categorizes failed POST request as ERROR",
			line: "2026/05/24 16:51:57 [GIN] | 500 | POST /worker/register",
			contains: []string{
				"\033[31m[ERROR]\033[0m",
			},
		},
		{
			name: "Categorizes 404 GET request as ERROR",
			line: "2026/05/24 16:51:57 [GIN] | 404 | GET /invalid",
			contains: []string{
				"\033[31m[ERROR]\033[0m",
			},
		},
		{
			name: "Categorizes PocketBase DB query logs as DEBUG",
			line: "2026/05/24 16:51:57 [0.00ms] UPDATE workers SET cert_not_after='' WHERE id='123'",
			contains: []string{
				"\033[90m[DEBUG]\033[0m",
			},
		},
		{
			name: "Redacts output in job_runs SQL logs",
			line: "2026/05/24 16:51:57 [0.00ms] UPDATE `job_runs` SET `output`='sensitive ''api'' key', `status`='success'",
			contains: []string{
				"`output`='[REDACTED]'",
			},
		},
		{
			name: "Redacts output in sync_logs SQL logs",
			line: "2026/05/24 16:51:57 [0.00ms] UPDATE `sync_logs` SET `output`='some git log output', `status`='success'",
			contains: []string{
				"`output`='[REDACTED]'",
			},
		},
		{
			name: "Redacts output in job_runs SQL logs with double quotes",
			line: "2026/05/24 16:51:57 [0.00ms] UPDATE \"job_runs\" SET \"output\"='sensitive key', \"status\"='success'",
			contains: []string{
				"\"output\"='[REDACTED]'",
			},
		},
		{
			name: "Redacts output in sync_logs SQL logs with double quotes",
			line: "2026/05/24 16:51:57 [0.00ms] UPDATE \"sync_logs\" SET \"output\"='some git log output', \"status\"='success'",
			contains: []string{
				"\"output\"='[REDACTED]'",
			},
		},
		{
			name: "Redacts output in job_runs SQL when newlines are collapsed (multiline container output)",
			line: "2026/05/24 16:51:57 [0.00ms] UPDATE `job_runs` SET `output`='container1   nginx   29 hours ago   Up   0.0.0.0:8082->80/tcp   nginx  container2   redis   3 weeks ago   Up (healthy)   6379/tcp   redis-1', `status`='success' WHERE `id`='abc123'",
			contains: []string{
				"`output`='[REDACTED]'",
			},
		},
		{
			name: "Suppresses PocketBase slog HTTP access GET log as DEBUG",
			line: "2026/05/24 16:51:57 INFO GET /api/collections/stacks/records/re5j080yyq8urmf status=200 latency=1.2ms",
			contains: []string{
				"\033[90m[DEBUG]\033[0m",
			},
		},
		{
			name: "Suppresses PocketBase slog HTTP access GET /api/custom as DEBUG",
			line: "2026/05/24 16:51:57 INFO GET /api/custom/stacks/re5j080yyq8urmf/services status=200",
			contains: []string{
				"\033[90m[DEBUG]\033[0m",
			},
		},
		{
			name: "Suppresses PocketBase slog HTTP access container stats as DEBUG",
			line: "2026/05/24 16:51:57 INFO GET /api/custom/stacks/re5j080yyq8urmf/container/4203e180bd91/stats status=200",
			contains: []string{
				"\033[90m[DEBUG]\033[0m",
			},
		},
		{
			name: "Categorizes DB migrations start message as INFO",
			line: "2026/05/24 16:51:57 [db] Running database migrations...",
			contains: []string{
				"\033[36m[INFO]\033[0m",
				"\033[1;32m[db]\033[0m",
				"Running database migrations...",
			},
		},
		{
			name: "Categorizes DB migrations completed message as INFO",
			line: "2026/05/24 16:51:57 [db] Database migrations completed successfully.",
			contains: []string{
				"\033[36m[INFO]\033[0m",
				"\033[1;32m[db]\033[0m",
				"Database migrations completed successfully.",
			},
		},
		{
			name: "Categorizes applying migration details as DEBUG",
			line: "2026/05/24 16:51:57 Applying migration 12_remove_embedded_worker.go...",
			contains: []string{
				"\033[90m[DEBUG]\033[0m",
				"Applying migration",
			},
		},
		{
			name: "Categorizes pb_migrations details as DEBUG",
			line: "2026/05/24 16:51:57 pb_migrations/1_init_collections.go applied",
			contains: []string{
				"\033[90m[DEBUG]\033[0m",
				"pb_migrations/1_init_collections.go applied",
			},
		},
		{
			name: "Suppresses ANSI colorized db query logs during bootstrap",
			line: "\x1b[90m[4.00ms] CREATE TABLE IF NOT EXISTS `_migrations`\x1b[0m",
			contains: []string{
				"\033[90m[DEBUG]\033[0m",
				"CREATE TABLE IF NOT EXISTS `_migrations`",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			writer := &ColorWriter{out: &buf}
			_, _ = writer.Write([]byte(tt.line + "\n"))
			got := buf.String()
			for _, c := range tt.contains {
				if !strings.Contains(got, c) {
					t.Errorf("formatLineText() = %q, expected to contain %q", got, c)
				}
			}
		})
	}
}

func TestLogLevelFiltering(t *testing.T) {
	SetFormat(FormatText)

	tests := []struct {
		name          string
		configLevel   LogLevel
		logLine       string
		shouldProduce bool
	}{
		{
			name:          "DEBUG log is filtered out when config is INFO",
			configLevel:   LevelInfo,
			logLine:       "2026/05/24 16:51:57 [executor] get_status: start\n",
			shouldProduce: false,
		},
		{
			name:          "DEBUG log is printed when config is DEBUG",
			configLevel:   LevelDebug,
			logLine:       "2026/05/24 16:51:57 [executor] get_status: start\n",
			shouldProduce: true,
		},
		{
			name:          "INFO log is printed when config is INFO",
			configLevel:   LevelInfo,
			logLine:       "2026/05/24 16:51:57 [WORKER] online\n",
			shouldProduce: true,
		},
		{
			name:          "INFO log is filtered out when config is WARN",
			configLevel:   LevelWarn,
			logLine:       "2026/05/24 16:51:57 [WORKER] online\n",
			shouldProduce: false,
		},
		{
			name:          "ANSI colorized db query log is filtered out when config is INFO",
			configLevel:   LevelInfo,
			logLine:       "\x1b[90m[4.00ms] CREATE TABLE IF NOT EXISTS `_migrations`\x1b[0m\n",
			shouldProduce: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			SetLevel(tt.configLevel)
			var buf bytes.Buffer
			writer := &ColorWriter{out: &buf}
			_, err := writer.Write([]byte(tt.logLine))
			if err != nil {
				t.Fatalf("Write() error: %v", err)
			}
			produced := buf.Len() > 0
			if produced != tt.shouldProduce {
				t.Errorf("expected production = %v, got %v for log %q", tt.shouldProduce, produced, tt.logLine)
			}
		})
	}
}

func TestJSONFormatting(t *testing.T) {
	SetFormat(FormatJSON)
	SetLevel(LevelDebug)

	input := "2026/05/24 16:51:57 [WORKER] Registered new remote worker: node-01\n"
	var buf bytes.Buffer
	writer := &ColorWriter{out: &buf}

	_, err := writer.Write([]byte(input))
	if err != nil {
		t.Fatalf("Write() error: %v", err)
	}

	output := buf.String()
	if !strings.HasSuffix(output, "\n") {
		t.Errorf("JSON output should end with a newline")
	}

	var parsed JSONLog
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Fatalf("Failed to parse JSON log output: %v, raw: %q", err, output)
	}

	if parsed.Time != "2026-05-24 16:51:57" {
		t.Errorf("Time = %q, want %q", parsed.Time, "2026-05-24 16:51:57")
	}
	if parsed.Level != "INFO" {
		t.Errorf("Level = %q, want %q", parsed.Level, "INFO")
	}
	if parsed.Category != "WORKER" {
		t.Errorf("Category = %q, want %q", parsed.Category, "WORKER")
	}
	if parsed.Message != "Registered new remote worker: node-01" {
		t.Errorf("Message = %q, want %q", parsed.Message, "Registered new remote worker: node-01")
	}

	// Test a get_status debug log in JSON
	buf.Reset()
	inputDebug := "2026/05/24 16:51:57 [executor] get_status start\n"
	_, _ = writer.Write([]byte(inputDebug))
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("Failed to parse debug JSON: %v", err)
	}
	if parsed.Level != "DEBUG" {
		t.Errorf("Debug Level = %q, want %q", parsed.Level, "DEBUG")
	}
}

func TestColorWriterLineBuffering(t *testing.T) {
	var buf bytes.Buffer
	writer := &ColorWriter{out: &buf}
	SetFormat(FormatText)
	SetLevel(LevelInfo)

	// Write in chunks
	_, _ = writer.Write([]byte("2026/05/24 16:51:57 [db] Running database "))
	if buf.Len() > 0 {
		t.Errorf("expected buffer to be empty before newline, got %q", buf.String())
	}

	_, _ = writer.Write([]byte("migrations...\n"))
	got := buf.String()
	if !strings.Contains(got, "Running database migrations...") {
		t.Errorf("expected logs to contain message, got %q", got)
	}
}

func TestColorWriterSuppressEmptyWrites(t *testing.T) {
	var buf bytes.Buffer
	writer := &ColorWriter{out: &buf}

	// Writes that are purely ANSI escape codes or whitespace should be suppressed
	inputs := [][]byte{
		[]byte("\x1b[90m"),
		[]byte("\x1b[0m"),
		[]byte("\n"),
		[]byte("   \n"),
	}

	for _, input := range inputs {
		n, err := writer.Write(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if n != len(input) {
			t.Errorf("expected to write %d bytes, got %d", len(input), n)
		}
	}

	if buf.Len() > 0 {
		t.Errorf("expected buffer to be empty, but got %q", buf.String())
	}
}
