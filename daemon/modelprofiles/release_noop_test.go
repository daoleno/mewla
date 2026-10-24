package modelprofiles

import (
	"bytes"
	"fmt"
	"os"
	"testing"
)

// Releasing a Session that has no binding changes nothing, so it must not
// rewrite (and fsync) the durable route file. A binding that does exist is
// still released and persisted.
func TestReleaseSessionWithoutBindingDoesNotRewriteRouteFile(t *testing.T) {
	profiles, routes, listener := stage2bRoot(t)
	owner, err := StartOwner(OwnerConfig{ProfilesPath: profiles, RoutesPath: routes, ListenerPath: listener,
		Lookup: readyLookup("test-secret"), Verifier: lifecycleTestVerifier{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	if _, err := owner.UpsertProfile(codexResponsesProfile("a", "gpt-5", "up-a"), 0, true); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"live:@1", "live:@2"} {
		plan, err := owner.PrepareLaunch(ExecutorCodex, "a", "codex")
		if err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, id); err != nil {
			t.Fatal(err)
		}
	}
	before := mustStat(t, routes)
	beforeBytes := mustRead(t, routes)

	for i := 0; i < 20; i++ {
		persist, err := owner.ReleaseSession(fmt.Sprintf("gone:%%%d", i))
		if err != nil || !persist.Applied || !persist.Durable {
			t.Fatalf("release of a missing binding: %+v %v", persist, err)
		}
	}
	after := mustStat(t, routes)
	if !os.SameFile(before, after) || !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("releasing a missing binding rewrote the route file")
	}
	if !bytes.Equal(beforeBytes, mustRead(t, routes)) {
		t.Fatal("route file content changed")
	}

	if persist, err := owner.ReleaseSession("live:@1"); err != nil || !persist.Applied || !persist.Durable {
		t.Fatalf("release of a bound Session: %+v %v", persist, err)
	}
	if bytes.Equal(beforeBytes, mustRead(t, routes)) {
		t.Fatal("releasing a bound Session must persist")
	}
	if _, found := owner.SessionSnapshot("live:@1"); found {
		t.Fatal("released Session is still bound")
	}
	if _, found := owner.SessionSnapshot("live:@2"); !found {
		t.Fatal("unrelated Session was released")
	}
}

// The durable file is a function of the table's content, not of Go map order.
func TestDurableSnapshotIsDeterministic(t *testing.T) {
	profiles, routes, listener := stage2bRoot(t)
	owner, err := StartOwner(OwnerConfig{ProfilesPath: profiles, RoutesPath: routes, ListenerPath: listener,
		Lookup: readyLookup("test-secret"), Verifier: lifecycleTestVerifier{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	if _, err := owner.UpsertProfile(codexResponsesProfile("a", "gpt-5", "up-a"), 0, true); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 12; i++ {
		plan, err := owner.PrepareLaunch(ExecutorCodex, "a", "codex")
		if err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, fmt.Sprintf("s:@%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	first, err := EncodeDurableSnapshot(owner.table.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		again, err := EncodeDurableSnapshot(owner.table.Snapshot())
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first, again) {
			t.Fatalf("snapshot %d encodes differently for an unchanged table", i)
		}
	}
}

func mustStat(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
