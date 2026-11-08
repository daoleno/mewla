//go:build linux

package stats

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func selfRchar(t *testing.T) uint64 {
	t.Helper()
	raw, err := os.ReadFile("/proc/self/io")
	if err != nil {
		t.Skipf("no /proc/self/io: %v", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if value, ok := strings.CutPrefix(line, "rchar: "); ok {
			n, _ := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
			return n
		}
	}
	t.Fatal("no rchar")
	return 0
}

func claudeSessionLines(from, count int) string {
	var b strings.Builder
	for i := from; i < from+count; i++ {
		fmt.Fprintf(&b, `{"type":"assistant","timestamp":"2026-04-04T10:%02d:00.000Z","cwd":"/tmp/mewla","message":{"id":"m%d","model":"claude-sonnet-4-6","content":[{"type":"text","text":"%s"}],"usage":{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}`+"\n", i%60, i, strings.Repeat("x", 600))
	}
	return b.String()
}

// The stats refresh runs every few minutes over every Claude session file
// ever written. A file that did not change must not be read again.
func TestClaudeStatsRereadOnlyChangedFiles(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude", "projects", "-tmp-mewla")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	big := filepath.Join(dir, "big.jsonl")
	small := filepath.Join(dir, "small.jsonl")
	if err := os.WriteFile(big, []byte(claudeSessionLines(0, 4000)), 0o644); err != nil { // about 2.6 MB
		t.Fatal(err)
	}
	if err := os.WriteFile(small, []byte(claudeSessionLines(10000, 2)), 0o644); err != nil {
		t.Fatal(err)
	}
	size, _ := os.Stat(big)

	c := NewCollector()
	first := c.collectClaudeSessionStats(home)
	before := selfRchar(t)
	second := c.collectClaudeSessionStats(home)
	read := selfRchar(t) - before
	if read > uint64(size.Size())/20 {
		t.Fatalf("an unchanged session file was read again: %d bytes read against a %d byte file", read, size.Size())
	}
	day1, day2 := first["2026-04-04"], second["2026-04-04"]
	if day1 == nil || day2 == nil || day1.models["claude-sonnet-4-6"] != day2.models["claude-sonnet-4-6"] || day2.models["claude-sonnet-4-6"].totalTokens != 4002*15 {
		t.Fatalf("cached totals differ: %+v vs %+v", day1.models, day2.models)
	}
	if day2.projects["mewla"].sessions != day1.projects["mewla"].sessions {
		t.Fatalf("project sessions %d then %d", day1.projects["mewla"].sessions, day2.projects["mewla"].sessions)
	}

	// A file that grows is read again; a file that is gone stops counting.
	f, err := os.OpenFile(small, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(claudeSessionLines(20000, 1))
	_ = f.Close()
	third := c.collectClaudeSessionStats(home)["2026-04-04"]
	if got := third.models["claude-sonnet-4-6"].totalTokens; got != 4003*15 {
		t.Fatalf("grown file not picked up: total %d", got)
	}
	if err := os.Remove(big); err != nil {
		t.Fatal(err)
	}
	fourth := c.collectClaudeSessionStats(home)["2026-04-04"]
	if got := fourth.models["claude-sonnet-4-6"].totalTokens; got != 3*15 {
		t.Fatalf("removed file still counted: total %d", got)
	}
}

// A price catalog downloaded again carries a new timestamp and, most times,
// the same rates. Only changed rates may cost the history a second read.
func TestClaudeStatsCacheSurvivesAPriceRefreshWithTheSameRates(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude", "projects", "-tmp-mewla")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "big.jsonl")
	if err := os.WriteFile(path, []byte(claudeSessionLines(0, 4000)), 0o644); err != nil {
		t.Fatal(err)
	}
	size, _ := os.Stat(path)

	prices.mu.Lock()
	savedModels, savedAt, savedSource := prices.models, prices.updatedAt, prices.source
	prices.models = cloneProviderPricing(savedModels)
	prices.mu.Unlock()
	t.Cleanup(func() {
		prices.mu.Lock()
		prices.models, prices.updatedAt, prices.source = savedModels, savedAt, savedSource
		prices.mu.Unlock()
	})

	c := NewCollector()
	c.collectClaudeSessionStats(home)

	prices.mu.Lock()
	prices.updatedAt = time.Now().Add(time.Hour)
	for _, models := range prices.models {
		for id, p := range models {
			p.updatedAt = time.Now()
			models[id] = p
		}
	}
	prices.mu.Unlock()
	before := selfRchar(t)
	c.collectClaudeSessionStats(home)
	if read := selfRchar(t) - before; read > uint64(size.Size())/20 {
		t.Fatalf("a price refresh with the same rates read the history again: %d bytes", read)
	}

	prices.mu.Lock()
	for _, models := range prices.models {
		for id, p := range models {
			p.input += 1
			models[id] = p
		}
	}
	prices.mu.Unlock()
	// The scan of the file is kept, so other rates cost no read: the estimates
	// are priced again from what the scan holds, and must be the ones a
	// collector starting now would give.
	got := c.collectClaudeSessionStats(home)["2026-04-04"].models["claude-sonnet-4-6"]
	want := NewCollector().collectClaudeSessionStats(home)["2026-04-04"].models["claude-sonnet-4-6"]
	if want.estimatedCost <= 0 || math.Abs(got.estimatedCost-want.estimatedCost) > 1e-9*want.estimatedCost {
		t.Fatalf("changed rates kept costs priced at the old ones: %v, want %v", got.estimatedCost, want.estimatedCost)
	}
}
