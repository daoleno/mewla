package doctor

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

// On Windows every Session runs in the session host: the platform is
// supported and tmux is neither checked nor offered.
func TestWindowsNeedsNoTmux(t *testing.T) {
	home := t.TempDir()
	report, err := Run(Options{
		GOOS:     "windows",
		Home:     home,
		StateDir: filepath.Join(home, ".mewla"),
		Addr:     "127.0.0.1:0",
		PathEnv:  t.TempDir(),
		Listen:   func(network, address string) (io.Closer, error) { return nopCloser{}, nil },
		HTTPGet:  func(ctx context.Context, url string) (int, []byte, error) { return 0, nil, errors.New("refused") },
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Platform.Status != StatusOK || !report.Platform.Supported {
		t.Fatalf("platform = %+v", report.Platform)
	}
	if !report.Tmux.NotNeeded || report.Tmux.Status != StatusOK || report.Tmux.Remediation != "" {
		t.Fatalf("tmux = %+v", report.Tmux)
	}
	for _, check := range report.Checks {
		if check.ID == "tmux" {
			t.Fatalf("checks list tmux: %+v", report.Checks)
		}
	}
	for _, code := range report.Remediations {
		if code == RemediationInstallTmux || code == RemediationUnsupportedPlatform {
			t.Fatalf("remediations = %v", report.Remediations)
		}
	}
	var human bytes.Buffer
	if err := WriteHuman(&human, report); err != nil {
		t.Fatal(err)
	}
	if text := human.String(); strings.Contains(text, "] tmux:") || strings.Contains(text, "Install tmux") || strings.Contains(text, "Missing tmux") || strings.Contains(text, "not supported") {
		t.Fatalf("human output mentions tmux or an unsupported platform:\n%s", text)
	}
	if check := CheckTmux(Options{GOOS: "windows", LookPath: func(string) (string, error) { return "", errors.New("missing") }}); check.Status != StatusOK {
		t.Fatalf("first-run tmux check on Windows = %+v", check)
	}
}
