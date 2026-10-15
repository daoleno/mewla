package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/atomicfile"
)

const (
	// DefaultReleasesURL is where releases are published. Mewla finds the
	// newest stable one from its /latest redirect, as install.sh does, so
	// updates need no GitHub API call and no API rate limit.
	DefaultReleasesURL = "https://github.com/daoleno/mewla/releases"
	ManifestAsset      = "release-manifest.json"
	// ArchiveBinary is the executable inside mewla-<os>-<arch>.tar.gz.
	ArchiveBinary       = "mewla"
	ManifestSignature   = "release-manifest.json.sig"
	maxManifestSize     = 1 << 20
	maxSignatureSize    = 1 << 10
	maxDaemonArchive    = 256 << 20
	CacheTTL            = 24 * time.Hour
	UpdatePublicKeyPKIX = "MCowBQYDK2VwAyEAD+GvALNRsWlmuB8FL5nwWchsLuXI7tasTGYSqkdabzw="
)

type Artifact struct {
	Path   string `json:"path"`
	Role   string `json:"role"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	GOOS   string `json:"goos,omitempty"`
	GOARCH string `json:"goarch,omitempty"`
}

type Manifest struct {
	SchemaVersion int        `json:"schema_version"`
	Product       string     `json:"product"`
	Version       string     `json:"version"`
	Artifacts     []Artifact `json:"artifacts"`
}

type Candidate struct {
	Version     string
	Tag         string
	Artifact    Artifact
	ArtifactURL string
}

type Client struct {
	HTTPClient  *http.Client
	ReleasesURL string
	PublicKey   ed25519.PublicKey
	GOOS        string
	GOARCH      string
}

func NewClient() (*Client, error) {
	der, err := base64.StdEncoding.DecodeString(UpdatePublicKeyPKIX)
	if err != nil {
		return nil, fmt.Errorf("decode update public key: %w", err)
	}
	parsed, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, fmt.Errorf("parse update public key: %w", err)
	}
	key, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("update public key is not Ed25519")
	}
	return &Client{
		HTTPClient:  &http.Client{Timeout: 15 * time.Second},
		ReleasesURL: DefaultReleasesURL,
		PublicKey:   key,
		GOOS:        runtime.GOOS,
		GOARCH:      runtime.GOARCH,
	}, nil
}

func PlatformArtifactName(goos, goarch string) (string, error) {
	switch goos + "/" + goarch {
	case "linux/amd64":
		return "mewla-linux-amd64.tar.gz", nil
	case "linux/arm64":
		return "mewla-linux-arm64.tar.gz", nil
	case "darwin/arm64":
		return "mewla-darwin-arm64.tar.gz", nil
	default:
		return "", fmt.Errorf("self-update is not supported on %s/%s", goos, goarch)
	}
}

// Latest returns the newest stable release when it is newer than current,
// with its archive checked against the signed release manifest, or nil.
func (c *Client) Latest(ctx context.Context, current string) (*Candidate, error) {
	wantName, err := PlatformArtifactName(c.GOOS, c.GOARCH)
	if err != nil {
		return nil, err
	}
	tag, version, err := c.latestTag(ctx)
	if err != nil {
		return nil, err
	}
	if currentVersion, err := parseSemVersion(strings.TrimPrefix(current, "v")); err == nil &&
		compareSemVersion(version, currentVersion) <= 0 {
		return nil, nil
	}
	download := strings.TrimRight(c.ReleasesURL, "/") + "/download/" + tag + "/"
	manifestBytes, err := c.download(ctx, download+ManifestAsset, maxManifestSize)
	if err != nil {
		return nil, fmt.Errorf("download %s %s: %w", tag, ManifestAsset, err)
	}
	signature, err := c.download(ctx, download+ManifestSignature, maxSignatureSize)
	if err != nil {
		return nil, fmt.Errorf("download %s %s: %w", tag, ManifestSignature, err)
	}
	manifest, err := VerifyManifest(manifestBytes, signature, c.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("verify %s manifest: %w", tag, err)
	}
	if manifest.Version != version.String() {
		return nil, fmt.Errorf("%s manifest is for version %s", tag, manifest.Version)
	}
	artifact, ok := manifestArtifact(manifest, c.GOOS, c.GOARCH, wantName)
	if !ok {
		return nil, fmt.Errorf("%s has no %s", tag, wantName)
	}
	return &Candidate{
		Version:     manifest.Version,
		Tag:         tag,
		Artifact:    artifact,
		ArtifactURL: download + wantName,
	}, nil
}

// latestTag reads the tag from the releases/latest redirect without
// following it, and accepts only a v-prefixed semantic version.
func (c *Client) latestTag(ctx context.Context) (string, semVersion, error) {
	base, err := url.Parse(strings.TrimRight(c.ReleasesURL, "/"))
	if err != nil {
		return "", semVersion{}, fmt.Errorf("releases URL: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String()+"/latest", nil)
	if err != nil {
		return "", semVersion{}, err
	}
	req.Header.Set("User-Agent", "mewla-self-update")
	client := *c.HTTPClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return "", semVersion{}, fmt.Errorf("find the latest release: %w", err)
	}
	_ = resp.Body.Close()
	location, err := resp.Location()
	if err != nil || resp.StatusCode < 300 || resp.StatusCode > 399 {
		return "", semVersion{}, fmt.Errorf("find the latest release: HTTP %s without a release redirect", resp.Status)
	}
	tag, ok := strings.CutPrefix(location.Path, base.Path+"/tag/")
	if !ok || location.Host != base.Host {
		return "", semVersion{}, errors.New("find the latest release: no release is published yet")
	}
	version, err := parseSemVersion(strings.TrimPrefix(tag, "v"))
	if err != nil || tag != "v"+version.String() {
		return "", semVersion{}, fmt.Errorf("find the latest release: unexpected tag %q", tag)
	}
	return tag, version, nil
}

func (c *Client) DownloadBinary(ctx context.Context, candidate Candidate) ([]byte, error) {
	if candidate.Artifact.Size <= 0 || candidate.Artifact.Size > maxDaemonArchive {
		return nil, fmt.Errorf("invalid authenticated archive size %d", candidate.Artifact.Size)
	}
	archive, err := c.download(ctx, candidate.ArtifactURL, candidate.Artifact.Size+1)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", candidate.Artifact.Path, err)
	}
	if int64(len(archive)) != candidate.Artifact.Size {
		return nil, fmt.Errorf("archive size mismatch: got %d want %d", len(archive), candidate.Artifact.Size)
	}
	digest := sha256.Sum256(archive)
	if !strings.EqualFold(hex.EncodeToString(digest[:]), candidate.Artifact.SHA256) {
		return nil, errors.New("archive checksum mismatch")
	}
	binary, err := extractBinary(archive)
	if err != nil {
		return nil, err
	}
	return binary, nil
}

func (c *Client) download(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/octet-stream")
	req.Header.Set("User-Agent", "mewla-self-update")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response exceeds %d bytes", limit)
	}
	return data, nil
}

func ParseManifest(raw []byte) (Manifest, error) {
	var manifest Manifest
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	if err := decoder.Decode(&manifest); err != nil {
		return manifest, fmt.Errorf("parse signed release manifest: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return manifest, errors.New("signed release manifest has trailing data")
	}
	if manifest.SchemaVersion != 2 || manifest.Product != "mewla" {
		return manifest, errors.New("unsupported signed release manifest")
	}
	if _, err := parseSemVersion(manifest.Version); err != nil {
		return manifest, fmt.Errorf("invalid manifest version: %w", err)
	}
	return manifest, nil
}

func VerifyManifest(raw, signature []byte, publicKey ed25519.PublicKey) (Manifest, error) {
	if len(publicKey) != ed25519.PublicKeySize || len(signature) != ed25519.SignatureSize || !ed25519.Verify(publicKey, raw, signature) {
		return Manifest{}, errors.New("release manifest signature verification failed")
	}
	return ParseManifest(raw)
}

func manifestArtifact(manifest Manifest, goos, goarch, name string) (Artifact, bool) {
	for _, artifact := range manifest.Artifacts {
		if artifact.Path == name && artifact.Role == "daemon_archive" && artifact.GOOS == goos && artifact.GOARCH == goarch && artifact.Size > 0 && len(artifact.SHA256) == sha256.Size*2 {
			if _, err := hex.DecodeString(artifact.SHA256); err == nil {
				return artifact, true
			}
		}
	}
	return Artifact{}, false
}

func extractBinary(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("open daemon archive: %w", err)
	}
	defer gz.Close()
	tarReader := tar.NewReader(gz)
	var binary []byte
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read daemon archive: %w", err)
		}
		if strings.TrimPrefix(filepath.Clean(header.Name), "./") != ArchiveBinary {
			continue
		}
		if header.Typeflag != tar.TypeReg || header.Size <= 0 || header.Size > maxDaemonArchive {
			return nil, errors.New("daemon archive has invalid mewla entry")
		}
		if binary != nil {
			return nil, errors.New("daemon archive has duplicate mewla entries")
		}
		binary, err = io.ReadAll(io.LimitReader(tarReader, header.Size+1))
		if err != nil || int64(len(binary)) != header.Size {
			return nil, errors.New("read mewla binary from archive")
		}
	}
	if len(binary) == 0 {
		return nil, errors.New("daemon archive does not contain mewla")
	}
	return binary, nil
}

var renameFile = os.Rename

func ReplaceExecutable(executable string, binary []byte) error {
	target, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	info, err := os.Stat(target)
	if err != nil {
		return fmt.Errorf("stat executable: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(target), ".mewla-update-*")
	if err != nil {
		return fmt.Errorf("create update beside executable: %w", err)
	}
	tempPath := temp.Name()
	keep := false
	defer func() {
		_ = temp.Close()
		if !keep {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(info.Mode().Perm() | 0o100); err != nil {
		return fmt.Errorf("set update permissions: %w", err)
	}
	if _, err := temp.Write(binary); err != nil {
		return fmt.Errorf("write update: %w", err)
	}
	if err := temp.Sync(); err != nil {
		return fmt.Errorf("sync update: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close update: %w", err)
	}
	if err := renameFile(tempPath, target); err != nil {
		return fmt.Errorf("replace executable: %w", err)
	}
	keep = true
	if dir, err := os.Open(filepath.Dir(target)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

type Cache struct {
	CheckedAt     time.Time `json:"checked_at"`
	LatestVersion string    `json:"latest_version,omitempty"`
}

func ReadCache(path string, now time.Time) (Cache, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Cache{}, false
	}
	var cache Cache
	if json.Unmarshal(raw, &cache) != nil || cache.CheckedAt.After(now.Add(time.Minute)) || now.Sub(cache.CheckedAt) > CacheTTL {
		return Cache{}, false
	}
	if cache.LatestVersion != "" {
		if _, err := parseSemVersion(cache.LatestVersion); err != nil {
			return Cache{}, false
		}
	}
	return cache, true
}

func WriteCache(path string, cache Cache) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(cache)
	if err != nil {
		return err
	}
	return atomicfile.Write(path, append(raw, '\n'), 0o600)
}

func NoticeLine(current, latest string) string {
	currentVersion, err := parseSemVersion(current)
	if err != nil || latest == "" {
		return ""
	}
	latestVersion, err := parseSemVersion(latest)
	if err != nil || compareSemVersion(latestVersion, currentVersion) <= 0 {
		return ""
	}
	return fmt.Sprintf("Mewla %s is available; run: mewla update", latest)
}

type semVersion struct {
	major, minor, patch uint64
	prerelease          []string
}

func (v semVersion) String() string {
	result := fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch)
	if len(v.prerelease) > 0 {
		result += "-" + strings.Join(v.prerelease, ".")
	}
	return result
}

func parseSemVersion(raw string) (semVersion, error) {
	var result semVersion
	if strings.Contains(raw, "+") {
		raw = strings.SplitN(raw, "+", 2)[0]
	}
	core := raw
	if before, after, ok := strings.Cut(raw, "-"); ok {
		core = before
		if after == "" {
			return result, errors.New("empty prerelease")
		}
		result.prerelease = strings.Split(after, ".")
		for _, part := range result.prerelease {
			if part == "" || !isSemVersionIdentifier(part) || (len(part) > 1 && part[0] == '0' && isNumeric(part)) {
				return result, errors.New("invalid prerelease")
			}
		}
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return result, errors.New("version must have major.minor.patch")
	}
	values := []*uint64{&result.major, &result.minor, &result.patch}
	for i, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') || !isNumeric(part) {
			return result, errors.New("invalid numeric version")
		}
		value, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return result, err
		}
		*values[i] = value
	}
	return result, nil
}

func compareSemVersion(a, b semVersion) int {
	for _, pair := range [][2]uint64{{a.major, b.major}, {a.minor, b.minor}, {a.patch, b.patch}} {
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
	}
	if len(a.prerelease) == 0 && len(b.prerelease) == 0 {
		return 0
	}
	if len(a.prerelease) == 0 {
		return 1
	}
	if len(b.prerelease) == 0 {
		return -1
	}
	for i := 0; i < len(a.prerelease) && i < len(b.prerelease); i++ {
		left, right := a.prerelease[i], b.prerelease[i]
		if left == right {
			continue
		}
		leftNumeric, rightNumeric := isNumeric(left), isNumeric(right)
		switch {
		case leftNumeric && rightNumeric:
			if len(left) < len(right) || (len(left) == len(right) && left < right) {
				return -1
			}
			return 1
		case leftNumeric:
			return -1
		case rightNumeric:
			return 1
		case left < right:
			return -1
		default:
			return 1
		}
	}
	if len(a.prerelease) < len(b.prerelease) {
		return -1
	}
	if len(a.prerelease) > len(b.prerelease) {
		return 1
	}
	return 0
}

func isNumeric(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func isSemVersionIdentifier(value string) bool {
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'A' || char > 'Z') && (char < 'a' || char > 'z') && char != '-' {
			return false
		}
	}
	return true
}
