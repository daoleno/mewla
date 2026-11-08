//go:build linux

package stats

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func grokUpdatesLines(day time.Time, from, count int) string {
	var b strings.Builder
	for i := from; i < from+count; i++ {
		fmt.Fprintf(&b, `{"timestamp":%d,"params":{"_meta":{"totalTokens":%d,"modelId":"grok-4.5"},"pad":"%s"}}`+"\n",
			day.Unix()+int64(i), 100*(i+1), strings.Repeat("x", 600))
	}
	return b.String()
}

func writeGrokSession(t *testing.T, home, name, updates string) string {
	t.Helper()
	dir := filepath.Join(home, ".grok", "sessions", "%2Ftmp%2Fmewla", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	summary := `{"info":{"id":"` + name + `","cwd":"/tmp/mewla"},"current_model_id":"grok-4.5"}`
	if err := os.WriteFile(filepath.Join(dir, "summary.json"), []byte(summary), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "updates.jsonl"), []byte(updates), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// Every refresh walked every Grok session and read all of its updates. A
// session that did not change must not be read again, and a start of the
// daemon must not read them all again either.
func TestGrokStatsReadOnlyChangedSessions(t *testing.T) {
	setTestLocalLocation(t, time.UTC)
	home := t.TempDir()
	day := time.Date(2026, time.April, 4, 9, 0, 0, 0, time.UTC)
	bigUpdates := grokUpdatesLines(day, 0, 4000) // about 2.8 MB
	big := writeGrokSession(t, home, "big", bigUpdates)
	small := writeGrokSession(t, home, "small", grokUpdatesLines(day, 0, 2))

	c := NewCollector()
	c.usageCacheDir = t.TempDir()
	first := c.collectGrokStats(home)
	before := selfRchar(t)
	second := c.collectGrokStats(home)
	if read := selfRchar(t) - before; read > uint64(len(bigUpdates))/20 {
		t.Fatalf("unchanged Grok sessions were read again: %d bytes against %d", read, len(bigUpdates))
	}
	if first["2026-04-04"].models["grok-4.5"] != second["2026-04-04"].models["grok-4.5"] {
		t.Fatalf("cached totals differ: %+v vs %+v", first["2026-04-04"].models, second["2026-04-04"].models)
	}

	// A new collector, as after a restart, starts from what was stored.
	restarted := NewCollector()
	restarted.usageCacheDir = c.usageCacheDir
	before = selfRchar(t)
	third := restarted.collectGrokStats(home)
	if read := selfRchar(t) - before; read > uint64(len(bigUpdates))/20 {
		t.Fatalf("a restart read the Grok sessions again: %d bytes against %d", read, len(bigUpdates))
	}
	if first["2026-04-04"].models["grok-4.5"] != third["2026-04-04"].models["grok-4.5"] {
		t.Fatalf("stored totals differ: %+v vs %+v", first["2026-04-04"].models, third["2026-04-04"].models)
	}

	// A session that grows is read again; one that is gone stops counting.
	f, err := os.OpenFile(filepath.Join(small, "updates.jsonl"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(grokUpdatesLines(day, 2, 1))
	_ = f.Close()
	grown := restarted.collectGrokStats(home)["2026-04-04"].models["grok-4.5"].totalTokens
	if want := first["2026-04-04"].models["grok-4.5"].totalTokens + 100; grown != want {
		t.Fatalf("grown session total = %d, want %d", grown, want)
	}
	if err := os.RemoveAll(big); err != nil {
		t.Fatal(err)
	}
	if got := restarted.collectGrokStats(home)["2026-04-04"].models["grok-4.5"].totalTokens; got != 300 {
		t.Fatalf("removed session still counted: total %d, want 300", got)
	}
}

// Another price table means other cost estimates in what was stored.
func TestGrokStatsStoreKeyFollowsThePrices(t *testing.T) {
	a := map[string]map[string]modelPricing{"xai": {"grok-4.5": {input: 2}}}
	b := map[string]map[string]modelPricing{"xai": {"grok-4.5": {input: 3}}}
	if grokUsageStoreKey(a) == grokUsageStoreKey(b) {
		t.Fatal("other rates share a key")
	}
	if grokUsageStoreKey(a) != grokUsageStoreKey(a) {
		t.Fatal("the same rates differ in key")
	}
	if grokUsageStoreKey(a) == claudeUsageStoreKey(a) {
		t.Fatal("Grok and Claude share a key")
	}
}
