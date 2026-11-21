package watcher

import "time"

// startPin is the first start observed for a Worker whose process the scan
// did not bind, with what that observation was made of.
type startPin struct {
	pid     int
	pgid    int
	command string
	at      time.Time
}

// pinFallbackStart keeps a Worker's start steady when the scan could not read
// it. An unbound agent's start is the poll's own snapshot time (fallback), a
// new value every poll, and readers that look for a conversation "written
// since the Worker started" would never find an idle one. The first observed
// value is kept for as long as the Worker is the same launch: the same pid,
// the same foreground process group (a new command in the pane is a new group)
// and the same agent. A start the scan did read is returned as is, and
// forgets any pin.
func pinFallbackStart(pins map[string]startPin, id string, fallback bool, command string, pid, pgid int, observed time.Time) time.Time {
	if !fallback {
		delete(pins, id)
		return observed
	}
	name := workerCommandName(command)
	if pin, ok := pins[id]; ok && pin.pid == pid && pin.pgid == pgid && pin.command == name && !pin.at.IsZero() {
		return pin.at
	}
	pins[id] = startPin{pid: pid, pgid: pgid, command: name, at: observed}
	return observed
}
