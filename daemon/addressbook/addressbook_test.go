package addressbook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStorePersistsAndReloads(t *testing.T) {
	dir := t.TempDir()
	one, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := one.Add("https://Work.Example:443/", SourceManual); err != nil {
		t.Fatal(err)
	}
	if _, err := one.Add("https://verified.example", SourceDiscovered); err != nil {
		t.Fatal(err)
	}
	two, err := New(filepath.Clean(dir))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := two.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].URL != "https://verified.example" || entries[1].URL != "https://work.example" {
		t.Fatalf("entries=%#v", entries)
	}
}

func TestNormalizeRejectsInsecureRemoteAndPath(t *testing.T) {
	for _, raw := range []string{"http://8.8.8.8", "https://example/a", "ftp://example", "https://user@example"} {
		if _, err := Normalize(raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
	if got, err := Normalize("http://127.0.0.1:9876/"); err != nil || got != "http://127.0.0.1:9876" {
		t.Fatalf("got %q err %v", got, err)
	}
}

func TestRemoveIsLiveOnNextRead(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add("https://a.example", SourceManual); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove("https://a.example"); err != nil {
		t.Fatal(err)
	}
	if s.Contains("https://a.example") {
		t.Fatal("removed address remains")
	}
}

func TestReplaceDiscoveredForgetsAddressesFromEarlierStarts(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"http://127.0.0.1:9876", "http://192.168.1.20:9876"} {
		if _, err := s.Add(raw, SourceDiscovered); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Add("https://a.example", SourceManual); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add("http://10.0.0.5:9876", SourceManual); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceDiscovered([]string{"http://127.0.0.1:9876", "http://10.0.0.5:9876", "http://10.0.0.9:9876"}); err != nil {
		t.Fatal(err)
	}
	entries, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Source{}
	for _, entry := range entries {
		got[entry.URL] = entry.Source
	}
	want := map[string]Source{
		"http://10.0.0.5:9876":  SourceManual,
		"http://10.0.0.9:9876":  SourceDiscovered,
		"http://127.0.0.1:9876": SourceDiscovered,
		"https://a.example":     SourceManual,
	}
	if len(got) != len(want) {
		t.Fatalf("entries = %v", got)
	}
	for url, source := range want {
		if got[url] != source {
			t.Fatalf("entries = %v, want %v", got, want)
		}
	}
}

func TestTailscaleAddressesAreDiscoveredAndPublicHTTPIsSkipped(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceDiscovered([]string{"http://100.92.174.90:9876", "http://8.8.8.8:9876", "http://192.168.1.20:9876"}); err != nil {
		t.Fatal(err)
	}
	if !s.Contains("http://100.92.174.90:9876") || !s.Contains("http://192.168.1.20:9876") {
		t.Fatal("Tailscale or LAN address was not recorded")
	}
	if s.Contains("http://8.8.8.8:9876") {
		t.Fatal("public plain-http address was recorded")
	}
	if _, err := Normalize("http://100.128.0.1:9876"); err == nil {
		t.Fatal("accepted an address outside 100.64.0.0/10")
	}
}

// An address book written by an earlier version learned phones' Link tunnel
// ports from request Host headers; those entries are dropped on read and
// nothing records new ones.
func TestEarlierLearnedLoopbackAddressesAreDropped(t *testing.T) {
	dir := t.TempDir()
	book, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	polluted := `{"entries":[
		{"url":"https://127.0.0.1:42091","source":"verified"},
		{"url":"https://127.0.0.1:44303","source":"verified"},
		{"url":"https://127.0.0.1:7443","source":"manual"},
		{"url":"https://swift-hill-a9de.m.mewla.ai","source":"remote"}]}`
	if err := os.WriteFile(filepath.Join(dir, fileName), []byte(polluted), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := book.List()
	if err != nil {
		t.Fatal(err)
	}
	var urls []string
	for _, entry := range entries {
		urls = append(urls, entry.URL)
	}
	if strings.Join(urls, " ") != "https://127.0.0.1:7443 https://swift-hill-a9de.m.mewla.ai" {
		t.Fatalf("entries = %v", urls)
	}
}
