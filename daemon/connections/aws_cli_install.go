package connections

import (
	"archive/zip"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/openpgp"
)

// Official AWS CLI signing key, fingerprint
// FB5D B77F D5C1 18B8 0511 ADA8 A631 0ACC 4672 475C.
// https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html
//
//go:embed aws_cli_signing_key.asc
var awsCLISigningKey string

var errAWSDownload = errors.New("Couldn't download AWS CLI. Check this computer's connection and retry.")
var errAWSIncomplete = errors.New("AWS CLI download was incomplete or damaged. Retry.")
var errAWSInstall = errors.New("AWS CLI installation didn't finish. Retry.")

// AWS's own archive installer is used after verification. Downloading and
// unpacking in Go avoids requiring curl, unzip or gpg on a fresh computer.
type awsInstaller struct {
	client     *http.Client
	goos, arch string
	run        func(context.Context, string, []string, []string) (cloudOutput, error)
}

func newAWSInstaller() *awsInstaller {
	return &awsInstaller{client: &http.Client{Timeout: 2 * time.Minute}, goos: runtime.GOOS, arch: runtime.GOARCH, run: runCloudProcess}
}
func awsInstallHint(goos, arch string) string {
	if (goos == "linux" || goos == "darwin") && (arch == "amd64" || arch == "arm64") {
		return ""
	}
	if goos == "windows" && (arch == "amd64" || arch == "arm64") {
		return "Install AWS CLI v2 for Windows on this computer, then choose Check again."
	}
	return fmt.Sprintf("AWS CLI v2 is not available for %s/%s. Use a supported Linux, macOS or Windows computer.", goos, arch)
}

var awsInstallMu sync.Mutex

var awsVersion = regexp.MustCompile(`^aws-cli/2\.(\d+)\.(\d+)\b`)

