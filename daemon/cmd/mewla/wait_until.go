package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// parseWaitUntil reads when a wait ends, always in the future: a duration from
// now (90m, 36h, 3d), a local clock time (15:04, the next one), a local date
// (2006-01-02, its start), a local date and time (2006-01-02 15:04 or
// 2006-01-02T15:04), or RFC3339.
func parseWaitUntil(value string, now time.Time) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, fmt.Errorf("a time is required")
	}
	var at time.Time
	if days, ok := strings.CutSuffix(value, "d"); ok {
		if n, err := strconv.Atoi(days); err == nil {
			at = now.Add(time.Duration(n) * 24 * time.Hour)
		}
	}
	if at.IsZero() {
		if d, err := time.ParseDuration(value); err == nil {
			at = now.Add(d)
		}
	}
	if at.IsZero() {
		if clock, err := time.ParseInLocation("15:04", value, now.Location()); err == nil {
			at = time.Date(now.Year(), now.Month(), now.Day(), clock.Hour(), clock.Minute(), 0, 0, now.Location())
			if !at.After(now) {
				at = at.AddDate(0, 0, 1)
			}
		}
	}
	if at.IsZero() {
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			at = parsed
		}
	}
	for _, layout := range []string{"2006-01-02 15:04", "2006-01-02T15:04", "2006-01-02"} {
		if !at.IsZero() {
			break
		}
		if parsed, err := time.ParseInLocation(layout, value, now.Location()); err == nil {
			at = parsed
		}
	}
	if at.IsZero() {
		return time.Time{}, fmt.Errorf("%q is not a duration (90m, 36h, 3d) or a local time (15:04, 2006-01-02, 2006-01-02 15:04, RFC3339)", value)
	}
	if !at.After(now) {
		return time.Time{}, fmt.Errorf("%s is not in the future", at.Format("2006-01-02 15:04 MST"))
	}
	return at, nil
}
