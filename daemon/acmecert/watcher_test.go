package acmecert

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The renewer writes the certificate and status.json into Dir and keeps the
// account key elsewhere; a Watcher of Dir serves what it renewed, reports its
// status, and never needs the renewer's credential.
func TestWatcherServesWhatTheKeeperRenews(t *testing.T) {
	now := time.Now()
	root := t.TempDir()
	dir, accountKey := filepath.Join(root, "cert"), filepath.Join(root, "account", AccountKeyFile)
	seedC, seedK := writePair(t, t.TempDir(), "seed", now.Add(-time.Hour), now.Add(80*24*time.Hour))
	ca := &fakeCA{t: t, life: 90 * 24 * time.Hour, done: make(chan struct{}, 4)}
	k := newKeeper(t, ca, Config{Dir: dir, AccountKey: accountKey, SeedCert: seedC, SeedKey: seedK})
	w, err := NewWatcher(dir, seedC, seedK, time.Hour, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	before := watcherSerial(t, w)
	if d := w.Status().DaysLeft(now); d < 79 || d > 80 {
		t.Fatalf("the watcher should serve the seed first: %.1f days", d)
	}

	runUntilCleanup(t, k.Run)
	runUntilCleanup(t, w.Run)
	time.Sleep(100 * time.Millisecond)
	k.RenewNow()
	waitIssue(t, ca)
	// The keeper holds the renewal before it rewrites status.json (and status.json
	// of its first look exists already): a reload in between serves the new
	// certificate with the old status, which this watcher, looking hourly, would
	// not read again. Reload once status.json itself has the renewal.
	waitFor(t, func() bool {
		var onDisk Status
		raw, err := os.ReadFile(filepath.Join(dir, StatusFile))
		return err == nil && json.Unmarshal(raw, &onDisk) == nil && onDisk.Renewed.After(now)
	})
	w.Reload()
	waitFor(t, func() bool { return watcherSerial(t, w) != before })
	if watcherSerial(t, w) != serial(t, k) {
		t.Fatal("the watcher does not serve the renewed certificate")
	}
	waitFor(t, func() bool { return !w.Status().Renewed.IsZero() })
	st := w.Status()
	if st.Domain != testDomain || st.Failures != 0 || st.Checked.IsZero() || st.DaysLeft(time.Now()) < 89 {
		t.Fatalf("watcher status: %+v", st)
	}
	var onDisk Status
	raw, _ := os.ReadFile(filepath.Join(dir, StatusFile))
	if err := json.Unmarshal(raw, &onDisk); err != nil || onDisk.Domain != testDomain {
		t.Fatalf("status.json: %s %v", raw, err)
	}
	// The ACME account key is kept out of the directory the server reads.
	if _, err := k.accountKey(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, AccountKeyFile)); !os.IsNotExist(err) {
		t.Fatalf("account key in the served directory: %v", err)
	}
	if info, err := os.Stat(accountKey); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("account key: %v %v", info, err)
	}
}

// A pair caught between its two writes (a new chain with the old key) is
// not loaded: the served certificate stays until the pair matches.
func TestWatcherKeepsServingThroughAHalfWrittenPair(t *testing.T) {
	now := time.Now()
	dir := t.TempDir()
	c1, k1 := pemPair(t, now.Add(-time.Hour), now.Add(60*24*time.Hour))
	_ = os.WriteFile(filepath.Join(dir, CertFile), c1, 0o600)
	_ = os.WriteFile(filepath.Join(dir, KeyFile), k1, 0o600)
	w, err := NewWatcher(dir, "", "", time.Hour, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	first := watcherSerial(t, w)
	c2, k2 := pemPair(t, now.Add(-time.Hour), now.Add(90*24*time.Hour))
	_ = os.WriteFile(filepath.Join(dir, CertFile), c2, 0o600)
	w.look()
	if watcherSerial(t, w) != first || w.Status().LastError == "" {
		t.Fatalf("a mismatched pair was loaded or not reported: %+v", w.Status())
	}
	_ = os.WriteFile(filepath.Join(dir, KeyFile), k2, 0o600)
	w.look()
	if watcherSerial(t, w) == first || w.Status().LastError != "" {
		t.Fatal("the matched pair was not loaded")
	}

	// What the keeper wrote wins over a seed, even when it expires sooner (a
	// CA's shorter lifetime); an expired pair in Dir does not.
	seedC, seedK := writePair(t, t.TempDir(), "seed", now.Add(-time.Hour), now.Add(85*24*time.Hour))
	shorter := t.TempDir()
	_ = os.WriteFile(filepath.Join(shorter, CertFile), c1, 0o600)
	_ = os.WriteFile(filepath.Join(shorter, KeyFile), k1, 0o600)
	w2, _ := NewWatcher(shorter, seedC, seedK, time.Hour, t.Logf)
	if d := w2.Status().DaysLeft(now); d > 61 {
		t.Fatalf("the seed was served over the keeper's certificate: %.1f days", d)
	}
	expired := t.TempDir()
	c3, k3 := pemPair(t, now.Add(-48*time.Hour), now.Add(-time.Hour))
	_ = os.WriteFile(filepath.Join(expired, CertFile), c3, 0o600)
	_ = os.WriteFile(filepath.Join(expired, KeyFile), k3, 0o600)
	w3, _ := NewWatcher(expired, seedC, seedK, time.Hour, t.Logf)
	if d := w3.Status().DaysLeft(now); d < 84 {
		t.Fatalf("an expired certificate replaced the seed: %.1f days", d)
	}
}

func watcherSerial(t *testing.T, w *Watcher) string {
	t.Helper()
	c, err := w.GetCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}
	return c.Leaf.SerialNumber.String()
}