func validAWSVersion(value string) bool {
	match := awsVersion.FindStringSubmatch(strings.TrimSpace(value))
	if match == nil {
		return false
	}
	minor, _ := strconv.Atoi(match[1])
	return minor >= 32
}
func validateAWSExecutable(ctx context.Context, path string) error {
	out, err := runCloudProcess(ctx, path, []string{"--version"}, awsCLI{}.environment())
	if err != nil || out.ExitCode != 0 || !validAWSVersion(out.Stdout) {
		return errors.New("AWS CLI v2 is missing or damaged")
	}
	return nil
}
func (a *awsInstaller) download(ctx context.Context, address, path string, limit int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return errAWSDownload
	}
	response, err := a.client.Do(req)
	if err != nil {
		return errAWSDownload
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errAWSDownload
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return errAWSInstall
	}
	size, copyErr := io.Copy(file, io.LimitReader(response.Body, limit+1))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || size == 0 || size > limit || response.ContentLength >= 0 && size != response.ContentLength {
		return errAWSIncomplete
	}
	return nil
}
func verifyAWSArchive(archive, signature string) error {
	keys, err := openpgp.ReadArmoredKeyRing(strings.NewReader(awsCLISigningKey))
	if err != nil {
		return errAWSIncomplete
	}
	data, err := os.Open(archive)
	if err != nil {
		return errAWSIncomplete
	}
	defer data.Close()
	sig, err := os.Open(signature)
	if err != nil {
		return errAWSIncomplete
	}
	defer sig.Close()
	if _, err = openpgp.CheckDetachedSignature(keys, data, sig); err != nil {
		return errAWSIncomplete
	}
	return nil
}
func unpackAWSArchive(archive, destination string) error {
	reader, err := zip.OpenReader(archive)
	if err != nil {
		return errAWSIncomplete
	}
	defer reader.Close()
	var total uint64
	for _, entry := range reader.File {
		total += entry.UncompressedSize64
		if total > 1<<30 {
			return errAWSIncomplete
		}
		name := filepath.FromSlash(entry.Name)
		if !filepath.IsLocal(name) || !strings.HasPrefix(entry.Name, "aws/") || entry.Mode()&os.ModeSymlink != 0 {
			return errAWSIncomplete
		}
		target := filepath.Join(destination, name)
		if entry.FileInfo().IsDir() {
			if err = os.MkdirAll(target, 0700); err != nil {
				return errAWSInstall
			}
			continue
		}
		if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return errAWSInstall
		}
		input, e := entry.Open()
		if e != nil {
			return errAWSIncomplete
		}
		mode := os.FileMode(0600)
		if entry.Mode()&0111 != 0 {
			mode = 0700
		}
		output, e := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if e != nil {
			input.Close()
			return errAWSInstall
		}
		n, e := io.Copy(output, io.LimitReader(input, int64(entry.UncompressedSize64)+1))
		input.Close()
		closeErr := output.Close()
		if e != nil || closeErr != nil || uint64(n) != entry.UncompressedSize64 {
			return errAWSIncomplete
		}
	}
	return nil
}
func (a *awsInstaller) install(ctx context.Context, dir string) error {
	if !awsInstallMu.TryLock() {
		return errors.New("AWS CLI is already installing. Retry shortly.")
	}
	defer awsInstallMu.Unlock()
	if hint := awsInstallHint(a.goos, a.arch); hint != "" {
		return errors.New(hint)
	}
	address := "https://awscli.amazonaws.com/AWSCLIV2.pkg"
	if a.goos == "linux" {
		arch := "x86_64"
		if a.arch == "arm64" {
			arch = "aarch64"
		}
		address = "https://awscli.amazonaws.com/awscli-exe-linux-" + arch + ".zip"
	}
	// ETag is only a bandwidth hint; activation still requires signature verification.
	etag := ""
	request, _ := http.NewRequestWithContext(ctx, http.MethodHead, address, nil)
	if response, err := a.client.Do(request); err == nil {
		response.Body.Close()
		if response.StatusCode == http.StatusOK {
			etag = response.Header.Get("ETag")
		}
	}
	var previous struct {
		ETag    string `json:"etag"`
		Version string `json:"version"`
	}
	data, _ := os.ReadFile(filepath.Join(dir, "package.json"))
	_ = json.Unmarshal(data, &previous)
	if etag != "" && previous.ETag == etag {
		check, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if version, err := cloudExecutableVersion(check, "aws", filepath.Join(dir, "bin", "aws"), a.run, awsCLI{}.environment()); err == nil && version == previous.Version {
			return nil
		}
	}
	generation, lease, err := newCloudGeneration(dir, "install-")
	if err != nil {
		return errAWSInstall
	}
	defer closeCloudLease(lease)
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(generation)
		}
	}()
	downloads := filepath.Join(generation, "downloads")
	if err = os.Mkdir(downloads, 0700); err != nil {
		return errAWSInstall
	}
	env := awsCLI{}.environment()
	if a.goos == "linux" {
		arch := "x86_64"
		if a.arch == "arm64" {
			arch = "aarch64"
		}
		address := "https://awscli.amazonaws.com/awscli-exe-linux-" + arch + ".zip"
		archive := filepath.Join(downloads, "aws.zip")
		signature := archive + ".sig"
		if err = a.download(ctx, address, archive, 128<<20); err != nil {
			return err
		}
		if err = a.download(ctx, address+".sig", signature, 64<<10); err != nil {
			return err
		}
		if err = verifyAWSArchive(archive, signature); err != nil {
			return err
		}
		if err = unpackAWSArchive(archive, downloads); err != nil {
			return err
		}
		out, e := a.run(ctx, filepath.Join(downloads, "aws", "install"), []string{"--install-dir", filepath.Join(generation, "aws-cli"), "--bin-dir", filepath.Join(generation, "bin")}, env)
		if e != nil || out.ExitCode != 0 {
			return errAWSInstall
		}
	} else {
		pkg := filepath.Join(downloads, "AWSCLIV2.pkg")
		if err = a.download(ctx, "https://awscli.amazonaws.com/AWSCLIV2.pkg", pkg, 256<<20); err != nil {
			return err
		}
		signature, e := a.run(ctx, "/usr/sbin/pkgutil", []string{"--check-signature", pkg}, env)
		// AWS's official installer pins Apple's AWS team ID, not just a name.
		if e != nil || signature.ExitCode != 0 || !strings.Contains(signature.Stdout, "94KV3E626L") {
			return errAWSIncomplete
		}
		var escaped bytes.Buffer
		_ = xml.EscapeText(&escaped, []byte(generation))
		choices := filepath.Join(downloads, "choices.xml")
		data := `<?xml version="1.0"?><plist version="1.0"><array><dict><key>choiceAttribute</key><string>customLocation</string><key>attributeSetting</key><string>` + escaped.String() + `</string><key>choiceIdentifier</key><string>default</string></dict></array></plist>`
		if err = os.WriteFile(choices, []byte(data), 0600); err != nil {
			return errAWSInstall
		}
		out, e := a.run(ctx, "/usr/sbin/installer", []string{"-pkg", pkg, "-target", "CurrentUserHomeDirectory", "-applyChoiceChangesXML", choices}, env)
		if e != nil || out.ExitCode != 0 {
			return errAWSInstall
		}
	}
	installed := filepath.Join(generation, "aws-cli", "v2", "current", "bin", "aws")
	if a.goos == "darwin" {
		installed = filepath.Join(generation, "aws-cli", "aws")
	}
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, e := a.run(checkCtx, installed, []string{"--version"}, env)
	if e != nil || out.ExitCode != 0 || !validAWSVersion(out.Stdout) {
		return errAWSInstall
	}
	version := strings.TrimPrefix(awsVersion.FindString(strings.TrimSpace(out.Stdout)), "aws-cli/")
	if current, err := cloudExecutableVersion(checkCtx, "aws", filepath.Join(dir, "bin", "aws"), a.run, env); err == nil && cloudVersionLess(version, current) {
		return errors.New("The AWS download is older than the installed CLI")
	}
	out, e = a.run(checkCtx, installed, []string{"sts", "get-caller-identity", "--generate-cli-skeleton", "input"}, env)
	if e != nil || out.ExitCode != 0 || !strings.HasPrefix(strings.TrimSpace(out.Stdout), "{") {
		return errAWSInstall
	}
	if err = ctx.Err(); err != nil {
		return errAWSInstall
	}
	if err = activateCloudGeneration(dir, "aws", installed); err != nil {
		return errAWSInstall
	}
	published = true
	if etag != "" {
		previous.ETag, previous.Version = etag, version
		data, _ := json.Marshal(previous)
		_ = os.WriteFile(filepath.Join(dir, "package.json"), data, 0600)
	}
	_ = os.RemoveAll(downloads)
	pruneCloudGenerations(dir, "aws")
	return nil
}
