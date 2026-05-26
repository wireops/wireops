package logger

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"
)

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
var sqlOutputRegex = regexp.MustCompile("(?i)(?:\"output\"|`output`|output)\\s*=\\s*'")

func stripAnsi(str string) string {
	return ansiRegex.ReplaceAllString(str, "")
}

type LogLevel int

const (
	LevelDebug LogLevel = iota
	LevelInfo
	LevelWarn
	LevelError
)

type LogFormat int

const (
	FormatText LogFormat = iota
	FormatJSON
)

var (
	currentLogLevel  LogLevel  = LevelInfo
	currentLogFormat LogFormat = FormatText
)

// JSONLog represents the structured JSON output format compatible with Loki.
type JSONLog struct {
	Time     string `json:"time"`
	Level    string `json:"level"`
	Category string `json:"category,omitempty"`
	Message  string `json:"message"`
}

// SafeLogf sanitizes the formatted string to prevent log forging vulnerabilities.
func SafeLogf(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	msg = strings.ReplaceAll(msg, "\n", " ")
	msg = strings.ReplaceAll(msg, "\r", " ")
	log.Print(msg)
}

// ColorWriter intercepts log writes, classifies, filters, and formats them.
type ColorWriter struct {
	out io.Writer
	mu  sync.Mutex
	buf []byte
}

func (w *ColorWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.buf = append(w.buf, p...)

	for {
		idx := bytes.IndexByte(w.buf, '\n')
		if idx == -1 {
			break
		}

		lineBytes := w.buf[:idx+1]
		w.buf = w.buf[idx+1:]

		if err := w.writeLine(lineBytes); err != nil {
			return len(p), err
		}
	}

	return len(p), nil
}

