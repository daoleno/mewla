//go:build linux

package stats

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fillUsage sets every field it can reach to a distinct non-zero value.
func fillUsage(v reflect.Value, counter *int) {
	v = usageField(v)
	*counter++
	switch v.Kind() {
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(int64(*counter))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(uint64(*counter))
	case reflect.Float32, reflect.Float64:
		v.SetFloat(float64(*counter) + 0.125)
	case reflect.String:
		v.SetString(fmt.Sprintf("s%d", *counter))
	case reflect.Pointer:
		v.Set(reflect.New(v.Type().Elem()))
		fillUsage(v.Elem(), counter)
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			fillUsage(v.Field(i), counter)
		}
	case reflect.Map:
		v.Set(reflect.MakeMap(v.Type()))
		for i := 0; i < 2; i++ {
			element := reflect.New(v.Type().Elem()).Elem()
			fillUsage(element, counter)
			v.SetMapIndex(reflect.ValueOf(fmt.Sprintf("k%d", *counter)).Convert(v.Type().Key()), element)
		}
	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 2, 2))
		for i := 0; i < 2; i++ {
			fillUsage(v.Index(i), counter)
		}
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			fillUsage(v.Index(i), counter)
		}
	}
}

// The cache on disk must carry every field of the aggregates, including ones
// added later: a field that did not come back would be a silent undercount.
func TestUsageWireRoundTripsEveryField(t *testing.T) {
	var original map[string]*dateAgg
	counter := 0
	fillUsage(reflect.ValueOf(&original).Elem(), &counter)
	if len(original) == 0 {
		t.Fatal("nothing was filled")
	}
	tree, err := encodeUsage(reflect.ValueOf(original))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(tree)
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var back any
	if err := decoder.Decode(&back); err != nil {
		t.Fatal(err)
	}
	var restored map[string]*dateAgg
	if err := decodeUsage(back, reflect.ValueOf(&restored).Elem()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original, restored) {
		t.Fatalf("round trip differs:\n%+v\n%+v", original, restored)
	}
}

func writeClaudeProject(t *testing.T, home string) (big string, size int64) {
	t.Helper()
	dir := filepath.Join(home, ".claude", "projects", "-tmp-mewla")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	big = filepath.Join(dir, "big.jsonl")
	if err := os.WriteFile(big, []byte(claudeSessionLines(0, 4000)), 0o644); err != nil { // about 2.6 MB
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "small.jsonl"), []byte(claudeSessionLines(10000, 2)), 0o644); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(big)
	return big, info.Size()
}

// A restart of the daemon (a hot reload of a development build too) must not
// read the history of Claude sessions again.
func TestClaudeStatsCacheSurvivesARestart(t *testing.T) {
	home := t.TempDir()
	cache := filepath.Join(t.TempDir(), "usage")
	big, size := writeClaudeProject(t, home)

	first := NewCollector()
	first.usageCacheDir = cache
	want := first.collectClaudeSessionStats(home)

	second := NewCollector() // a new process: no memory of the first
	second.usageCacheDir = cache
	before := selfRchar(t)
	got := second.collectClaudeSessionStats(home)
	if read := selfRchar(t) - before; read > uint64(size)/20 {
		t.Fatalf("a restart read the history again: %d bytes against a %d byte file", read, size)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("the restored totals differ:\n%+v\n%+v", want["2026-04-04"], got["2026-04-04"])
	}
	if got["2026-04-04"].models["claude-sonnet-4-6"].totalTokens != 4002*15 {
		t.Fatalf("total tokens = %d", got["2026-04-04"].models["claude-sonnet-4-6"].totalTokens)
	}

	// A file that changed while the daemon was down is read again.
	f, err := os.OpenFile(big, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(claudeSessionLines(30000, 1))
	_ = f.Close()
	third := NewCollector()
	third.usageCacheDir = cache
	if total := third.collectClaudeSessionStats(home)["2026-04-04"].models["claude-sonnet-4-6"].totalTokens; total != 4003*15 {
		t.Fatalf("a grown file was not read again: %d", total)
	}
}

func TestClaudeStatsCacheIsDroppedForOtherRatesAndDamage(t *testing.T) {
	home := t.TempDir()
	cache := filepath.Join(t.TempDir(), "usage")
	_, size := writeClaudeProject(t, home)

	first := NewCollector()
	first.usageCacheDir = cache
	first.collectClaudeSessionStats(home)

	// Other rates: the stored cost estimates are not the ones to show.
	prices.mu.Lock()
	saved := prices.models
	prices.models = cloneProviderPricing(saved)
	for _, models := range prices.models {
		for id, p := range models {
			p.input += 1
			models[id] = p
		}
	}
	prices.mu.Unlock()
	t.Cleanup(func() {
		prices.mu.Lock()
		prices.models = saved
		prices.mu.Unlock()
	})
	other := NewCollector()
	other.usageCacheDir = cache
	before := selfRchar(t)
	other.collectClaudeSessionStats(home)
	if read := selfRchar(t) - before; read < uint64(size) {
		t.Fatalf("a store made with other rates was used: %d bytes read", read)
	}

	// A store that does not parse is a rescan, not an error.
	if err := os.WriteFile(filepath.Join(cache, claudeUsageStoreFile), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	damaged := NewCollector()
	damaged.usageCacheDir = cache
	before = selfRchar(t)
	if total := damaged.collectClaudeSessionStats(home)["2026-04-04"].models["claude-sonnet-4-6"].totalTokens; total != 4002*15 {
		t.Fatalf("total tokens = %d", total)
	}
	if read := selfRchar(t) - before; read < uint64(size) {
		t.Fatalf("a damaged store was used: %d bytes read", read)
	}
}

// The key must be the same in every process: a rate table written with an
// address in it would never match the store of the process before.
func TestClaudeUsageStoreKeyNamesNoAddress(t *testing.T) {
	prices.mu.RLock()
	current := cloneProviderPricing(prices.models)
	prices.mu.RUnlock()
	var text bytes.Buffer
	writeUsagePrices(&text, current)
	if text.Len() == 0 {
		t.Fatal("no rates")
	}
	if strings.Contains(text.String(), "0xc0") {
		t.Fatalf("the rates are written with an address: %.200s", text.String())
	}
	if claudeUsageStoreKey(current) != claudeUsageStoreKey(cloneProviderPricing(current)) {
		t.Fatal("the key differs for the same rates")
	}
}
