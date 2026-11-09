package selfupdate

import (
	"archive/zip"
	"bytes"
	"testing"
)

func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, body := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		f.Write([]byte(body))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtractBinaryReadsTheWindowsZip(t *testing.T) {
	archive := zipOf(t, map[string]string{"LICENSE": "l", "NOTICE": "n", "TRADEMARKS.md": "t", "mewla.exe": "MZ-binary"})
	got, err := extractBinary(archive)
	if err != nil || string(got) != "MZ-binary" {
		t.Fatalf("extractBinary = %q, %v", got, err)
	}
	if _, err := extractBinary(zipOf(t, map[string]string{"LICENSE": "l"})); err == nil {
		t.Fatal("a zip without mewla.exe was accepted")
	}
	if name, err := PlatformArtifactName("windows", "amd64"); err != nil || name != "mewla-windows-amd64.zip" {
		t.Fatalf("PlatformArtifactName(windows) = %q, %v", name, err)
	}
}
