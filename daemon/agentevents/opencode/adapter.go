// Package opencode adapts OpenCode 2's server API to the normalized agent
// events.
//
// OpenCode 2 keeps every session behind one background service, and each TUI
// is a client of it. Mewla follows a Worker's session on that service: the
// session id is chosen at launch (`--session`), the service pushes every
// execution, message and permission request over its event stream, and a
// message, an answer or a stop goes back as an ordinary API call. Mapper
// turns the stream into events; nothing is read from OpenCode's storage and
// nothing is typed into the pane.
package opencode

import (
	"github.com/daoleno/mewla/daemon/agentevents"
	"github.com/daoleno/mewla/daemon/work"
)

// Provider is the executor provider this adapter serves.
const Provider = "opencode"

// Adapter follows an OpenCode Worker through the background service.
type Adapter struct{}

// Descriptor declares OpenCode's capabilities: the service has a typed
// interface for all of them, so nothing is a keystroke.
func (Adapter) Descriptor() agentevents.Descriptor {
	return agentevents.Descriptor{
		Provider: Provider,
		Capabilities: map[agentevents.Capability]agentevents.Level{
			agentevents.CapBinding:   agentevents.LevelStructured,
			agentevents.CapEvents:    agentevents.LevelStructured,
			agentevents.CapSend:      agentevents.LevelStructured,
			agentevents.CapApprove:   agentevents.LevelStructured,
			agentevents.CapInterrupt: agentevents.LevelStructured,
		},
	}
}

// PrepareLaunch gives a fresh launch its own session id, so the Worker's
// conversation is known from the first moment. A launch that names or
// continues a session, or that is not a plain command, is left as it is and
// followed by the fallback.
func (Adapter) PrepareLaunch(command string) (agentevents.Launch, error) {
	prepared, err := work.EnsureOpenCodeSessionLaunchCommand(command)
	if err != nil {
		return agentevents.Launch{Command: command}, nil
	}
	return agentevents.Launch{Command: prepared}, nil
}
