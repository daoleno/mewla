package main

import (
	"time"

	"github.com/daoleno/mewla/daemon/agentevents"
	"github.com/daoleno/mewla/daemon/server"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
)

// turnCommandScope says, for a keystroke Stop, which turn it interrupts and
// how its commands are told apart: only for a Worker whose adapter declares
// that it ends them by process (agentevents.LevelProcess), while its events
// say a turn is running.
func turnCommandScope(w *watcher.Watcher, execs *work.ExecutorConfig, adapters *agentevents.Registry, hub *agentevents.Hub) func(string) (watcher.TurnCommandScope, bool) {
	return func(sessionID string) (watcher.TurnCommandScope, bool) {
		worker := w.GetWorker(sessionID)
		if worker == nil {
			return watcher.TurnCommandScope{}, false
		}
		adapter, ok := adapters.For(providerOf(execs, worker.ExecutorID, worker.Command))
		if !ok || adapter.Descriptor().Level(agentevents.CapStopCommands) != agentevents.LevelProcess {
			return watcher.TurnCommandScope{}, false
		}
		matcher, ok := adapter.(agentevents.TurnCommandMatcher)
		if !ok {
			return watcher.TurnCommandScope{}, false
		}
		state, known := hub.State(sessionID)
		if !known || state.Exited || state.Current == nil || state.Current.Status != agentevents.TurnRunning || state.Current.StartedAt.IsZero() {
			return watcher.TurnCommandScope{}, false
		}
		turnID := state.Current.ID
		provider := adapter.Descriptor().Provider
		return watcher.TurnCommandScope{
			Since: state.Current.StartedAt,
			Match: matcher.TurnCommand,
			Agent: func() string {
				version := "unknown version"
				if reader, ok := adapter.(agentevents.AgentVersionReader); ok {
					if read := reader.AgentVersion(state.Binding); read != "" {
						version = read
					}
				}
				return provider + " " + version
			},
			Report: func(stopped []agentevents.StoppedCommand) {
				hub.Publish(agentevents.Event{WorkerID: sessionID, Kind: agentevents.KindCommandsStopped, At: time.Now().UTC(), TurnID: turnID, Stopped: stopped})
			},
		}, true
	}
}

// stoppedCommandsRecord is a KindCommandsStopped event as the line the
// Session's thread shows.
func stoppedCommandsRecord(e agentevents.Event) server.StoppedCommands {
	record := server.StoppedCommands{SessionID: e.WorkerID, TurnID: e.TurnID, At: e.At}
	for _, command := range e.Stopped {
		record.Commands = append(record.Commands, server.StoppedCommand{Command: command.Command, Ended: command.Ended, Reason: command.Reason, Unrecognized: command.Unrecognized})
	}
	return record
}
