package selfupdate

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSemVersionPrecedenceSingleStream(t *testing.T) {
	ordered := []string{
		"0.1.0-beta.2",
		"0.1.0-beta.3",
		"0.1.0-beta.10",
		"0.1.0-rc.1",
		"0.1.0",
		"0.1.1-alpha.1",
	}
	for i := 1; i < len(ordered); i++ {
		left, err := parseSemVersion(ordered[i-1])
		if err != nil {
			t.Fatal(err)
		}
		right, err := parseSemVersion(ordered[i])
		if err != nil {
			t.Fatal(err)
		}
		if compareSemVersion(left, right) >= 0 {
			t.Fatalf("%s must precede %s", ordered[i-1], ordered[i])
		}
	}
}

func TestPlatformArtifactName(t *testing.T) {
	tests := map[string]string{
		"linux/amd64":  "mewla-linux-amd64.tar.gz",
		"linux/arm64":  "mewla-linux-arm64.tar.gz",
		"darwin/arm64": "mewla-darwin-arm64.tar.gz",
	}
	for platform, want := range tests {
		parts := strings.Split(platform, "/")
		got, err := PlatformArtifactName(parts[0], parts[1])
		if err != nil || got != want {
			t.Fatalf("PlatformArtifactName(%s) = %q, %v; want %q", platform, got, err, want)
		}
	}
	if _, err := PlatformArtifactName("windows", "amd64"); err == nil {
		t.Fatal("windows/amd64 unexpectedly supported")
	}
}

