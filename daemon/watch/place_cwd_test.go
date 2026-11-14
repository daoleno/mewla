package watch

import "testing"

// A Session place stored from a raw OSC 7 report reads back as its path, and
// a new report replaces it with the path.
func TestPlaceHealsAnOSC7Cwd(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store.index.Sessions["s1"] = "file:///Users/x/a%20b"
	if got := store.PlaceOf("session", "s1"); got != "/Users/x/a b" {
		t.Fatalf("stored place reads as %q", got)
	}
	store.Place("session", "s2", "file://localhost/Users/x/%E9%A1%B9%E7%9B%AE")
	if got := store.index.Sessions["s2"]; got != "/Users/x/项目" {
		t.Fatalf("place stored as %q", got)
	}
}
