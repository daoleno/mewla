package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDefaultVersionMatchesAppBase(t *testing.T) {
	t.Parallel()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// daemon/cmd/mewla -> repo root
	root := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
	basePath := filepath.Join(root, "app", "app.base.json")
	raw, err := os.ReadFile(basePath)
	if err != nil {
		t.Fatalf("read app.base.json: %v", err)
	}
	var doc struct {
		Expo struct {
			Version string `json:"version"`
			Android struct {
				Package     string `json:"package"`
				VersionCode int    `json:"versionCode"`
			} `json:"android"`
		} `json:"expo"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse app.base.json: %v", err)
	}
	if doc.Expo.Version != Version {
		t.Fatalf("Version %q != app.base.json expo.version %q", Version, doc.Expo.Version)
	}
	if doc.Expo.Android.Package != "com.daoleno.mewla" {
		t.Fatalf("unexpected package %q", doc.Expo.Android.Package)
	}
	if doc.Expo.Android.VersionCode < 1 {
		t.Fatalf("versionCode must be positive; got %d", doc.Expo.Android.VersionCode)
	}
}

func TestFormatVersionLine(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		version string
		commit  string
		want    string
	}{
		{"with commit", "0.2.13", "a1b2c3d", "mewla 0.2.13 (a1b2c3d, linux/amd64)"},
		{"dirty commit", "0.2.13", "a1b2c3d-dirty", "mewla 0.2.13 (a1b2c3d-dirty, linux/amd64)"},
		{"without commit", "0.2.13", "", "mewla 0.2.13 (linux/amd64)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := formatVersionLine(tc.version, tc.commit, "linux", "amd64"); got != tc.want {
				t.Fatalf("formatVersionLine() = %q, want %q", got, tc.want)
			}
		})
	}
}

// captureVersionStdout swaps os.Stdout for a pipe so the three version entry
// points can be exercised through run without touching the terminal.
func captureVersionStdout(t *testing.T, run func() error) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = writer
	runErr := run()
	os.Stdout = original
	if closeErr := writer.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	output, readErr := io.ReadAll(reader)
	if closeErr := reader.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if runErr != nil {
		t.Fatalf("run: %v", runErr)
	}
	if readErr != nil {
		t.Fatalf("read stdout: %v", readErr)
	}
	return string(output)
}

func TestVersionCommandEntryPoints(t *testing.T) {
	want := formatVersionLine(Version, vcsRevision(), runtime.GOOS, runtime.GOARCH) + "\n"
	for _, args := range [][]string{{"version"}, {"--version"}, {"-v"}} {
		got := captureVersionStdout(t, func() error {
			return run(args, io.Discard)
		})
		if got != want {
			t.Fatalf("run(%q) = %q, want %q", args, got, want)
		}
	}
}

func TestVersionCommandJSON(t *testing.T) {
	got := captureVersionStdout(t, func() error {
		return run([]string{"version", "--json"}, io.Discard)
	})
	if strings.Count(got, "\n") != 1 {
		t.Fatalf("version --json should print one line; got %q", got)
	}
	var info versionInfo
	if err := json.Unmarshal([]byte(got), &info); err != nil {
		t.Fatalf("parse version JSON %q: %v", got, err)
	}
	if info.Version != Version {
		t.Fatalf("version = %q, want %q", info.Version, Version)
	}
	if info.Commit != vcsRevision() {
		t.Fatalf("commit = %q, want %q", info.Commit, vcsRevision())
	}
	if info.OS != runtime.GOOS || info.Arch != runtime.GOARCH {
		t.Fatalf("platform = %s/%s, want %s/%s", info.OS, info.Arch, runtime.GOOS, runtime.GOARCH)
	}
	if info.Go != runtime.Version() {
		t.Fatalf("go = %q, want %q", info.Go, runtime.Version())
	}
}

func TestVCSRevisionShortForm(t *testing.T) {
	t.Parallel()

	revision := vcsRevision()
	if revision == "" {
		t.Skip("build has no VCS metadata")
	}
	base := strings.TrimSuffix(revision, "-dirty")
	if len(base) > 7 {
		t.Fatalf("revision %q is not shortened to 7 chars", revision)
	}
	if strings.Contains(strings.TrimSuffix(revision, "-dirty"), "-") {
		t.Fatalf("revision %q should be a single short commit", revision)
	}
}
