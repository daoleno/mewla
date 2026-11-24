package watcher

import (
	"strings"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/gc"
)

// GCWhyLookup is where mewla gc why reads on this daemon: its state root and
// its live Sessions by resource unit. ok is false before the resource
// manager has a state root.
func (w *Watcher) GCWhyLookup() (gc.WhyLookup, bool) {
	manager := w.resourceManager()
	rooted, ok := manager.(interface{ stateRoot() string })
	if !ok || rooted.stateRoot() == "" {
		return gc.WhyLookup{}, false
	}
	session := func(match func(*classifier.Worker) bool) (gc.WhySession, bool) {
		for _, worker := range w.Workers() {
			if worker == nil || !match(worker) {
				continue
			}
			name := strings.TrimSuffix(worker.Name, " ("+worker.ID+")")
			if worker.Hidden {
				name = "Brain"
			}
			return gc.WhySession{ID: worker.ID, Name: name, State: string(worker.State), Brain: worker.Hidden}, true
		}
		return gc.WhySession{}, false
	}
	return gc.WhyLookup{
		Root: rooted.stateRoot(),
		Session: func(unit string) (gc.WhySession, bool) {
			return session(func(worker *classifier.Worker) bool { return manager.UnitForTarget(worker.ID) == unit })
		},
		Pane: func(pid int) (gc.WhySession, bool) {
			target, ok := w.screens.targetOfPane(pid)
			if !ok {
				return gc.WhySession{}, false
			}
			return session(func(worker *classifier.Worker) bool { return worker.ID == target })
		},
	}, true
}

// GCCollects reports whether this daemon's collector runs: it measures sizes
// and moves items to the trash. Windows' resource manager has no collector.
func (w *Watcher) GCCollects() bool {
	_, ok := w.resourceManager().(interface{ collect() })
	return ok
}