func TestVerifyManifestSignatureAndSchema(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"schema_version":2,"product":"mewla","version":"1.2.3-beta.1","artifacts":[]}`)
	signature := ed25519.Sign(privateKey, raw)
	manifest, err := VerifyManifest(raw, signature, publicKey)
	if err != nil || manifest.Version != "1.2.3-beta.1" {
		t.Fatalf("VerifyManifest() = %#v, %v", manifest, err)
	}
	tampered := append([]byte(nil), raw...)
	tampered[len(tampered)-2] ^= 1
	if _, err := VerifyManifest(tampered, signature, publicKey); err == nil {
		t.Fatal("tampered manifest verified")
	}
	signature[0] ^= 1
	if _, err := VerifyManifest(raw, signature, publicKey); err == nil {
		t.Fatal("tampered signature verified")
	}
	foreign := []byte(`{"schema_version":2,"product":"other","version":"1.2.3-beta.1","artifacts":[]}`)
	if _, err := VerifyManifest(foreign, ed25519.Sign(privateKey, foreign), publicKey); err == nil {
		t.Fatal("manifest for another product verified")
	}
}

// fakeGitHub serves releases the way github.com does: /releases/latest
// redirects to /releases/tag/<tag>, and files live under /releases/download.
func fakeGitHub(t *testing.T, latestLocation string, version string) (*httptest.Server, *[]string, ed25519.PublicKey) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	archive := []byte("archive")
	digest := sha256.Sum256(archive)
	manifest, err := json.Marshal(Manifest{
		SchemaVersion: 2,
		Product:       "mewla",
		Version:       version,
		Artifacts: []Artifact{{
			Path: "mewla-linux-amd64.tar.gz", Role: "daemon_archive", SHA256: hex.EncodeToString(digest[:]), Size: int64(len(archive)), GOOS: "linux", GOARCH: "amd64",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Host+r.URL.Path)
		download := "/daoleno/mewla/releases/download/v" + version + "/"
		switch r.URL.Path {
		case "/daoleno/mewla/releases/latest":
			http.Redirect(w, r, latestLocation, http.StatusFound)
		case "/daoleno/mewla/releases/tag/v" + version:
			t.Errorf("the release page was fetched; the redirect should not be followed")
		case download + ManifestAsset:
			_, _ = w.Write(manifest)
		case download + ManifestSignature:
			_, _ = w.Write(ed25519.Sign(privateKey, manifest))
		case download + "mewla-linux-amd64.tar.gz":
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server, &requests, publicKey
}

func TestLatestFollowsTheLatestReleaseRedirectWithoutTheAPI(t *testing.T) {
	server, requests, publicKey := fakeGitHub(t, "/daoleno/mewla/releases/tag/v0.2.17", "0.2.17")
	client := &Client{HTTPClient: server.Client(), ReleasesURL: server.URL + "/daoleno/mewla/releases", PublicKey: publicKey, GOOS: "linux", GOARCH: "amd64"}
	candidate, err := client.Latest(context.Background(), "0.2.16")
	if err != nil {
		t.Fatal(err)
	}
	if candidate == nil || candidate.Version != "0.2.17" || candidate.Tag != "v0.2.17" ||
		candidate.ArtifactURL != server.URL+"/daoleno/mewla/releases/download/v0.2.17/mewla-linux-amd64.tar.gz" {
		t.Fatalf("candidate = %#v", candidate)
	}
	if _, err := client.DownloadBinary(context.Background(), *candidate); err == nil || strings.Contains(err.Error(), "checksum") {
		// The fake archive passes its checksum and then fails to unpack.
		t.Fatalf("DownloadBinary() error = %v", err)
	}
	for _, request := range *requests {
		if strings.Contains(request, "api.github.com") || strings.Contains(request, "/repos/") {
			t.Fatalf("API request: %s", request)
		}
	}

	if candidate, err := client.Latest(context.Background(), "0.2.17"); err != nil || candidate != nil {
		t.Fatalf("same version: %#v, %v", candidate, err)
	}
	if candidate, err := client.Latest(context.Background(), "0.2.18-beta.1"); err != nil || candidate != nil {
		t.Fatalf("newer beta: %#v, %v", candidate, err)
	}
}

func TestLatestRejectsRedirectsWithoutAReleaseTag(t *testing.T) {
	for name, location := range map[string]string{
		"no release yet": "/daoleno/mewla/releases",
		"not semver":     "/daoleno/mewla/releases/tag/nightly",
		"no v prefix":    "/daoleno/mewla/releases/tag/0.2.17",
		"other host":     "https://example.com/daoleno/mewla/releases/tag/v0.2.17",
	} {
		server, _, publicKey := fakeGitHub(t, location, "0.2.17")
		client := &Client{HTTPClient: server.Client(), ReleasesURL: server.URL + "/daoleno/mewla/releases", PublicKey: publicKey, GOOS: "linux", GOARCH: "amd64"}
		if candidate, err := client.Latest(context.Background(), "0.2.16"); err == nil {
			t.Fatalf("%s: candidate %#v", name, candidate)
		}
	}

	server, _, _ := fakeGitHub(t, "/daoleno/mewla/releases/tag/v0.2.17", "0.2.17")
	_, otherKey, _ := ed25519.GenerateKey(rand.Reader)
	client := &Client{HTTPClient: server.Client(), ReleasesURL: server.URL + "/daoleno/mewla/releases", PublicKey: otherKey.Public().(ed25519.PublicKey), GOOS: "linux", GOARCH: "amd64"}
	if _, err := client.Latest(context.Background(), "0.2.16"); err == nil || !strings.Contains(err.Error(), "verify") {
		t.Fatalf("manifest signed by another key: %v", err)
	}
}

func TestDownloadBinaryRejectsArtifactChecksumMismatch(t *testing.T) {
	archive := []byte("not the authenticated archive")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer server.Close()
	client := &Client{HTTPClient: server.Client()}
	_, err := client.DownloadBinary(context.Background(), Candidate{
		ArtifactURL: server.URL,
		Artifact: Artifact{
			Path: "mewla-linux-amd64.tar.gz", Size: int64(len(archive)), SHA256: strings.Repeat("0", 64),
		},
	})
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("DownloadBinary() error = %v", err)
	}
}

func TestReplaceExecutableRenameFailurePreservesOriginal(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "mewla")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	originalRename := renameFile
	renameFile = func(string, string) error { return errors.New("injected rename failure") }
	t.Cleanup(func() { renameFile = originalRename })
	if err := ReplaceExecutable(target, []byte("new")); err == nil {
		t.Fatal("ReplaceExecutable unexpectedly succeeded")
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "old" {
		t.Fatalf("original executable changed to %q", raw)
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".mewla-update-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("temporary files remain: %v, %v", matches, err)
	}
}

func TestCacheFreshnessAndNotice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "update-check.json")
	now := time.Date(2026, 7, 14, 1, 2, 3, 0, time.UTC)
	if err := WriteCache(path, Cache{CheckedAt: now, LatestVersion: "0.1.0-beta.10"}); err != nil {
		t.Fatal(err)
	}
	cache, fresh := ReadCache(path, now.Add(CacheTTL-time.Second))
	if !fresh || cache.LatestVersion != "0.1.0-beta.10" {
		t.Fatalf("fresh cache = %#v, %v", cache, fresh)
	}
	if _, fresh := ReadCache(path, now.Add(CacheTTL+time.Second)); fresh {
		t.Fatal("expired cache reported fresh")
	}
	if got := NoticeLine("0.1.0-beta.3", "0.1.0-beta.10"); got != "Mewla 0.1.0-beta.10 is available; run: mewla update" {
		t.Fatalf("notice = %q", got)
	}
	if got := NoticeLine("0.1.0", "0.1.0-beta.10"); got != "" {
		t.Fatalf("downgrade notice = %q", got)
	}
}

func TestDaemonArchiveRoleIsSingleMewlaExecutable(t *testing.T) {
	name, err := PlatformArtifactName("linux", "amd64")
	if err != nil || name != "mewla-linux-amd64.tar.gz" {
		t.Fatalf("artifact=%q err=%v", name, err)
	}
	manifest := Manifest{
		SchemaVersion: 2,
		Product:       "mewla",
		Version:       "0.1.6",
		Artifacts: []Artifact{{
			Path: name, Role: "daemon_archive", SHA256: strings.Repeat("ab", 32), Size: 8, GOOS: "linux", GOARCH: "amd64",
		}},
	}
	got, ok := manifestArtifact(manifest, "linux", "amd64", name)
	if !ok || got.Role != "daemon_archive" || got.Path != name {
		t.Fatalf("manifest artifact %+v ok=%v", got, ok)
	}
}
