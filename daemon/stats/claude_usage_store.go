package stats

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"time"
)

// The Claude session files add up to gigabytes, and a refresh reads only the
// ones that changed (Collector.claudeFiles). That memory went with the
// process, so every start of the daemon, and every hot reload of a development
// build, read all of them again: some 8 GB at 100s of MB a second. The cache is
// written beside the other usage caches and read back when the daemon starts.

// claudeUsageStoreVersion is bumped when scanSessionJSONL changes what it adds
// up from a session file. The shape of the aggregates and the price table are
// part of the cache's key already.
const claudeUsageStoreVersion = 1

const claudeUsageStoreFile = "claude-files-v1.json"

type claudeUsageStore struct {
	Key   string
	Files map[string]claudeUsageStoreEntry
}

type claudeUsageStoreEntry struct {
	Size     int64
	Modified time.Time
	ByDate   any
}

// claudeUsageStoreKey says what a stored aggregate was computed from: the
// version of the scan, the shape of the aggregate and the prices applied to it
// (a file's cost estimates come from the price table, so other rates mean other
// aggregates). The time prices were downloaded at is not part of it.
func claudeUsageStoreKey(prices map[string]map[string]modelPricing) string {
	hash := sha256.New()
	fmt.Fprintf(hash, "%d\n", claudeUsageStoreVersion)
	writeUsageShape(hash, reflect.TypeOf(map[string]*dateAgg{}))
	providers := make([]string, 0, len(prices))
	for provider := range prices {
		providers = append(providers, provider)
	}
	sort.Strings(providers)
	for _, provider := range providers {
		models := make([]string, 0, len(prices[provider]))
		for model := range prices[provider] {
			models = append(models, model)
		}
		sort.Strings(models)
		for _, model := range models {
			price := prices[provider][model]
			price.updatedAt = time.Time{}
			fmt.Fprintf(hash, "%s|%s|%+v\n", provider, model, price)
		}
	}
	return fmt.Sprintf("%x", hash.Sum(nil))
}

func writeUsageShape(out interface{ Write([]byte) (int, error) }, t reflect.Type) {
	fmt.Fprintf(out, "%s(", t.Kind())
	switch t.Kind() {
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			fmt.Fprintf(out, "%s:", t.Field(i).Name)
			writeUsageShape(out, t.Field(i).Type)
		}
	case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
		writeUsageShape(out, t.Elem())
	}
	fmt.Fprint(out, ")")
}

// loadClaudeFiles returns the stored per-file aggregates whose key is key, or
// an empty cache: a missing, unreadable or foreign store only costs a rescan.
func (c *Collector) loadClaudeFiles(key string) map[string]claudeFileUsage {
	files := map[string]claudeFileUsage{}
	if c.usageCacheDir == "" {
		return files
	}
	raw, err := os.ReadFile(filepath.Join(c.usageCacheDir, claudeUsageStoreFile))
	if err != nil {
		return files
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var store claudeUsageStore
	if decoder.Decode(&store) != nil || store.Key != key {
		return files
	}
	for path, entry := range store.Files {
		var byDate map[string]*dateAgg
		if decodeUsage(entry.ByDate, reflect.ValueOf(&byDate).Elem()) != nil {
			return map[string]claudeFileUsage{}
		}
		if byDate == nil {
			byDate = map[string]*dateAgg{}
		}
		files[path] = claudeFileUsage{size: entry.Size, modified: entry.Modified, byDate: byDate}
	}
	return files
}

// saveClaudeFiles writes the per-file aggregates; a failure only costs the
// next start a rescan.
func (c *Collector) saveClaudeFiles(key string, files map[string]claudeFileUsage) {
	if c.usageCacheDir == "" {
		return
	}
	store := claudeUsageStore{Key: key, Files: make(map[string]claudeUsageStoreEntry, len(files))}
	for path, file := range files {
		byDate, err := encodeUsage(reflect.ValueOf(file.byDate))
		if err != nil {
			return
		}
		store.Files[path] = claudeUsageStoreEntry{Size: file.size, Modified: file.modified, ByDate: byDate}
	}
	raw, err := json.Marshal(store)
	if err != nil {
		return
	}
	_ = writeUsageCache(filepath.Join(c.usageCacheDir, claudeUsageStoreFile), raw)
}
