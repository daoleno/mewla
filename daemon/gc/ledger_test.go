package gc

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTheLedgerRecordsWhatTheCollectorDid(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	dir := filepath.Join(root, "t", "old")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "f"), make([]byte, 1000), 0o600); err != nil {
		t.Fatal(err)
	}
	item, err := Trash(root, KindUnmarked, dir, "unit-a", ReasonUnmarked, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(root, item.ID); err != nil {
		t.Fatal(err)
	}
	item, err = Trash(root, KindUnmarked, dir, "unit-a", ReasonUnmarked, now.Add(-4*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Finalize(root, now); err != nil {
		t.Fatal(err)
	}
	events, err := Events(root, now.Add(-7*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	var actions []string
	for _, e := range events {
		actions = append(actions, e.Action)
		if e.Subject != dir || e.Owner != "unit-a" {
			t.Fatalf("event %+v", e)
		}
	}
	if got := join(actions); got != "trashed,restored,trashed,deleted" {
		t.Fatalf("actions %s", got)
	}
	if events[0].Bytes != 1000 || events[3].Bytes != 1000 {
		t.Fatalf("bytes %d, %d", events[0].Bytes, events[3].Bytes)
	}
	if recent, _ := Events(root, now.Add(-time.Minute)); len(recent) != 3 {
		t.Fatalf("since a minute ago: %d events, want 3 (the backdated trash is older)", len(recent))
	}
}

func TestTheLedgerKnowsASessionAfterItEnds(t *testing.T) {
	root := t.TempDir()
	at := time.Date(2026, 11, 24, 8, 0, 0, 0, time.UTC)
	if _, found, err := SessionOfUnit(root, "u1"); err != nil || found {
		t.Fatalf("no ledger yet: found=%v err=%v", found, err)
	}
	BindSession(root, "u1", "mh-1", at)
	HoldSession(root, "mh-1", "its Work closed: a build runs", at.Add(time.Minute))
	held, err := HeldSessions(root)
	if err != nil || len(held) != 1 || held[0].HeldBecause != "its Work closed: a build runs" {
		t.Fatalf("held %+v err %v", held, err)
	}
	HoldSession(root, "mh-1", "", at.Add(2*time.Minute))
	if held, _ := HeldSessions(root); len(held) != 0 {
		t.Fatalf("cleared hold still listed: %+v", held)
	}
	BindSession(root, "u1", "mh-1", at.Add(time.Hour)) // a rebind of the same Session keeps its start
	EndSession(root, "u1", at.Add(2*time.Hour))
	s, found, err := SessionOfUnit(root, "u1")
	if err != nil || !found || s.SessionID != "mh-1" || !s.BoundAt.Equal(at) || !s.EndedAt.Equal(at.Add(2*time.Hour)) {
		t.Fatalf("session %+v found=%v err=%v", s, found, err)
	}
	HoldSession(root, "mh-1", "late", at.Add(3*time.Hour))
	if held, _ := HeldSessions(root); len(held) != 0 {
		t.Fatalf("an ended Session was held: %+v", held)
	}
}

func TestSizesAreMeasuredOncePerDay(t *testing.T) {
	root, unit, scratch := whyFixture(t)
	if err := os.WriteFile(filepath.Join(scratch, "build.out"), make([]byte, 4096), 0o600); err != nil {
		t.Fatal(err)
	}
	loose := filepath.Join(root, "t", "loose")
	if err := os.MkdirAll(loose, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(loose, "x"), make([]byte, 100), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if measured, err := RecordSizes(root, now); err != nil || !measured {
		t.Fatalf("first = %v, %v", measured, err)
	}
	if measured, err := RecordSizes(root, now); err != nil || measured {
		t.Fatalf("second the same day = %v, %v", measured, err)
	}
	samples, err := Sizes(root, now.Local().Format("2006-01-02"))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]SizeSample{}
	for _, s := range samples {
		got[s.Kind] = s
	}
	if s := got[SizeScratch]; s.Count != 1 || s.Bytes < 4096 {
		t.Fatalf("scratch %+v", s)
	}
	if s := got[SizeUnmarked]; s.Count != 1 || s.Bytes != 100 {
		t.Fatalf("unmarked %+v", s)
	}
	if len(got) != 5 {
		t.Fatalf("kinds %v", got)
	}
	if got[SizeScratch].Oldest.IsZero() || !got[SizeCache].Oldest.IsZero() {
		t.Fatalf("oldest: scratch %v, empty cache %v", got[SizeScratch].Oldest, got[SizeCache].Oldest)
	}
	owners, err := OwnerSizes(root, now.Local().Format("2006-01-02"), 5)
	if err != nil || len(owners) != 1 || owners[0].Unit != unit || owners[0].Bytes < 4096 {
		t.Fatalf("owners %+v err %v", owners, err)
	}
}

func join(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ","
		}
		out += p
	}
	return out
}
