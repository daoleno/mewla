package watcher

import "time"

// PollPane is the wire shape of one tmux pane inventory entry for the
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

// PollSources injects the polling loop's external reads (tmux pane
// inventory, pane capture, process snapshot, pane generation). Production
// callers never install it; it exists so end-to-end tests can drive the real
// poll loop against a real canonical ledger without tmux.
type PollSources struct {
	ListPanes         func() ([]PollPane, error)
	CapturePane       func(target string) (content string, alive bool, deadStatus int)
	SnapshotProcesses func() map[int]PollProcess
	// PaneGeneration returns the pane generation for a target; nil keeps
	// the real tmux read. Test-only.
	PaneGeneration func(target string) string
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
		w.listPanes = func() ([]tmuxPane, error) {
			windows, err := sources.ListPanes()
			if err != nil {
				return nil, err
			}
			out := make([]tmuxPane, 0, len(windows))
			for _, win := range windows {
				out = append(out, tmuxPane{
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
