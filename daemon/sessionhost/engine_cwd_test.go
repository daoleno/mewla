//go:build ghosttyvt && cgo && (linux || darwin)

package sessionhost

import (
	"os"
	"testing"

	"github.com/daoleno/mewla/daemon/ghosttyvt"
)

// macProgram is a running program whose working directory cannot be read,
// as on macOS (processCwd is ""): the Session's cwd is the program's own
// OSC 7 report.
type macProgram struct{ program }

func (macProgram) Cwd() string     { return "" }
func (macProgram) Command() string { return "" }

// A shell on a Mac reports OSC 7 as a file URL with its host name and a
// percent-encoded path. The Session's cwd, which the watcher hands the app as
// the Worker's folder and the New session sheet offers back as a dir, is the
// absolute path, never the URL.
func TestOSC7ReportReachesSessionInfoAsAPath(t *testing.T) {
	host, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	term, err := ghosttyvt.New(ghosttyvt.Options{Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(term.Close)
	s := &session{id: "mac", dir: "/start", alive: true, proc: macProgram{}, screen: term, status: newStatusRecords()}
	term.Write([]byte("\x1b]7;file://" + host + "/Users/x/a%20b\x07"))
	if got := s.info().Cwd; got != "/Users/x/a b" {
		t.Fatalf("Session cwd after OSC 7 = %q, want %q", got, "/Users/x/a b")
	}
	// A report from another machine (a shell over ssh) says nothing about
	// this one: the Session keeps the folder it started in.
	term.Write([]byte("\x1b]7;file://elsewhere.example/home/x\x07"))
	if got := s.info().Cwd; got != "/start" {
		t.Fatalf("Session cwd after a foreign OSC 7 = %q, want the start dir", got)
	}
}
