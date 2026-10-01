package stats

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUsageCacheSurvivesRestartAndInvalidatesSource(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "source")
	cache := filepath.Join(dir, "cache")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	parse := func() (map[string]codexUsage, error) {
		calls++
		return map[string]codexUsage{"day": {totalTokens: 123, byContext: map[int64]codexUsage{42: {inputTokens: 100}}}}, nil
	}
	first, err := cachedUsageFile(cache, "test", path, parse)
	if err != nil {
		t.Fatal(err)
	}
	first["day"] = codexUsage{}
	// No in-memory cache: the next call models another process/collector.
	second, err := cachedUsageFile(cache, "test", path, parse)
	if err != nil || calls != 1 || second["day"].byContext[42].inputTokens != 100 {
		t.Fatalf("cache miss or damaged context: %d %+v %v", calls, second, err)
	}
	if err := os.WriteFile(path, []byte("append changes source"), 0600); err != nil {
		t.Fatal(err)
	}
	_, _ = cachedUsageFile(cache, "test", path, parse)
	if calls != 2 {
		t.Fatal("source change did not invalidate")
	}
	entries, _ := os.ReadDir(cache)
	for _, e := range entries {
		_ = os.WriteFile(filepath.Join(cache, e.Name()), []byte("corrupt"), 0600)
	}
	_, err = cachedUsageFile(cache, "test", path, parse)
	if err != nil || calls != 3 {
		t.Fatal("corrupt cache did not recover")
	}
}
func TestStatsRecentSummaryOnlySkipsFreshRestartScan(t *testing.T) {
	c := &Collector{usageCacheDir: t.TempDir(), cached: &StatsResponse{Type: "stats_data"}}
	c.saveSummary()
	next := &Collector{usageCacheDir: c.usageCacheDir}
	if !next.loadRecentSummary() || next.cached.Type != "stats_data" {
		t.Fatal("fresh summary not loaded")
	}
	// Preserve the file format but make its age stale.
	raw := []byte(`{"At":"2000-01-01T00:00:00Z","Timezone":"` + time.Local.String() + `","Stats":{"type":"stats_data"}}`)
	_ = os.WriteFile(filepath.Join(c.usageCacheDir, "summary.json"), raw, 0600)
	if next.loadRecentSummary() {
		t.Fatal("stale summary reused")
	}
}
