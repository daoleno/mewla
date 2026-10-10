package workerproc

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestParseProcStatStartTicks pins the /proc/<pid>/stat field-22 extraction,
// including comm fields containing spaces and parentheses.
func TestParseProcStatStartTicks(t *testing.T) {
	cases := []struct {
		name string
		stat string
		want int64
		ok   bool
	}{
		{
			name: "plain comm",
			stat: "123 (pi) S 1 123 123 0 -1 4194560 100 0 0 0 50 0 0 0 20 0 1 0 424242 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0",
			want: 424242,
			ok:   true,
		},
		{
			name: "spaced and parenthesized comm",
			stat: "9 (pi (node) child) S 1 9 9 0 -1 4194560 0 0 0 0 0 0 0 0 20 0 1 0 777 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0",
			want: 777,
			ok:   true,
		},
		{
			name: "missing starttime field",
			stat: "123 (pi) S 1 123 123 0 -1 0",
			ok:   false,
		},
		{
			name: "no closing paren",
			stat: "123 pi S 1 123",
			ok:   false,
		},
		{
			name: "non-numeric starttime",
			stat: "123 (pi) S 1 123 123 0 -1 4194560 0 0 0 0 50 0 0 0 20 0 1 0 x 0",
			ok:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseProcStatStartTicks([]byte(tc.stat))
			if ok != tc.ok || (tc.ok && got != tc.want) {
				t.Fatalf("parseProcStatStartTicks(%q) = %d, %v; want %d, %v", tc.stat, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// TestSysconfClockTicksRetriesAfterFailure proves a getconf CLK_TCK failure
// fails closed for the current poll but is retried after a short interval
// instead of disabling precise process-start evidence for the rest of the
// daemon lifetime. A fake getconf shadows the real binary: phase 1 fails,
// phase 2 succeeds after the retry interval, and phase 3 proves the success
// is cached (getconf is then entirely absent from PATH, so any re-exec would
// return an error).
func TestSysconfClockTicksRetriesAfterFailure(t *testing.T) {
	// Reset the shared cache so this test is order-independent, and leave it
	// reset so a later test re-derives the real tick rate from the real
	// getconf (PATH is restored by t.Setenv at test end).
	ticksState.mu.Lock()
	ticksState.value = 0
	ticksState.retryAfter = time.Time{}
	ticksState.mu.Unlock()
	defer func() {
		ticksState.mu.Lock()
		ticksState.value = 0
		ticksState.retryAfter = time.Time{}
		ticksState.mu.Unlock()
	}()

	fakeDir := t.TempDir()
	fake := filepath.Join(fakeDir, "getconf")
	writeFakeGetconf := func(body string) {
		t.Helper()
		if err := os.WriteFile(fake, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", fakeDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	// Phase 1: getconf unavailable; the poll fails closed with no cached
	// value and no crash.
	writeFakeGetconf("exit 1")
	if got := sysconfClockTicks(); got != 0 {
		t.Fatalf("failed getconf must fail closed with 0, got %d", got)
	}

	// Phase 2: after the retry interval, a working getconf restores precise
	// evidence and caches it.
	ticksState.mu.Lock()
	ticksState.retryAfter = time.Time{}
	ticksState.mu.Unlock()
	writeFakeGetconf("echo 100")
	if got := sysconfClockTicks(); got != 100 {
		t.Fatalf("retried getconf must cache 100, got %d", got)
	}

	// Phase 3: the success is cached. With getconf entirely absent from
	// PATH, only the cached value can answer.
	t.Setenv("PATH", fakeDir)
	if got := sysconfClockTicks(); got != 100 {
		t.Fatalf("cached CLK_TCK must survive without getconf, got %d", got)
	}
}
