package logfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriterRotatesIntoOneBackup(t *testing.T) {
	path := Path(t.TempDir())
	writer, err := Open(path, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	line := strings.Repeat("x", 30) + "\n"
	for range 5 {
		if _, err := writer.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	live, _ := os.ReadFile(path)
	rotated, _ := os.ReadFile(RotatedPath(path))
	if len(live) == 0 || len(live) > 64 || len(rotated) == 0 || len(rotated) > 64 {
		t.Fatalf("live %d bytes, rotated %d bytes", len(live), len(rotated))
	}
	if matches, _ := filepath.Glob(path + ".*"); len(matches) != 1 {
		t.Fatalf("backups = %v", matches)
	}
}
