package routes

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// normalizeLogTime accepts the timestamp forms documented by the API and
// returns a Docker-compatible Unix timestamp. Relative durations are resolved
// against the supplied reference so since and until in one request have a
// single, unambiguous meaning.
func normalizeLogTime(raw string, reference time.Time) (string, time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", time.Time{}, nil
	}
	if duration, err := time.ParseDuration(raw); err == nil {
		if duration <= 0 {
			return "", time.Time{}, fmt.Errorf("relative log time must be a positive duration")
		}
		resolved := reference.Add(-duration)
		return formatDockerLogTime(resolved), resolved, nil
	}
	if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return formatDockerLogTime(parsed), parsed, nil
	}
	if seconds, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return strconv.FormatInt(seconds, 10), time.Unix(seconds, 0), nil
	}
	if parsed, ok := parseUnixTimestamp(raw); ok {
		return raw, parsed, nil
	}
	return "", time.Time{}, fmt.Errorf("must be RFC3339 with timezone, a Unix timestamp, or a positive relative duration such as 30m")
}

func formatDockerLogTime(value time.Time) string {
	if value.Nanosecond() == 0 {
		return strconv.FormatInt(value.Unix(), 10)
	}
	return strconv.FormatInt(value.Unix(), 10) + "." + strings.TrimRight(fmt.Sprintf("%09d", value.Nanosecond()), "0")
}

func parseUnixTimestamp(raw string) (time.Time, bool) {
	negative := strings.HasPrefix(raw, "-")
	digits := strings.TrimPrefix(raw, "-")
	parts := strings.Split(digits, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || len(parts[1]) > 9 {
		return time.Time{}, false
	}
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	fraction, err := strconv.ParseInt(parts[1]+strings.Repeat("0", 9-len(parts[1])), 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	if negative {
		whole = -whole
		fraction = -fraction
	}
	return time.Unix(whole, fraction), true
}

func normalizeContainerLogFilters(since, until, pattern string, reference time.Time) (string, string, error) {
	if pattern != "" {
		if _, err := regexp.Compile(pattern); err != nil {
			return "", "", fmt.Errorf("invalid pattern: %w", err)
		}
	}
	normalizedSince, sinceTime, err := normalizeLogTime(since, reference)
	if err != nil {
		return "", "", fmt.Errorf("invalid since: %w", err)
	}
	normalizedUntil, untilTime, err := normalizeLogTime(until, reference)
	if err != nil {
		return "", "", fmt.Errorf("invalid until: %w", err)
	}
	if !sinceTime.IsZero() && !untilTime.IsZero() && !sinceTime.Before(untilTime) {
		return "", "", fmt.Errorf("since must be before until")
	}
	return normalizedSince, normalizedUntil, nil
}

func filterLogOutput(logs, pattern string) string {
	if pattern == "" {
		return logs
	}
	re := regexp.MustCompile(pattern) // validated before dispatch
	lines := strings.Split(logs, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if re.MatchString(line) {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

func capLogOutput(logs string) (string, bool) {
	const maxLogResponseBytes = 64 << 10
	if len(logs) <= maxLogResponseBytes {
		return logs, false
	}
	return tailLogOutput(logs, maxLogResponseBytes), true
}

func tailLogOutput(logs string, maxBytes int) string {
	if len(logs) <= maxBytes {
		return logs
	}
	tail := logs[len(logs)-maxBytes:]
	for len(tail) > 0 && tail[0]&0xc0 == 0x80 {
		tail = tail[1:]
	}
	return strings.ToValidUTF8(tail, "")
}
