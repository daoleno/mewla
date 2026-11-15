package watcher

import "time"

// PollPane is the wire shape of one Session inventory entry for the
// test-only poll source seam.
type PollPane struct {
	Target           string
	Name             string
	Cwd              string
	Command          string
	PiSessionBinding string
	PanePID          int
	Hidden           bool
	Delegated        bool
	ResourceUnit     string
}

// PollProcess is the wire shape of one process-table entry for the test-only
// poll source seam.
type PollProcess struct {
	PID       int
	PPID      int
	PGID      int
	TPGID     int
	StartedAt time.Time
	Comm      string
	Args      string
}

// PollSources injects the polling loop's external reads (Session
// inventory, screen capture, process snapshot, pane generation). Production
// callers never install it; it exists so end-to-end tests can drive the real
// poll loop against a real canonical ledger without a session host.
type PollSources struct {
	ListPanes         func() ([]PollPane, error)
	CapturePane       func(target string) (content string, alive bool, deadStatus int)
	SnapshotProcesses func() map[int]PollProcess
	// PaneGeneration returns the pane generation for a target; nil keeps
	// the real read. Test-only.
	PaneGeneration func(target string) string
	// StartSession replaces the session host's start of a new Session: it
	// gets what the Session would run and returns its id. Test-only.
	StartSession func(PollStart) (string, error)
}

// PollStart is what CreateSession hands StartSession.
type PollStart struct {
	Name    string
	Cwd     string
	Command string
	Env     []string
}

// startHookBackend starts Sessions through PollSources.StartSession and asks
// the backend it wraps everything else.
type startHookBackend struct {
	sessionBackend
	start func(PollStart) (string, error)
}

func (b startHookBackend) place(string, bool, string) (backendPlacement, error) {
	return backendPlacement{}, nil
}

func (b startHookBackend) create(_ backendPlacement, spec backendCreateSpec) (string, error) {
	return b.start(PollStart{Name: spec.name, Cwd: spec.cwd, Command: spec.command, Env: spec.env})
}

// creator is the backend CreateSession starts a Session with.
func (w *Watcher) creator() sessionBackend {
	backend := w.sessions()
	w.mu.RLock()
	sources := w.pollSources
	w.mu.RUnlock()
	if sources != nil && sources.StartSession != nil {
		return startHookBackend{sessionBackend: backend, start: sources.StartSession}
	}
	return backend
}

// SetPollSources installs test-only poll sources and returns a restore
// function. Nil entries keep the production source. Test-only; production
// code must never install it.
func (w *Watcher) SetPollSources(sources PollSources) func() {
	w.mu.Lock()
	previousSources := w.pollSources
	previousList := w.listPanes
	previousCapture := w.capturePane
	previousSnapshot := w.snapshotProcesses
	w.pollSources = &sources
	if sources.ListPanes != nil {
		w.listPanes = func() ([]sessionPane, error) {
			windows, err := sources.ListPanes()
			if err != nil {
				return nil, err
			}
			out := make([]sessionPane, 0, len(windows))
			for _, win := range windows {
				out = append(out, sessionPane{
					target: win.Target, name: win.Name, cwd: win.Cwd,
					command: win.Command, piSessionBinding: win.PiSessionBinding, panePID: win.PanePID,
					hidden: win.Hidden, delegated: win.Delegated,
					resourceUnit: win.ResourceUnit,
				})
			}
			return out, nil
		}
	}
	if sources.CapturePane != nil {
		w.capturePane = sources.CapturePane
	}
	if sources.SnapshotProcesses != nil {
		w.snapshotProcesses = func() map[int]processInfo {
			processes := sources.SnapshotProcesses()
			out := make(map[int]processInfo, len(processes))
			for _, process := range processes {
				out[process.PID] = processInfo{
					pid: process.PID, ppid: process.PPID, pgid: process.PGID,
					tpgid: process.TPGID, startedAt: process.StartedAt,
					comm: process.Comm, args: process.Args,
				}
			}
			return out
		}
	}
	w.mu.Unlock()
	return func() {
		w.mu.Lock()
		w.listPanes = previousList
		w.capturePane = previousCapture
		w.snapshotProcesses = previousSnapshot
		w.pollSources = previousSources
		w.mu.Unlock()
	}
}
