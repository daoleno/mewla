package friends

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDraftsNeedALinkAreCappedAndExpire(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	store := NewStore(t.TempDir())
	store.Now = func() time.Time { return now }
	if _, err := store.AddDraft("hello", ""); err != ErrNotLinked {
		t.Fatalf("unlinked draft = %v", err)
	}
	if err := store.SaveState(State{Handle: "me", EnabledAt: now}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < MaxPendingDrafts; i++ {
		if _, err := store.AddDraft("post", "work"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.AddDraft("one more", ""); err != ErrTooManyDrafts {
		t.Fatalf("over the cap = %v", err)
	}
	now = now.Add(DraftTTL + time.Hour)
	drafts, err := store.Drafts()
	if err != nil || len(drafts) != 0 {
		t.Fatalf("expired drafts = %v, %v", drafts, err)
	}
	if _, err := store.AddDraft("fresh", ""); err != nil {
		t.Fatalf("after expiry: %v", err)
	}
	info, _ := os.Stat(filepath.Join(store.Dir, "state.json"))
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("state mode %v", info.Mode().Perm())
	}
	if err := store.Clear(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.State(); err != ErrNotLinked {
		t.Fatalf("after clear = %v", err)
	}
}