func (w *ColorWriter) writeLine(p []byte) error {
	line := string(p)
	line = stripAnsi(line)
	if strings.TrimSpace(line) == "" {
		return nil
	}
	hasNewline := strings.HasSuffix(line, "\n")
	line = strings.TrimSuffix(line, "\n")
	line = strings.TrimSuffix(line, "\r")

	timestamp, msg, found := parseTimestamp(line)
	if found {
		timestamp = strings.ReplaceAll(timestamp, "/", "-")
	} else {
		timestamp = time.Now().Format("2006-01-02 15:04:05")
	}

	// Sanitize DB query logs to prevent sensitive output leaks
	msg = sanitizeDbQuery(msg)

	// Clean ANSI codes for classification
	cleanMsgForClassify := stripAnsi(msg)

	// Classify the level based on the message
	level := LevelInfo
	lowerMsg := strings.ToLower(cleanMsgForClassify)

	isHTTPMethod := false
	for _, method := range []string{"GET", "POST", "PATCH", "DELETE", "PUT"} {
		if strings.Contains(cleanMsgForClassify, method+" ") || strings.Contains(cleanMsgForClassify, " "+method) || strings.Contains(cleanMsgForClassify, "|"+method) {
			isHTTPMethod = true
			break
		}
	}

	// PocketBase slog emits HTTP access lines as "INFO GET /path ..." or "INFO POST /path ..."
	// without a category bracket. Detect them by the leading level word followed by a method.
	// Error checks take priority: if the line carries a 4xx/5xx status or error keywords it is
	// NOT demoted to Debug regardless of the PocketBase HTTP prefix.
	isPocketBaseHTTPLog := false
	for _, method := range []string{"GET", "POST", "PATCH", "DELETE", "PUT"} {
		if strings.HasPrefix(cleanMsgForClassify, "INFO "+method+" ") ||
			strings.HasPrefix(cleanMsgForClassify, "DEBUG "+method+" ") {
			isPocketBaseHTTPLog = true
			break
		}
	}
	if isPocketBaseHTTPLog && (hasApiErrorStatus(cleanMsgForClassify) ||
		strings.Contains(lowerMsg, "error") ||
		strings.Contains(lowerMsg, "failed") ||
		strings.Contains(lowerMsg, "panic")) {
		isPocketBaseHTTPLog = false
	}

	isDbQuery := false
	if strings.HasPrefix(cleanMsgForClassify, "[") {
		if msIdx := strings.Index(cleanMsgForClassify, "ms]"); msIdx > 0 && msIdx < 15 {
			isDbQuery = true
		}
	}

	isMigrationLog := strings.Contains(lowerMsg, "applying migration") ||
		strings.Contains(lowerMsg, "applied migration") ||
		strings.Contains(lowerMsg, "pb_migrations") ||
		strings.Contains(lowerMsg, "migration(s)")

	if strings.Contains(lowerMsg, "get_status") || strings.Contains(lowerMsg, "inspect") || strings.Contains(lowerMsg, "get_resources") || strings.Contains(lowerMsg, "cors") || isDbQuery || isMigrationLog || isPocketBaseHTTPLog {
		level = LevelDebug
	} else if isHTTPMethod {
		if hasApiErrorStatus(msg) || strings.Contains(lowerMsg, "error") || strings.Contains(lowerMsg, "failed") || strings.Contains(lowerMsg, "failure") || strings.Contains(lowerMsg, "fatal") || strings.Contains(lowerMsg, "panic") {
			level = LevelError
		} else {
			level = LevelDebug
		}
	} else if strings.Contains(lowerMsg, "warning") || strings.Contains(lowerMsg, "warn") {
		level = LevelWarn
	} else if strings.Contains(lowerMsg, "error") || strings.Contains(lowerMsg, "failed") || strings.Contains(lowerMsg, "failure") || strings.Contains(lowerMsg, "fatal") || strings.Contains(lowerMsg, "panic") {
		level = LevelError
	}

	// Filter based on active log level
	if level < currentLogLevel {
		return nil
	}

	// Parse category tag if present, e.g. "[WORKER]" or "[executor]"
	openIdx := strings.Index(msg, "[")
	closeIdx := strings.Index(msg, "]")
	category := ""
	cleanMsg := msg

	if openIdx >= 0 && closeIdx > openIdx && openIdx < 5 {
		category = msg[openIdx+1 : closeIdx]
		cleanMsg = msg[closeIdx+1:]
		// Trim leading spaces from cleanMsg
		for len(cleanMsg) > 0 && cleanMsg[0] == ' ' {
			cleanMsg = cleanMsg[1:]
		}
	}

	// Dynamic category tag rewriting/promotion
	if category == "executor" || category == "WORKER" || category == "worker" || category == "reconciler" || category == "jobscheduler" {
		lowerClean := strings.ToLower(cleanMsg)
		if strings.HasPrefix(lowerClean, "deploy ") || strings.HasPrefix(lowerClean, "redeploy ") {
			category = "deploy"
			if strings.HasPrefix(lowerClean, "deploy ") {
				cleanMsg = cleanMsg[7:]
			} else {
				cleanMsg = cleanMsg[9:]
			}
			cleanMsg = strings.TrimSpace(cleanMsg)
		} else if strings.HasPrefix(lowerClean, "run_job ") || strings.HasPrefix(lowerClean, "kill_job ") || strings.HasPrefix(lowerClean, "job_completed ") {
			category = "job"
			if strings.HasPrefix(lowerClean, "run_job ") {
				cleanMsg = cleanMsg[8:]
			} else if strings.HasPrefix(lowerClean, "kill_job ") {
				cleanMsg = cleanMsg[9:]
			} else {
				cleanMsg = cleanMsg[14:]
			}
			cleanMsg = strings.TrimSpace(cleanMsg)
		} else if strings.HasPrefix(lowerClean, "job ") {
			category = "job"
			cleanMsg = cleanMsg[4:]
			cleanMsg = strings.TrimSpace(cleanMsg)
		}
	}

	if currentLogFormat == FormatJSON {
		levelStr := "INFO"
		switch level {
		case LevelDebug:
			levelStr = "DEBUG"
		case LevelInfo:
			levelStr = "INFO"
		case LevelWarn:
			levelStr = "WARN"
		case LevelError:
			levelStr = "ERROR"
		}

		logObj := JSONLog{
			Time:     timestamp,
			Level:    levelStr,
			Category: strings.ToUpper(category),
			Message:  cleanMsg,
		}

		jsonBytes, marshalErr := json.Marshal(logObj)
		if marshalErr != nil {
			_, err := w.out.Write(p)
			return err
		}
		outputLine := string(jsonBytes)
		if hasNewline {
			outputLine += "\n"
		}
		_, writeErr := io.WriteString(w.out, outputLine)
		return writeErr
	}

	// Default: Colorized Text Format
	var levelPrefix string
	switch level {
	case LevelDebug:
		levelPrefix = "\033[90m[DEBUG]\033[0m"
	case LevelInfo:
		levelPrefix = "\033[36m[INFO]\033[0m"
	case LevelWarn:
		levelPrefix = "\033[33m[WARN]\033[0m"
	case LevelError:
		levelPrefix = "\033[31m[ERROR]\033[0m"
	}

	var finalMsg string
	if category != "" {
		var colorCode string
		switch strings.ToLower(category) {
		case "worker":
			colorCode = "\033[1;36m" // Cyan Bold
		case "executor", "runner":
			colorCode = "\033[1;35m" // Magenta Bold
		case "deploy":
			colorCode = "\033[1;38;5;208m" // Orange Bold
		case "job":
			colorCode = "\033[1;38;5;99m"  // Violet/Purple Bold
		case "jobscheduler", "scheduler":
			colorCode = "\033[1;32m" // Green Bold
		case "db":
			colorCode = "\033[1;32m" // Green Bold (Green Bold)
		case "routes":
			colorCode = "\033[1;34m" // Blue Bold
		case "reconciler":
			colorCode = "\033[1;33m" // Yellow Bold
		case "cron":
			colorCode = "\033[90m"   // Gray
		case "cors":
			colorCode = "\033[0;33m" // Yellow Dim
		case "smtp", "oidc":
			colorCode = "\033[1;38;5;206m" // Pink/Magenta Bold
		case "gin":
			colorCode = "\033[0;34m" // Blue Dim
		default:
			colorCode = "\033[1;37m" // White Bold
		}
		coloredTag := colorCode + "[" + category + "]" + "\033[0m"
		finalMsg = coloredTag + " " + highlightKeywords(cleanMsg)
	} else {
		finalMsg = highlightKeywords(cleanMsg)
	}

	coloredTimestamp := "\033[90m" + timestamp + "\033[0m"
	result := coloredTimestamp + " " + levelPrefix + " " + finalMsg
	if hasNewline {
		result += "\n"
	}
	_, writeErr := io.WriteString(w.out, result)
	return writeErr
}

