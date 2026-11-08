package workerproc

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestReadProcFileReadsSmallAndLargeFiles(t *testing.T) {
	dir := t.TempDir()
	for _, size := range []int{0, 10, procFileBuffer - 1, procFileBuffer, procFileBuffer + 1, 3*procFileBuffer + 7} {
		want := bytes.Repeat([]byte("x"), size)
		path := filepath.Join(dir, "f")
		if err := os.WriteFile(path, want, 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := ReadProcFile(path)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("size %d: got %d bytes, err %v", size, len(got), err)
		}
	}
	if _, err := ReadProcFile(filepath.Join(dir, "missing")); !os.IsNotExist(err) {
		t.Fatalf("missing file error = %v", err)
	}
}
