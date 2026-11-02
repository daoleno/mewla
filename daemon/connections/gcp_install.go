package connections

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

type gcpArchive struct{ name, digest string }

var gcpArchives = map[string]gcpArchive{
	"linux/amd64":  {"linux-x86_64", "78c570f5fcfe58a9311bd98c06b75530595e3cae6277aad75a676a39e6ee7b63"},
	"linux/arm64":  {"linux-arm", "f5e5df0433f77c0e7f5df9fd4bce2e46c7b7967e79cf66e31b1164d3d4c86546"},
	"darwin/amd64": {"darwin-x86_64", "89c3ca942eb609293dff5b549ec6bae5d9ec41633a132aeb8dd8087a915ab3a0"},
	"darwin/arm64": {"darwin-arm", "85cb726919838faff48bfba7cf676b691b0202bec7c25aabdc116649c6843242"},
}

const gcpInstallDocs = "https://docs.cloud.google.com/sdk/docs/install-sdk"

func gcpInstallSupport(platform string) (string, string) {
	if _, ok := gcpArchives[platform]; ok {
		return "", ""
	}
	return "Install Google Cloud CLI (gcloud) in ~/google-cloud-sdk, then check again.", gcpInstallDocs
}
func (gcloudCLI) installSupport() (string, string) {
	return gcpInstallSupport(runtime.GOOS + "/" + runtime.GOARCH)
}
func (gcloudCLI) installPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(home, "google-cloud-sdk", "bin", "gcloud"), filepath.Join(home, ".local", "bin", "gcloud")}
}

// A daemon owns its state directory, and only one gcloud install may mutate its
// managed SDK at a time. Cancellation releases the mutex after cleanup.
var gcpInstallMu sync.Mutex

func (gcloudCLI) install(ctx context.Context, dir string) error {
	pkg, ok := gcpArchives[runtime.GOOS+"/"+runtime.GOARCH]
	if !ok {
		message, _ := gcpInstallSupport(runtime.GOOS + "/" + runtime.GOARCH)
		return errors.New(message)
	}
	return installGCPArchive(ctx, dir, pkg, &http.Client{Timeout: 90 * time.Second}, runCloudProcess)
}

// Network and process boundaries are injectable for fault tests; the real
// provider always supplies the pinned official package and normal runner.
func installGCPArchive(ctx context.Context, dir string, pkg gcpArchive, client *http.Client, run func(context.Context, string, []string, []string) (cloudOutput, error)) error {
	if !gcpInstallMu.TryLock() {
		return errors.New("gcloud is already installing. Try again shortly.")
	}
	defer gcpInstallMu.Unlock()
	failed := func() error { return errors.New("Couldn't finish installing gcloud. Retry.") }
	if err := os.MkdirAll(dir, 0700); err != nil {
		return failed()
	}
	// Only our private staging directories are removed, including a previous
	// process interrupted before it could run its defer. Never touch CLI config.
	stale, _ := filepath.Glob(filepath.Join(dir, "install-*"))
	for _, path := range stale {
		if err := os.RemoveAll(path); err != nil {
			return failed()
		}
	}
	sdk := filepath.Join(dir, "google-cloud-sdk")
	env := (gcloudCLI{}).environment()
	ready := func(path string) bool {
		if info, err := os.Stat(filepath.Join(path, "bin", "gcloud")); err != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
			return false
		}
		probe, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		out, err := run(probe, filepath.Join(path, "bin", "gcloud"), []string{"version", "--format=json"}, env)
		return err == nil && out.ExitCode == 0 && len(out.Stdout) > 0
	}
	publish := func() error {
		bin := filepath.Join(dir, "bin")
		if err := os.MkdirAll(bin, 0700); err != nil {
			return err
		}
		temp := filepath.Join(bin, ".gcloud-new")
		_ = os.Remove(temp)
		if err := os.Symlink(filepath.Join(sdk, "bin", "gcloud"), temp); err != nil {
			return err
		}
		defer os.Remove(temp)
		return os.Rename(temp, filepath.Join(bin, "gcloud"))
	}
	// A crash after rename but before symlink publication needs no download.
	if ready(sdk) {
		if err := publish(); err != nil {
			return failed()
		}
		return nil
	}
	if err := os.RemoveAll(sdk); err != nil {
		return failed()
	}
	// Remove only our broken launcher. The SDK is unusable and not published.
	if err := os.Remove(filepath.Join(dir, "bin", "gcloud")); err != nil && !os.IsNotExist(err) {
		return failed()
	}
	scratch, err := os.MkdirTemp(dir, "install-")
	if err != nil {
		return failed()
	}
	defer os.RemoveAll(scratch)
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://dl.google.com/dl/cloudsdk/channels/rapid/downloads/google-cloud-cli-591.0.0-"+pkg.name+".tar.gz", nil)
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("Couldn't download gcloud. Check your connection and retry.")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("Couldn't download gcloud. Check your connection and retry.")
	}
	archive := filepath.Join(scratch, "sdk.tar.gz")
	file, err := os.OpenFile(archive, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return failed()
	}
	digest := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(file, digest), io.LimitReader(resp.Body, 256<<20))
	closeErr := file.Close()
	if copyErr != nil {
		return errors.New("The gcloud download was interrupted. Retry.")
	}
	if closeErr != nil {
		return failed()
	}
	if size >= 256<<20 || hex.EncodeToString(digest.Sum(nil)) != pkg.digest {
		return errors.New("The gcloud download failed verification. Retry.")
	}
	out, err := run(ctx, "tar", []string{"-xzf", archive, "-C", scratch, "--no-same-owner"}, env)
	if err != nil || out.ExitCode != 0 {
		return failed()
	}
	staged := filepath.Join(scratch, "google-cloud-sdk")
	out, err = run(ctx, filepath.Join(staged, "install.sh"), []string{"--quiet", "--usage-reporting=false", "--path-update=false", "--command-completion=false", "--install-python=false", "--no-compile-python"}, env)
	if err != nil || out.ExitCode != 0 {
		return failed()
	}
	if !ready(staged) {
		return failed()
	}
	if err = os.Rename(staged, sdk); err != nil {
		return failed()
	}
	// No executable is discoverable by Mewla until installation and verification
	// finish. A failed publication is rolled back; retries repair interrupted runs.
	if err = publish(); err != nil {
		os.RemoveAll(sdk)
		return failed()
	}
	return nil
}
