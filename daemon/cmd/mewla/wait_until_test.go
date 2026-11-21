package main

import (
	"testing"
	"time"
)

func TestParseWaitUntilIsAlwaysAFutureTime(t *testing.T) {
	zone := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 11, 21, 20, 30, 0, 0, zone)
	for value, want := range map[string]time.Time{
		"90m":                       now.Add(90 * time.Minute),
		"36h":                       now.Add(36 * time.Hour),
		"3d":                        now.Add(72 * time.Hour),
		"21:00":                     time.Date(2026, 11, 21, 21, 0, 0, 0, zone),
		"09:30":                     time.Date(2026, 11, 22, 9, 30, 0, 0, zone), // the next one
		"2026-11-25":                time.Date(2026, 11, 25, 0, 0, 0, 0, zone),
		"2026-11-22 09:30":          time.Date(2026, 11, 22, 9, 30, 0, 0, zone),
		"2026-11-22T09:30":          time.Date(2026, 11, 22, 9, 30, 0, 0, zone),
		"2026-11-22T01:30:00Z":      time.Date(2026, 11, 22, 1, 30, 0, 0, time.UTC),
		"2026-11-22T09:30:00+08:00": time.Date(2026, 11, 22, 9, 30, 0, 0, zone),
	} {
		got, err := parseWaitUntil(value, now)
		if err != nil || !got.Equal(want) {
			t.Errorf("parseWaitUntil(%q) = %v, %v; want %v", value, got, err, want)
		}
	}
	for _, value := range []string{"", "-2h", "2026-11-20", "2026-11-21 20:00", "tomorrow", "0d"} {
		if got, err := parseWaitUntil(value, now); err == nil {
			t.Errorf("parseWaitUntil(%q) = %v, want an error", value, got)
		}
	}
}
