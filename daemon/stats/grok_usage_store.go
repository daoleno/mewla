package stats

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
)

// Grok sessions are kept like Claude's (claude_usage_store.go): what each one
// contributed is written beside the other usage caches, so a start of the
// daemon reads only the sessions that changed meanwhile instead of every
// session's updates again.

// grokUsageStoreVersion is bumped when scanGrokSession changes what it adds up
// from a session. The shape of the aggregates and the price table are part of
// the key already.
const grokUsageStoreVersion = 1

const grokUsageStoreFile = "grok-files-v1.json"

type grokUsageStore struct {
	Key      string
	Sessions map[string]grokUsageStoreEntry
}

type grokUsageStoreEntry struct {
	Stamp  string
	ByDate any
}

func grokUsageStoreKey(prices map[string]map[string]modelPricing) string {
	hash := sha256.New()
	fmt.Fprintf(hash, "grok %d\n", grokUsageStoreVersion)
	writeUsageShape(hash, reflect.TypeOf(map[string]*dateAgg{}))
	writeUsagePrices(hash, prices)
	return fmt.Sprintf("%x", hash.Sum(nil))
}

// loadGrokFiles returns the stored sessions whose key is key, or an empty
// set: a missing, unreadable or foreign store only costs a rescan.
func (c *Collector) loadGrokFiles(key string) map[string]grokSessionUsage {
	sessions := map[string]grokSessionUsage{}
	if c.usageCacheDir == "" {
		return sessions
	}
	raw, err := os.ReadFile(filepath.Join(c.usageCacheDir, grokUsageStoreFile))
	if err != nil {
		return sessions
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var store grokUsageStore
	if decoder.Decode(&store) != nil || store.Key != key {
		return sessions
	}
	for dir, entry := range store.Sessions {
		var byDate map[string]*dateAgg
		if decodeUsage(entry.ByDate, reflect.ValueOf(&byDate).Elem()) != nil {
			return map[string]grokSessionUsage{}
		}
		if byDate == nil {
			byDate = map[string]*dateAgg{}
		}
		sessions[dir] = grokSessionUsage{stamp: entry.Stamp, byDate: byDate}
	}
	return sessions
}

// saveGrokFiles writes the per-session aggregates; a failure only costs the
// next start a rescan.
func (c *Collector) saveGrokFiles(key string, sessions map[string]grokSessionUsage) {
	if c.usageCacheDir == "" {
		return
	}
	store := grokUsageStore{Key: key, Sessions: make(map[string]grokUsageStoreEntry, len(sessions))}
	for dir, session := range sessions {
		byDate, err := encodeUsage(reflect.ValueOf(session.byDate))
		if err != nil {
			return
		}
		store.Sessions[dir] = grokUsageStoreEntry{Stamp: session.stamp, ByDate: byDate}
	}
	raw, err := json.Marshal(store)
	if err != nil {
		return
	}
	_ = writeUsageCache(filepath.Join(c.usageCacheDir, grokUsageStoreFile), raw)
}
