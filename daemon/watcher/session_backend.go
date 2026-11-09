package watcher

import (
	"strings"

	"github.com/daoleno/mewla/daemon/classifier"
)

// sessionBackend runs Sessions and keeps what Mewla records on them. Outside
// the backend a Session id is an opaque string: only the backend knows how ids
// are made, where a Session's labels and input receipts are kept, and how keys
// and pastes reach the program in it.
//
// tmux is the backend (tmux_*.go). Nothing outside a backend's own files may
// read or write its storage (tmux pane options) or parse its ids;
// TestSessionBackendBoundary enforces that, so a backend can be removed by
// deleting its files.
type sessionBackend interface {
	// validID is a syntax check on an id this backend could have issued. It
	// says nothing about presence.
	validID(id string) bool
	// observedGeneration is the generation of the Session an id names, as far
	// as the id itself proves it ("" when it proves none). A replaced Session
	// never has the generation of the one it replaced.
	observedGeneration(id string) string
	// terminal names how a Terminal view of the Session is opened.
	terminal(id string) TerminalTarget

	// listOwned is the authoritative inventory of this daemon's Sessions. A
	// backend that cannot be read is an error, never an empty inventory;
	// ErrTmuxServerGone says that nothing is running at all.
	listOwned() ([]sessionPane, error)
	// listServices lists the owned Sessions with the facts service discovery
	// attributes ports by. A backend with nothing running is an empty list.
	listServices() ([]servicePane, error)
	// probeOwnership reports whether a Session exists and is owned by Mewla.
	// A backend with nothing running reads as absent.
	probeOwnership(id string) (present, owned bool, err error)
	// resolveAbsence is the strict probe behind restart reconciliation: an
	// unreadable backend is ErrOwnershipProbeUnavailable, except when no
	// inventory has succeeded in this process (sawInventory false) and nothing
	// is running, which proves a Session recorded by an earlier run is gone.
	resolveAbsence(id string, sawInventory bool) (gone bool, err error)
	// processRoot returns the pid of the process the Session started; an
	// error when the Session is gone or dead.
	processRoot(id string) (pid int, err error)
	// currentPath is the Session's working directory as the backend sees it.
	currentPath(id string) (string, error)
	// delegatedResource reports the resource unit recorded on a delegated
	// Session.
	delegatedResource(id string) (delegated bool, unit string)

	// captureState returns the last 200 lines with liveness and, for a dead
	// Session, its exit status (-1 when unknown).
	captureState(id string) (screen string, alive bool, deadStatus int)
	// captureRecent returns the last 200 lines.
	captureRecent(id string) (string, error)
	// captureVisible returns the visible screen, with styles when styled.
	captureVisible(id string, styled bool) (string, error)
	// captureDialog returns the whole history with wrapped lines joined and
	// what the classifier needs to know about that capture.
	captureDialog(id string) (string, classifier.CaptureInfo, error)

	// sendKeys sends named keys (Enter, Escape, C-c, Up, …) or single
	// characters, in order, as one action.
	sendKeys(id string, keys ...string) error
	// repeatKey sends one named key count times.
	repeatKey(id, key string, count int) error
	// sendLiteral types text exactly, never reading it as key names.
	sendLiteral(id, text string) error
	// sendBytes types raw bytes.
	sendBytes(id string, data []byte) error

	// place decides where a new Session goes before anything is prepared for
	// it, so a bad request fails first.
	place(near string, detached bool, name string) (backendPlacement, error)
	// create starts a Session at a placement and returns its id. On failure
	// nothing is left running.
	create(at backendPlacement, spec backendCreateSpec) (string, error)
	// kill ends a Session. A Session that is already gone is not an error.
	kill(id string) error

	// inputIO is the Session input surface: generation proof, paste
	// submission and the durable input receipt ledger.
	inputIO() sessionInputIO
}

// backendPlacement is a backend's answer to place; only the backend reads it,
// and backend says which one gave it.
type backendPlacement struct {
	group    string
	newGroup bool
	backend  string
}

// Backends a placement can name.
const (
	backendTmux = ""
	backendHost = "host"
)

// backendCreateSpec is everything a backend needs to start a Session.
type backendCreateSpec struct {
	name    string
	cwd     string
	command string   // the shell command line the Session runs
	env     []string // KEY=value entries for the Session's environment
	labels  sessionLabels
}

// sessionLabels is what Mewla records on a Session it creates. The backend
// keeps them as long as the Session lives and reports them in listOwned.
type sessionLabels struct {
	hidden        bool
	delegated     bool
	executorID    string
	agentControl  string
	piSession     string // EncodePiSessionBinding, or ""
	resourceUnit  string
	resourceOwner string
}

// TerminalTarget is how a Terminal view of a Session is opened: the terminal
// backend that shows it and the server that backend reaches.
type TerminalTarget struct {
	Backend string // "tmux" or "host", the terminal package's backend names
	Socket  string
}

// TerminalTarget says how to open a Terminal view of a Session this watcher
// owns; ok is false for any other id.
func (w *Watcher) TerminalTarget(id string) (TerminalTarget, bool) {
	id = strings.TrimSpace(id)
	w.mu.RLock()
	_, owned := w.workers[id]
	backend := w.backend
	w.mu.RUnlock()
	if !owned || backend == nil {
		return TerminalTarget{}, false
	}
	return backend.terminal(id), true
}
