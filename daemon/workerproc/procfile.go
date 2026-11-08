package workerproc

import (
	"errors"
	"io"
	"os"
)

// procFileBuffer holds a typical /proc/<pid>/stat or cmdline, which are a few
// hundred bytes.
const procFileBuffer = 4096

// ReadProcFile reads a small procfs file with one read(2). os.ReadFile asks
// again after the data to find the end of the file, which doubles the read
// syscalls of a walk over every process; procfs returns a whole record in the
// first read when the buffer holds it. A record that fills the buffer is read
// to the end.
func ReadProcFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, procFileBuffer)
	n, err := f.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if n < len(buf) {
		return buf[:n], nil
	}
	rest, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	return append(buf[:n], rest...), nil
}
