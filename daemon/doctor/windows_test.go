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

// Windows is a supported platform.
func TestWindowsIsSupported(t *testing.T) {
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
	for _, code := range report.Remediations {
		if code == RemediationUnsupportedPlatform {
			t.Fatalf("remediations = %v", report.Remediations)
		}
	}
	var human bytes.Buffer
	if err := WriteHuman(&human, report); err != nil {
		t.Fatal(err)
	}
	if text := human.String(); strings.Contains(text, "not supported") {
		t.Fatalf("human output names an unsupported platform:\n%s", text)
	}
}
