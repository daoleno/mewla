package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/selfupdate"
	"github.com/daoleno/mewla/daemon/server"
)

func versionScript(version string) []byte {
	return []byte("#!/bin/sh\nif [ \"$1\" = version ]; then echo '{\"version\":\"" + version + "\"}'; exit 0; fi\nexit 0\n")
}

// releaseServer serves one signed stable release the way GitHub does: a
// /latest redirect, a signed manifest and a platform archive.
func releaseServer(t *testing.T, version string, binary []byte) (*selfupdate.Client, *int32) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "mewla", Mode: 0o755, Size: int64(len(binary)), Typeflag: tar.TypeReg})
	_, _ = tw.Write(binary)
	_ = tw.Close()
	_ = gz.Close()
	name, err := selfupdate.PlatformArtifactName("linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(archive.Bytes())
	manifest, _ := json.Marshal(selfupdate.Manifest{
		SchemaVersion: 2, Product: "mewla", Version: version,
		Artifacts: []selfupdate.Artifact{{Path: name, Role: "daemon_archive", SHA256: hex.EncodeToString(digest[:]), Size: int64(archive.Len()), GOOS: "linux", GOARCH: "amd64"}},
	})
	var downloads int32
	mux := http.NewServeMux()
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/releases/tag/v"+version, http.StatusFound)
	})
	base := "/releases/download/v" + version + "/"
	mux.HandleFunc(base+selfupdate.ManifestAsset, func(w http.ResponseWriter, r *http.Request) { w.Write(manifest) })
	mux.HandleFunc(base+selfupdate.ManifestSignature, func(w http.ResponseWriter, r *http.Request) {
		w.Write(ed25519.Sign(private, manifest))
	})
	mux.HandleFunc(base+name, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&downloads, 1)
		w.Write(archive.Bytes())
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &selfupdate.Client{
		HTTPClient: srv.Client(), ReleasesURL: srv.URL + "/releases", PublicKey: public, GOOS: "linux", GOARCH: "amd64",
	}, &downloads
}

type updaterFixture struct {
	updater    *daemonUpdater
	installed  string
	handovers  chan *updateHandover
	downloads  *int32
	idle       *atomic.Bool
	stateDir   string
	currentVer string
}

