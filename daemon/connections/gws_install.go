package connections

import (
	"archive/tar"
	"compress/gzip"
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

type gwsArchive struct{ name, digest string }

// Pins are refreshed in the Mewla release checklist, like the gcloud bootstrap.
const gwsVersion = "0.22.5"

var gwsArchives = map[string]gwsArchive{
	"linux/amd64":  {"x86_64-unknown-linux-musl", "4db473dde4b1ab872e4ff35d769b0d4af1f1a6441a605e79d5cf8ada9c87e920"},
	"linux/arm64":  {"aarch64-unknown-linux-musl", "e700fe63524932b10ec2130b47ece90aa850e66005fe52ccfc4cf8767bf9919a"},
	"darwin/amd64": {"x86_64-apple-darwin", "51f9bd731404d4bba26c36e2e30dd68c56dccd1f834c01252cb0b14d6a6544b2"},
	"darwin/arm64": {"aarch64-apple-darwin", "1d2a9ffd5bc9b2c2c4b48630daf082fad13d9e57d741988a2c248eed562f7dac"},
}

const gwsInstallDocs = "https://github.com/googleworkspace/cli#installation"
const gwsReleaseBase = "https://github.com/googleworkspace/cli/releases/download/v" + gwsVersion + "/"

func gwsInstallSupport(platform string) (string, string) {
	if _, ok := gwsArchives[platform]; ok {
		return "", ""
	}
	return "Install the Google Workspace CLI (gws), then check again.", gwsInstallDocs
}

var gwsInstallMu sync.Mutex

func (gwsCLI) install(ctx context.Context, dir string) error {
	pkg, ok := gwsArchives[runtime.GOOS+"/"+runtime.GOARCH]
	if !ok {
		message, _ := gwsInstallSupport(runtime.GOOS + "/" + runtime.GOARCH)
		return errors.New(message)
	}
	return installGWSArchive(ctx, dir, pkg, gwsReleaseBase, &http.Client{Timeout: 90 * time.Second}, runCloudProcess)
}

// installGWSArchive installs the pinned official release as dir/bin/gws. The
// download is checked against the pinned digest, extracted in-process (only the
// gws executable is read from the archive) and verified before it replaces the
// launcher, so an interrupted install leaves the previous one in place.
func installGWSArchive(ctx context.Context, dir string, pkg gwsArchive, base string, client *http.Client, run func(context.Context, string, []string, []string) (cloudOutput, error)) error {
	if !gwsInstallMu.TryLock() {
		return errors.New("The Google Workspace CLI is already installing. Try again shortly.")
	}
	defer gwsInstallMu.Unlock()
	failed := func() error { return errors.New("Couldn't finish installing the Google Workspace CLI. Retry.") }
	bin := filepath.Join(dir, "bin")
	launcher := filepath.Join(bin, "gws")
	env := (gwsCLI{}).environment()
	ready := func(path string) bool {
		probe, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		v, err := cloudExecutableVersion(probe, "gws", path, run, env)
		return err == nil && v == gwsVersion
	}
	if info, err := os.Stat(launcher); err == nil && info.Mode().IsRegular() && info.Mode()&0111 != 0 && ready(launcher) {
		return nil
	}
	if err := os.MkdirAll(bin, 0700); err != nil {
		return failed()
	}
	scratch, err := os.MkdirTemp(dir, "install-")
	if err != nil {
		return failed()
	}
	defer os.RemoveAll(scratch)
	req, _ := http.NewRequestWithContext(ctx, "GET", base+"google-workspace-cli-"+pkg.name+".tar.gz", nil)
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("Couldn't download the Google Workspace CLI. Check your connection and retry.")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("Couldn't download the Google Workspace CLI. Check your connection and retry.")
	}
	archive := filepath.Join(scratch, "gws.tar.gz")
	file, err := os.OpenFile(archive, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return failed()
	}
	digest := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(file, digest), io.LimitReader(resp.Body, 64<<20))
	closeErr := file.Close()
	if copyErr != nil {
		return errors.New("The Google Workspace CLI download was interrupted. Retry.")
	}
	if closeErr != nil {
		return failed()
	}
	if size >= 64<<20 || hex.EncodeToString(digest.Sum(nil)) != pkg.digest {
		return errors.New("The Google Workspace CLI download failed verification. Retry.")
	}
	staged := filepath.Join(scratch, "gws")
	if err := extractGWS(archive, staged); err != nil || !ready(staged) {
		return failed()
	}
	next := filepath.Join(bin, ".gws-new")
	_ = os.Remove(next)
	if err := os.Rename(staged, next); err != nil {
		return failed()
	}
	if err := os.Rename(next, launcher); err != nil {
		_ = os.Remove(next)
		return failed()
	}
	return nil
}

// extractGWS writes the archive's top-level "gws" file and nothing else.
func extractGWS(archive, dest string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer zr.Close()
	tr := tar.NewReader(zr)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return errors.New("gws is not in the archive")
		}
		if err != nil {
			return err
		}
		name := filepath.Clean(h.Name)
		if h.Typeflag != tar.TypeReg || (name != "gws" && name != filepath.Join(".", "gws")) {
			continue
		}
		if h.Size <= 0 || h.Size > 64<<20 {
			return errors.New("unexpected gws size")
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
		if err != nil {
			return err
		}
		n, err := io.Copy(out, io.LimitReader(tr, h.Size))
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil || n != h.Size {
			return errors.New("could not extract gws")
		}
		return nil
	}
}
