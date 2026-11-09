package stats

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/atomicfile"
)

type usageFileStamp struct {
	Size     int64
	Modified time.Time
	Identity string
}

func usageStamp(info os.FileInfo) usageFileStamp {
	s := usageFileStamp{Size: info.Size(), Modified: info.ModTime()}
	s.Identity = fileIdentity(info)
	return s
}

type usageFileEntry[T any] struct {
	Stamp usageFileStamp
	Value T
}

// usageMemo keeps the bytes of the cache files cachedUsageFile last read or
// wrote, so a refresh whose source files did not change reads none of them
// again: each is a read of its own and there is one per source file. The bytes
// are decoded afresh each time, so a caller may change what it gets. The memo
// is bounded by the size of those bytes.
var usageMemo = struct {
	sync.Mutex
	entries map[string]usageMemoEntry
	bytes   int64
}{entries: map[string]usageMemoEntry{}}

const usageMemoLimit = 64 << 20

type usageMemoEntry struct {
	stamp usageFileStamp
	raw   []byte
}

func usageMemoGet(target string, stamp usageFileStamp) ([]byte, bool) {
	usageMemo.Lock()
	defer usageMemo.Unlock()
	entry, ok := usageMemo.entries[target]
	if ok && entry.stamp.Size == stamp.Size && entry.stamp.Modified.Equal(stamp.Modified) && entry.stamp.Identity == stamp.Identity {
		return entry.raw, true
	}
	return nil, false
}

func usageMemoPut(target string, stamp usageFileStamp, raw []byte) {
	usageMemo.Lock()
	defer usageMemo.Unlock()
	if previous, ok := usageMemo.entries[target]; ok {
		usageMemo.bytes -= int64(len(previous.raw))
	}
	if usageMemo.bytes+int64(len(raw)) > usageMemoLimit {
		usageMemo.entries, usageMemo.bytes = map[string]usageMemoEntry{}, 0
	}
	usageMemo.entries[target] = usageMemoEntry{stamp: stamp, raw: raw}
	usageMemo.bytes += int64(len(raw))
}

func cachedUsageFile[T any](dir, kind, path string, parse func() (T, error)) (T, error) {
	if dir == "" {
		return parse()
	}
	info, err := os.Stat(path)
	if err != nil {
		return parse()
	}
	stamp := usageStamp(info)
	key := sha256.Sum256([]byte(kind + "\x00" + path + "\x00" + time.Local.String()))
	target := filepath.Join(dir, fmt.Sprintf("%x.json", key))
	if raw, ok := usageMemoGet(target, stamp); ok {
		var e usageFileEntry[T]
		if json.Unmarshal(raw, &e) == nil {
			return e.Value, nil
		}
	}
	if raw, err := os.ReadFile(target); err == nil {
		var e usageFileEntry[T]
		if json.Unmarshal(raw, &e) == nil && e.Stamp.Size == stamp.Size && e.Stamp.Modified.Equal(stamp.Modified) && e.Stamp.Identity == stamp.Identity {
			usageMemoPut(target, stamp, raw)
			return e.Value, nil
		}
	}
	value, err := parse()
	if err != nil {
		return value, err
	}
	after, err := os.Stat(path)
	if err == nil && usageStamp(after) == stamp {
		raw, err := json.Marshal(usageFileEntry[T]{stamp, value})
		if err == nil {
			_ = writeUsageCache(target, raw)
			usageMemoPut(target, stamp, raw)
		}
	}
	return value, nil
}
func writeUsageCache(path string, raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return atomicfile.Write(path, raw, 0o600)
}

// Explicit wire shape keeps the cache limited to numeric usage aggregates.
type codexUsageWire struct {
	Total, Input, Output, Reasoning, Cache int64
	Context                                map[int64]codexUsage
}

func (u codexUsage) MarshalJSON() ([]byte, error) {
	return json.Marshal(codexUsageWire{u.totalTokens, u.inputTokens, u.outputTokens, u.reasoningTokens, u.cacheRead, u.byContext})
}
func (u *codexUsage) UnmarshalJSON(raw []byte) error {
	var w codexUsageWire
	if err := json.Unmarshal(raw, &w); err != nil {
		return err
	}
	*u = codexUsage{w.Total, w.Input, w.Output, w.Reasoning, w.Cache, w.Context}
	return nil
}

type statsSummaryCache struct {
	At       time.Time
	Timezone string
	Stats    *StatsResponse
}

func (c *Collector) loadRecentSummary() bool {
	if c.usageCacheDir == "" {
		return false
	}
	raw, err := os.ReadFile(filepath.Join(c.usageCacheDir, "summary.json"))
	if err != nil {
		return false
	}
	var v statsSummaryCache
	if json.Unmarshal(raw, &v) != nil || v.Stats == nil || v.Timezone != time.Local.String() || time.Since(v.At) < 0 || time.Since(v.At) > 5*time.Minute || v.At.In(time.Local).Format("2006-01-02") != time.Now().Format("2006-01-02") {
		return false
	}
	c.mu.Lock()
	c.cached = v.Stats
	c.mu.Unlock()
	return true
}
func (c *Collector) saveSummary() {
	if c.usageCacheDir == "" {
		return
	}
	c.mu.RLock()
	v := statsSummaryCache{time.Now(), time.Local.String(), c.cached}
	raw, err := json.Marshal(v)
	c.mu.RUnlock()
	if err == nil {
		_ = writeUsageCache(filepath.Join(c.usageCacheDir, "summary.json"), raw)
	}
}