// SetLevel dynamically changes the active minimum log level (useful for tests).
func SetLevel(lvl LogLevel) {
	currentLogLevel = lvl
}

// SetFormat dynamically changes the active log format (useful for tests).
func SetFormat(fmt LogFormat) {
	currentLogFormat = fmt
}

// InitLogger redirects the default standard logger to write to the custom ColorWriter.
func InitLogger() {
	// Level
	lvlStr := strings.ToUpper(os.Getenv("LOG_LEVEL"))
	if lvlStr == "" {
		lvlStr = strings.ToUpper(os.Getenv("WIREOPS_LOG_LEVEL"))
	}
	switch lvlStr {
	case "DEBUG":
		currentLogLevel = LevelDebug
	case "INFO":
		currentLogLevel = LevelInfo
	case "WARN":
		currentLogLevel = LevelWarn
	case "ERROR":
		currentLogLevel = LevelError
	default:
		currentLogLevel = LevelInfo
	}

	// Format
	fmtStr := strings.ToLower(os.Getenv("LOG_FORMAT"))
	if fmtStr == "" {
		fmtStr = strings.ToLower(os.Getenv("WIREOPS_LOG_FORMAT"))
	}
	if fmtStr == "json" {
		currentLogFormat = FormatJSON
	} else {
		currentLogFormat = FormatText
	}

	log.SetOutput(&ColorWriter{out: os.Stderr})
	color.Output = &ColorWriter{out: os.Stdout}
	color.Error = &ColorWriter{out: os.Stderr}
}

// IsDebug reports whether the current log level is DEBUG.
func IsDebug() bool {
	return currentLogLevel == LevelDebug
}

