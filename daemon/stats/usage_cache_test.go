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
	// What the process remembers of the files is right whatever the disk says;
	// a new process has only the disk.
	if _, err = cachedUsageFile(cache, "test", path, parse); err != nil || calls != 2 {
		t.Fatalf("a remembered cache file was read again: %d parses, err %v", calls, err)
	}
	usageMemo.Lock()
	usageMemo.entries, usageMemo.bytes = map[string]usageMemoEntry{}, 0
	usageMemo.Unlock()
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

// A refresh whose source files did not change must not read their cache files
// either: there is one per source file.
func TestCachedUsageFileKeepsWhatItReadInMemory(t *testing.T) {
	dir, source := t.TempDir(), filepath.Join(t.TempDir(), "source.jsonl")
	if err := os.WriteFile(source, []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	parses := 0
	parse := func() (map[string]int, error) { parses++; return map[string]int{"n": parses}, nil }
	if _, err := cachedUsageFile(dir, "memo", source, parse); err != nil {
		t.Fatal(err)
	}
	// Without its cache file the answer can only come from memory.
	cacheFiles, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	if len(cacheFiles) != 1 {
		t.Fatalf("cache files = %v", cacheFiles)
	}
	if err := os.Remove(cacheFiles[0]); err != nil {
		t.Fatal(err)
	}
	got, err := cachedUsageFile(dir, "memo", source, parse)
	if err != nil || got["n"] != 1 || parses != 1 {
		t.Fatalf("unchanged source: got %v err %v after %d parses, want the first answer from memory", got, err, parses)
	}
	// A changed source is parsed again.
	if err := os.WriteFile(source, []byte("two\n two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, _ = cachedUsageFile(dir, "memo", source, parse); got["n"] != 2 || parses != 2 {
		t.Fatalf("changed source: got %v after %d parses, want a second parse", got, parses)
	}
}

func TestCodexStateStampFollowsTheDatabaseAndItsLog(t *testing.T) {
	db := filepath.Join(t.TempDir(), "state.sqlite")
	if codexStateStamp(db) != "" {
		t.Fatal("a missing database has a stamp")
	}
	if err := os.WriteFile(db, []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	first := codexStateStamp(db)
	if first == "" || first != codexStateStamp(db) {
		t.Fatalf("stamp %q is not stable", first)
	}
	if err := os.WriteFile(db+"-wal", []byte("w"), 0o600); err != nil {
		t.Fatal(err)
	}
	if codexStateStamp(db) == first {
		t.Fatal("a write-ahead log did not change the stamp")
	}
}
