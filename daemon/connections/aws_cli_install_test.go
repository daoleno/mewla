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
