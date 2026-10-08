package addressbook

import (
	"path/filepath"
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
	if err := one.Learn("verified.example"); err != nil {
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
