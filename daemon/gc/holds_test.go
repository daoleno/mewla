package gc

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHoldsSumUpKindsOwnersKeepsAndToday(t *testing.T) {
	root, unit, scratch := whyFixture(t)
	now := time.Now()
	if _, err := RecordSizes(root, now.AddDate(0, 0, -7)); err != nil { // a week ago the scratch was empty
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scratch, "build.out"), make([]byte, 8192), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := AddKeep(root, filepath.Join(scratch, "build.out"), unit, now.Add(48*time.Hour), "the gate reads it", now); err != nil {
		t.Fatal(err)
	}
	loose := filepath.Join(root, "t", "loose")
	if err := os.MkdirAll(loose, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(loose, "x"), make([]byte, 300), 0o600); err != nil {
		t.Fatal(err)
	}
	item, err := Trash(root, KindUnmarked, loose, "", ReasonUnmarked, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordSizes(root, now); err != nil {
		t.Fatal(err)
	}
	BindSession(root, unit, "mh-big", now)
	lookup := WhyLookup{Root: root, Work: func(session string) (WhyLink, bool) {
		return WhyLink{ID: "w-1", Name: "Release"}, session == "mh-big"
	}}
	h, err := SummarizeHolds(lookup, now)
	if err != nil {
		t.Fatal(err)
	}
	if h.Day != now.Local().Format("2006-01-02") || len(h.Kinds) != 5 {
		t.Fatalf("day %s kinds %+v", h.Day, h.Kinds)
	}
	kinds := map[string]HoldKind{}
	for _, k := range h.Kinds {
		kinds[k.Kind] = k
	}
	if k := kinds[SizeScratch]; k.Growth7d == nil || *k.Growth7d < 8192 {
		t.Fatalf("scratch growth %+v", k)
	}
	if k := kinds[SizeTrash]; k.Count != 1 || k.Bytes != 300 {
		t.Fatalf("trash %+v", k)
	}
	if len(h.Owners) != 1 || h.Owners[0].SessionID != "mh-big" || h.Owners[0].WorkTitle != "Release" || h.Owners[0].Bytes < 8192 {
		t.Fatalf("owners %+v", h.Owners)
	}
	if len(h.Kept) != 1 || h.Kept[0].SessionID != "mh-big" || h.Kept[0].Why != "the gate reads it" {
		t.Fatalf("kept %+v", h.Kept)
	}
	if h.Today.Items != 1 || h.Today.Bytes != 300 || len(h.Today.Restorable) != 1 || h.Today.Restorable[0].ID != item.ID {
		t.Fatalf("today %+v", h.Today)
	}
	// Restored, it is no longer restorable; it still counts as collected today.
	if _, err := Restore(root, item.ID); err != nil {
		t.Fatal(err)
	}
	if h, _ := SummarizeHolds(lookup, now); len(h.Today.Restorable) != 0 || h.Today.Items != 1 {
		t.Fatalf("after restore %+v", h.Today)
	}
}

func TestHoldsBeforeAnySampleAreEmptyNotAnError(t *testing.T) {
	h, err := SummarizeHolds(WhyLookup{Root: t.TempDir()}, time.Now())
	if err != nil || h.Day != "" || len(h.Kinds) != 0 || h.Kinds == nil {
		t.Fatalf("%+v %v", h, err)
	}
}