func newUpdaterFixture(t *testing.T, current, release string, releaseBinary []byte) updaterFixture {
	t.Helper()
	dir := t.TempDir()
	installed := filepath.Join(dir, "mewla")
	if err := os.WriteFile(installed, versionScript(current), 0o755); err != nil {
		t.Fatal(err)
	}
	client, downloads := releaseServer(t, release, releaseBinary)
	idle := &atomic.Bool{}
	idle.Store(true)
	handovers := make(chan *updateHandover, 4)
	updater := newDaemonUpdater(current, filepath.Join(dir, "state"), "127.0.0.1:0", "daemon-1",
		idle.Load, func(h *updateHandover) { handovers <- h })
	updater.supported = true
	updater.installed = installed
	updater.newClient = func() (*selfupdate.Client, error) { return client, nil }
	updater.first, updater.interval, updater.idleWait = 10*time.Millisecond, time.Hour, 20*time.Millisecond
	if err := os.MkdirAll(updater.stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	return updaterFixture{updater: updater, installed: installed, handovers: handovers, downloads: downloads, idle: idle, stateDir: updater.stateDir, currentVer: current}
}

func TestSelfUpdateOnlyForStampedStableReleaseBuilds(t *testing.T) {
	defer func(previous string) { releaseBuild = previous }(releaseBuild)
	releaseBuild = ""
	if selfUpdateSupported("0.2.87") {
		t.Fatal("an unstamped (dev or local) build must never update itself")
	}
	releaseBuild = "1"
	if !selfUpdateSupported("0.2.87") {
		t.Fatal("a stamped stable release build should update itself")
	}
	if selfUpdateSupported("0.2.88-beta.1") {
		t.Fatal("a beta must never update itself")
	}
	t.Setenv("MEWLA_NO_AUTOUPDATE", "1")
	if selfUpdateSupported("0.2.87") {
		t.Fatal("MEWLA_NO_AUTOUPDATE must switch updates off")
	}
	t.Setenv("MEWLA_NO_AUTOUPDATE", "")
	t.Setenv(selfupdate.ChildEnv, "1")
	if selfUpdateSupported("0.2.87") {
		t.Fatal("the trial child must not update")
	}
}

func TestDevBuildNeverChecksOrInstalls(t *testing.T) {
	f := newUpdaterFixture(t, "0.2.87", "0.2.88", versionScript("0.2.88"))
	f.updater.supported = false
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	f.updater.Run(ctx) // returns at once; nothing to check
	if err := f.updater.Install(); err != server.ErrUpdateUnavailable {
		t.Fatalf("Install on an unsupported build = %v", err)
	}
	select {
	case <-f.handovers:
		t.Fatal("handover requested by a build that does not update")
	default:
	}
	if atomic.LoadInt32(f.downloads) != 0 {
		t.Fatal("an unsupported build downloaded a release")
	}
	if status := f.updater.Status(); status.Supported || status.Version != "0.2.87" {
		t.Fatalf("status = %+v", status)
	}
}

func TestUpdateWaitsUntilIdleThenStagesWithoutTouchingTheInstalledBinary(t *testing.T) {
	f := newUpdaterFixture(t, "0.2.87", "0.2.88", versionScript("0.2.88"))
	f.idle.Store(false)
	before, _ := os.ReadFile(f.installed)
	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { f.updater.Run(ctx); close(done) }()

	select {
	case <-f.handovers:
		t.Fatal("updated while a Worker was running")
	case <-time.After(300 * time.Millisecond):
	}
	if status := f.updater.Status(); status.Latest != "0.2.88" || status.Updating {
		t.Fatalf("while busy, status = %+v, want the release offered but not installing", status)
	}
	f.idle.Store(true)
	select {
	case h := <-f.handovers:
		if h.Version != "0.2.88" || h.Staged != f.installed+selfupdate.StagedSuffix || h.DaemonID != "daemon-1" {
			t.Fatalf("handover = %+v", h)
		}
		if _, err := os.Stat(h.Staged); err != nil {
			t.Fatalf("staged binary missing: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no handover once idle")
	}
	after, _ := os.ReadFile(f.installed)
	if !bytes.Equal(before, after) {
		t.Fatal("staging must not change the installed binary")
	}
	<-done
	if status := f.updater.Status(); !status.Updating {
		t.Fatalf("status after handover = %+v", status)
	}
}

func TestUpdateButtonInstallsNow(t *testing.T) {
	f := newUpdaterFixture(t, "0.2.87", "0.2.88", versionScript("0.2.88"))
	f.idle.Store(false) // the button does not wait for idle
	if err := f.updater.Install(); err != nil {
		t.Fatal(err)
	}
	select {
	case h := <-f.handovers:
		if h.Version != "0.2.88" {
			t.Fatalf("handover = %+v", h)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("button did not start the update")
	}
}

func TestNothingToInstallWhenCurrent(t *testing.T) {
	f := newUpdaterFixture(t, "0.2.88", "0.2.88", versionScript("0.2.88"))
	if err := f.updater.install(context.Background(), false); err != server.ErrUpdateUnavailable {
		t.Fatalf("install when current = %v", err)
	}
	if status := f.updater.Status(); status.Latest != "" {
		t.Fatalf("status = %+v", status)
	}
}

func TestRolledBackVersionIsNotOfferedAgain(t *testing.T) {
	f := newUpdaterFixture(t, "0.2.87", "0.2.88", versionScript("0.2.88"))
	if err := selfupdate.RecordFailure(f.stateDir, selfupdate.Failure{Version: "0.2.88", Reason: "exited", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := f.updater.install(context.Background(), false); err != server.ErrUpdateUnavailable {
		t.Fatalf("install of a rolled-back version = %v", err)
	}
	status := f.updater.Status()
	if status.Latest != "" || !strings.Contains(status.Error, "0.2.88") {
		t.Fatalf("status = %+v, want the failure reported and nothing offered", status)
	}
	if atomic.LoadInt32(f.downloads) != 0 {
		t.Fatal("downloaded a version that already failed")
	}
}

func TestBrokenDownloadChangesNothing(t *testing.T) {
	f := newUpdaterFixture(t, "0.2.87", "0.2.88", []byte("#!/bin/sh\nexit 1\n")) // does not run
	err := f.updater.install(context.Background(), false)
	if err == nil || !strings.Contains(err.Error(), "stage update") {
		t.Fatalf("install = %v", err)
	}
	select {
	case <-f.handovers:
		t.Fatal("handed over to a binary that does not run")
	default:
	}
	if _, err := os.Stat(f.installed + selfupdate.StagedSuffix); !os.IsNotExist(err) {
		t.Fatal("staged file left behind")
	}
	if f.updater.Status().Updating {
		t.Fatal("a failed install must not stay 'updating'")
	}
}

func TestWorkersIdle(t *testing.T) {
	running := []*classifier.Worker{{State: classifier.StateBlocked}, {State: classifier.StateRunning}}
	quiet := []*classifier.Worker{{State: classifier.StateBlocked}, {State: classifier.StateDone}}
	if workersIdle(func() bool { return true }, func() []*classifier.Worker { return running }) {
		t.Fatal("idle with a running Worker")
	}
	if workersIdle(func() bool { return false }, func() []*classifier.Worker { return quiet }) {
		t.Fatal("idle before the first discovery pass")
	}
	if !workersIdle(func() bool { return true }, func() []*classifier.Worker { return quiet }) {
		t.Fatal("not idle with nothing running")
	}
}

func TestProbeUpdatedDaemonNeedsTheSameInstallationOnTheNewVersion(t *testing.T) {
	answer := func(id, version string) string {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, `{"status":"ok","daemon_id":%q,"version":%q}`, id, version)
		}))
		t.Cleanup(srv.Close)
		return strings.TrimPrefix(srv.URL, "http://")
	}
	if err := probeUpdatedDaemon(answer("d1", "0.2.88"), "d1", "0.2.88"); err != nil {
		t.Fatal(err)
	}
	if probeUpdatedDaemon(answer("d1", "0.2.87"), "d1", "0.2.88") == nil {
		t.Fatal("the old version answering must not pass the gate")
	}
	if probeUpdatedDaemon(answer("other", "0.2.88"), "d1", "0.2.88") == nil {
		t.Fatal("another installation answering must not pass the gate")
	}
	if probeUpdatedDaemon("127.0.0.1:1", "d1", "0.2.88") == nil {
		t.Fatal("nothing listening must not pass the gate")
	}
}

func TestAnIdleMachineThatBecomesBusyDuringTheDownloadIsLeftAlone(t *testing.T) {
	f := newUpdaterFixture(t, "0.2.87", "0.2.88", versionScript("0.2.88"))
	// Idle when the loop looks, busy when the staged download is re-checked,
	// idle again on the next look.
	var calls atomic.Int32
	f.updater.idle = func() bool { return calls.Add(1) != 2 }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go f.updater.Run(ctx)
	select {
	case h := <-f.handovers:
		if calls.Load() < 3 {
			t.Fatalf("handed over after only %d idle checks; the one after staging was skipped", calls.Load())
		}
		if _, err := os.Stat(h.Staged); err != nil {
			t.Fatalf("staged binary missing for the second attempt: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("never updated once idle again")
	}
	if atomic.LoadInt32(f.downloads) != 2 {
		t.Fatalf("downloads = %d, want the busy attempt and the idle one", atomic.LoadInt32(f.downloads))
	}
}

func TestOnlyOneInstallRunsAtATime(t *testing.T) {
	f := newUpdaterFixture(t, "0.2.87", "0.2.88", versionScript("0.2.88"))
	if err := f.updater.install(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	staged := f.installed + selfupdate.StagedSuffix
	if err := f.updater.install(context.Background(), true); err != errInProgress {
		t.Fatalf("second install = %v, want errInProgress", err)
	}
	if err := f.updater.install(context.Background(), false); err != errInProgress {
		t.Fatalf("button during an install = %v, want errInProgress", err)
	}
	if _, err := os.Stat(staged); err != nil {
		t.Fatalf("a second install must not disturb the staged binary: %v", err)
	}
	if len(f.handovers) != 1 || atomic.LoadInt32(f.downloads) != 1 {
		t.Fatalf("handovers %d, downloads %d; want one of each", len(f.handovers), atomic.LoadInt32(f.downloads))
	}
}

func TestAFailureThatIsNotTheVersionsFaultCoolsDownTheAutomaticPathOnly(t *testing.T) {
	f := newUpdaterFixture(t, "0.2.87", "0.2.88", versionScript("0.2.88"))
	failure := selfupdate.Failure{Version: "0.2.88", Reason: "back up databases: no space left", At: time.Now(), RetryAfter: time.Now().Add(time.Hour)}
	if err := selfupdate.RecordFailure(f.stateDir, failure); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go f.updater.Run(ctx)
	select {
	case <-f.handovers:
		t.Fatal("the automatic path retried inside the cooldown")
	case <-time.After(300 * time.Millisecond):
	}
	cancel()
	if status := f.updater.Status(); status.Latest != "0.2.88" || !strings.Contains(status.Error, "could not be installed") {
		t.Fatalf("status = %+v, want the release still offered with the reason", status)
	}
	// The button is the person asking again.
	if err := f.updater.Install(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-f.handovers:
	case <-time.After(5 * time.Second):
		t.Fatal("the button must retry a cooled-down version")
	}
}

func TestACooldownThatHasPassedIsRetriedAutomatically(t *testing.T) {
	f := newUpdaterFixture(t, "0.2.87", "0.2.88", versionScript("0.2.88"))
	failure := selfupdate.Failure{Version: "0.2.88", Reason: "disk full", At: time.Now().Add(-48 * time.Hour), RetryAfter: time.Now().Add(-24 * time.Hour)}
	if err := selfupdate.RecordFailure(f.stateDir, failure); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go f.updater.Run(ctx)
	select {
	case <-f.handovers:
	case <-time.After(5 * time.Second):
		t.Fatal("a version past its cooldown should be tried again")
	}
}

func TestAReadOnlyInstallDirectoryDoesNotOfferToUpdate(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := t.TempDir()
	if !installDirWritable(filepath.Join(dir, "mewla")) {
		t.Fatal("a writable directory was refused")
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o700)
	if installDirWritable(filepath.Join(dir, "mewla")) {
		t.Fatal("a read-only install directory must not offer to update")
	}
}
