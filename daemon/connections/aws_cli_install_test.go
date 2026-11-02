package connections

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type awsRoundTrip func(*http.Request) (*http.Response, error)

func (f awsRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestAWSInstallFailuresLeaveNoGeneration(t *testing.T) {
	for _, mode := range []string{"offline", "corrupt", "partial"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			a := newAWSInstaller()
			a.goos = "linux"
			a.arch = "amd64"
			a.client = &http.Client{Transport: awsRoundTrip(func(r *http.Request) (*http.Response, error) {
				if mode == "offline" {
					return nil, errors.New("blocked")
				}
				length := int64(3)
				if mode == "partial" {
					length = 100
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("bad")), ContentLength: length, Header: http.Header{}}, nil
			})}
			if err := a.install(context.Background(), root); err == nil {
				t.Fatal("bad download accepted")
			}
			entries, _ := os.ReadDir(filepath.Join(root, "versions"))
			if len(entries) != 0 {
				t.Fatal("half-installed generation remains")
			}
			if _, err := os.Lstat(filepath.Join(root, "bin", "aws")); !os.IsNotExist(err) {
				t.Fatal("bad installation activated")
			}
		})
	}
}
func TestAWSArchiveRejectsTraversalAndSymlinks(t *testing.T) {
	for _, name := range []string{"../escaped", "aws/../../escaped", "/aws/file", "aws/link"} {
		var b bytes.Buffer
		w := zip.NewWriter(&b)
		h := &zip.FileHeader{Name: name}
		if name == "aws/link" {
			h.SetMode(os.ModeSymlink | 0777)
		}
		entry, _ := w.CreateHeader(h)
		entry.Write([]byte("bad"))
		w.Close()
		dir := t.TempDir()
		archive := filepath.Join(dir, "archive.zip")
		os.WriteFile(archive, b.Bytes(), 0600)
		if err := unpackAWSArchive(archive, filepath.Join(dir, "out")); err == nil {
			t.Fatalf("unsafe archive accepted %q", name)
		}
	}
}
func TestAWSExecutableAndUnsupportedInstall(t *testing.T) {
	if err := validateAWSExecutable(context.Background(), "/bin/true"); err == nil {
		t.Fatal("empty executable accepted")
	}
	a := newAWSInstaller()
	a.goos = "windows"
	a.arch = "amd64"
	root := t.TempDir()
	err := a.install(context.Background(), root)
	if err == nil || !strings.Contains(err.Error(), "Install AWS CLI v2 for Windows") || !strings.Contains(err.Error(), "Check again") {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Fatal("unsupported installation mutated disk")
	}
}

func TestAWSUpdateFailureRetainsActiveAndOlderGenerations(t *testing.T) {
	for _, fault := range []string{"signature", "version", "smoke"} {
		t.Run(fault, func(t *testing.T) {
			dir := t.TempDir()
			old := fixtureAWS(t, filepath.Join(dir, "versions", "install-active"), "2.36.20")
			older := fixtureAWS(t, filepath.Join(dir, "versions", "install-running"), "2.35.0")
			activateFixture(t, dir, old)
			a := newAWSInstaller()
			a.goos = "darwin"
			a.arch = "arm64"
			if fault == "signature" {
				a.goos = "linux"
			}
			a.client = &http.Client{Transport: awsRoundTrip(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("injected bad package")), ContentLength: -1, Header: http.Header{}}, nil
			})}
			a.run = func(_ context.Context, binary string, args, env []string) (cloudOutput, error) {
				switch filepath.Base(binary) {
				case "pkgutil":
					return cloudOutput{Stdout: "Developer ID Installer: Amazon Web Services (94KV3E626L)"}, nil
				case "installer":
					return cloudOutput{}, nil
				case "aws":
					if len(args) == 1 && args[0] == "--version" {
						if fault == "version" {
							return cloudOutput{Stdout: "broken"}, nil
						}
						return cloudOutput{Stdout: "aws-cli/2.39.3"}, nil
					}
					return cloudOutput{ExitCode: 1}, nil
				}
				t.Fatal("unexpected process", binary)
				return cloudOutput{}, nil
			}
			if err := a.install(context.Background(), dir); err == nil {
				t.Fatal("bad update accepted")
			}
			active, _ := filepath.EvalSymlinks(filepath.Join(dir, "bin", "aws"))
			if active != old {
				t.Fatal("bad update replaced active generation")
			}
			if _, err := os.Stat(older); err != nil {
				t.Fatal("update deleted a running old generation")
			}
			entries, _ := os.ReadDir(filepath.Join(dir, "versions"))
			if len(entries) != 2 {
				t.Fatal("failed staging leaked", entries)
			}
		})
	}
}

func TestAWSETagRequiresMatchingInstalledVersion(t *testing.T) {
	for _, version := range []string{"2.39.3", "2.36.20"} {
		t.Run(version, func(t *testing.T) {
			dir := t.TempDir()
			activateFixture(t, dir, fixtureAWS(t, filepath.Join(dir, "versions", "existing"), version))
			os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"etag":"official-current","version":"2.39.3"}`), 0600)
			a := newAWSInstaller()
			a.goos = "linux"
			a.arch = "amd64"
			downloads := 0
			a.client = &http.Client{Transport: awsRoundTrip(func(r *http.Request) (*http.Response, error) {
				if r.Method == http.MethodHead {
					return &http.Response{StatusCode: 200, Header: http.Header{"Etag": []string{"official-current"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
				}
				downloads++
				return nil, errors.New("injected blocked package")
			})}
			err := a.install(context.Background(), dir)
			if version == "2.39.3" && (err != nil || downloads != 0) {
				t.Fatal("unchanged official package downloaded again", err)
			}
			if version == "2.36.20" && (err == nil || downloads != 1) {
				t.Fatal("old binary mistaken for current despite matching ETag", err)
			}
		})
	}
}