// parseTimestamp checks if a line starts with standard Go log timestamp "YYYY/MM/DD HH:MM:SS"
func parseTimestamp(line string) (string, string, bool) {
	if len(line) < 20 {
		return "", line, false
	}
	// Check digits and delimiters: YYYY/MM/DD HH:MM:SS
	for i := 0; i < 4; i++ {
		if line[i] < '0' || line[i] > '9' {
			return "", line, false
		}
	}
	if line[4] != '/' {
		return "", line, false
	}
	for i := 5; i < 7; i++ {
		if line[i] < '0' || line[i] > '9' {
			return "", line, false
		}
	}
	if line[7] != '/' {
		return "", line, false
	}
	for i := 8; i < 10; i++ {
		if line[i] < '0' || line[i] > '9' {
			return "", line, false
		}
	}
	if line[10] != ' ' {
		return "", line, false
	}
	for i := 11; i < 13; i++ {
		if line[i] < '0' || line[i] > '9' {
			return "", line, false
		}
	}
	if line[13] != ':' {
		return "", line, false
	}
	for i := 14; i < 16; i++ {
		if line[i] < '0' || line[i] > '9' {
			return "", line, false
		}
	}
	if line[16] != ':' {
		return "", line, false
	}
	for i := 17; i < 19; i++ {
		if line[i] < '0' || line[i] > '9' {
			return "", line, false
		}
	}
	// Index 19 must be a space separator; also ensure there is a message after it.
	if len(line) < 21 || line[19] != ' ' {
		return "", line, false
	}
	return line[:19], line[20:], true
}

// highlightKeywords colors common status indicators in log messages.
func highlightKeywords(msg string) string {
	words := strings.Split(msg, " ")
	for i, w := range words {
		cleaned := strings.ToLower(w)
		cleaned = strings.Trim(cleaned, ":,!.()[]\"'")

		switch cleaned {
		case "error", "failed", "failure", "fatal", "revoked", "erroring", "panic":
			words[i] = "\033[31m" + w + "\033[0m" // Red
		case "warning", "warn", "offline", "disconnected", "stalled":
			words[i] = "\033[33m" + w + "\033[0m" // Yellow
		case "success", "successful", "done", "started", "registered", "connected", "online":
			words[i] = "\033[32m" + w + "\033[0m" // Green
		}
	}
	return strings.Join(words, " ")
}

// hasApiErrorStatus checks if the message contains a status code between 400 and 599.
func hasApiErrorStatus(msg string) bool {
	for i := 0; i < len(msg)-2; i++ {
		if msg[i] >= '0' && msg[i] <= '9' &&
			msg[i+1] >= '0' && msg[i+1] <= '9' &&
			msg[i+2] >= '0' && msg[i+2] <= '9' {
			leftOk := i == 0 || msg[i-1] == ' ' || msg[i-1] == '|' || msg[i-1] == '\t' || msg[i-1] == '\x1b' || msg[i-1] == '[' || msg[i-1] == '='
			rightOk := i+3 == len(msg) || msg[i+3] == ' ' || msg[i+3] == '|' || msg[i+3] == '\t' || msg[i+3] == '\x1b' || msg[i+3] == ']'
			if leftOk && rightOk {
				num := int(msg[i]-'0')*100 + int(msg[i+1]-'0')*10 + int(msg[i+2]-'0')
				if num >= 400 && num <= 599 {
					return true
				}
			}
		}
	}
	return false
}

// sanitizeDbQuery redacts the output field value from SQLite query logs for job_runs and sync_logs.
func sanitizeDbQuery(msg string) string {
	lowerMsg := strings.ToLower(msg)
	if strings.Contains(lowerMsg, "job_runs") || strings.Contains(lowerMsg, "sync_logs") {
		loc := sqlOutputRegex.FindStringIndex(msg)
		if loc != nil {
			startVal := loc[1]
			endVal := -1
			for i := startVal; i < len(msg); i++ {
				if msg[i] == '\'' {
					if i+1 < len(msg) && msg[i+1] == '\'' {
						i++ // Skip escaped single quote
						continue
					}
					endVal = i
					break
				}
			}
			if endVal != -1 {
				return msg[:startVal] + "[REDACTED]" + msg[endVal:]
			}
		}
	}
	return msg
}
