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
		return strconv.FormatInt(resolved.Unix(), 10), resolved, nil
	}
	if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return strconv.FormatInt(parsed.Unix(), 10), parsed, nil
	}
	if seconds, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return strconv.FormatInt(seconds, 10), time.Unix(seconds, 0), nil
	}
	if seconds, err := strconv.ParseFloat(raw, 64); err == nil {
		whole := int64(seconds)
		nanos := int64((seconds - float64(whole)) * float64(time.Second))
		parsed := time.Unix(whole, nanos)
		return strconv.FormatInt(parsed.Unix(), 10), parsed, nil
	}
	return "", time.Time{}, fmt.Errorf("must be RFC3339 with timezone, a Unix timestamp, or a positive relative duration such as 30m")
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
	return logs[:maxLogResponseBytes], true
}
