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
)

// Pin the vendor archive so an upstream release cannot silently change the
// installer. The digest is of Google's HTTPS versioned archive, not a mirror.
func (gcloudCLI) install(ctx context.Context, dir string) error {
	archives := map[string]struct{ name, digest string }{
		"linux/amd64":  {"linux-x86_64", "78c570f5fcfe58a9311bd98c06b75530595e3cae6277aad75a676a39e6ee7b63"},
		"linux/arm64":  {"linux-arm", "f5e5df0433f77c0e7f5df9fd4bce2e46c7b7967e79cf66e31b1164d3d4c86546"},
		"darwin/amd64": {"darwin-x86_64", "89c3ca942eb609293dff5b549ec6bae5d9ec41633a132aeb8dd8087a915ab3a0"},
		"darwin/arm64": {"darwin-arm", "85cb726919838faff48bfba7cf676b691b0202bec7c25aabdc116649c6843242"},
	}
	pkg, ok := archives[runtime.GOOS+"/"+runtime.GOARCH]
	if !ok {
		return errors.New("Install Google Cloud CLI on this computer, then connect again. Automatic installation supports Linux and macOS on x86_64 and Arm64.")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	scratch, err := os.MkdirTemp(dir, "install-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch)
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://dl.google.com/dl/cloudsdk/channels/rapid/downloads/google-cloud-cli-591.0.0-"+pkg.name+".tar.gz", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.New("Google Cloud CLI download failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return errors.New("Google Cloud CLI download failed")
	}
	archive := filepath.Join(scratch, "sdk.tar.gz")
	file, err := os.OpenFile(archive, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	digest := sha256.New()
	size, err := io.Copy(io.MultiWriter(file, digest), io.LimitReader(resp.Body, 256<<20))
	closeErr := file.Close()
	if err != nil || closeErr != nil || size >= 256<<20 || hex.EncodeToString(digest.Sum(nil)) != pkg.digest {
		return errors.New("Google Cloud CLI download verification failed")
	}
	env := (gcloudCLI{}).environment()
	out, err := runCloudProcess(ctx, "tar", []string{"-xzf", archive, "-C", scratch, "--no-same-owner"}, env)
	if err != nil || out.ExitCode != 0 {
		return errors.New("Google Cloud CLI extraction failed")
	}
	sdk := filepath.Join(dir, "google-cloud-sdk")
	if _, err = os.Stat(sdk); err == nil {
		return errors.New("A managed gcloud installation already exists; check it before reinstalling")
	}
	if err = os.Rename(filepath.Join(scratch, "google-cloud-sdk"), sdk); err != nil {
		return err
	}
	success := false
	defer func() {
		if !success {
			os.RemoveAll(sdk)
		}
	}()
	out, err = runCloudProcess(ctx, filepath.Join(sdk, "install.sh"), []string{"--quiet", "--usage-reporting=false", "--path-update=false", "--command-completion=false", "--install-python=false", "--no-compile-python"}, env)
	if err != nil || out.ExitCode != 0 {
		return errors.New("Google Cloud CLI installation failed")
	}
	if err = os.MkdirAll(filepath.Join(dir, "bin"), 0700); err != nil {
		return err
	}
	if err = os.Symlink(filepath.Join(sdk, "bin", "gcloud"), filepath.Join(dir, "bin", "gcloud")); err != nil {
		return err
	}
	success = true
	return nil
}
