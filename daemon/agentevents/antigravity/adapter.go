// Package antigravity adapts Antigravity (agy) to the normalized agent events.
//
// agy's terminal UI has no channel to the outside, but agy speaks the Agent
// Client Protocol (`agy acp`). A Session Mewla starts is therefore Mewla's own
// program, `mewla agy-session`, which holds agy over ACP, writes the
// conversation to its log and reports to the daemon over a local socket
// (work/antigravity_session.go, cmd/mewla/agy_events.go). Everything is
// structured: the conversation id is known from the first moment, turns start
// and end with agy's own stop reason, messages go in as prompts, permissions
// arrive as requests with their options, and a stop is ACP's cancel.
package antigravity

import (
	"github.com/daoleno/mewla/daemon/agentevents"
	"github.com/daoleno/mewla/daemon/work"
)

// Provider is the executor provider this adapter serves.
const Provider = work.WorkerProviderAntigravity

// Adapter turns an agy launch into the Session program.
type Adapter struct {
	// Mewla is the mewla program; StateDir the daemon's state directory.
	Mewla    string
	StateDir string
}

// Descriptor declares agy's capabilities through ACP.
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

// PrepareLaunch runs a plain `agy [flags]` launch as the Session program. A
// command it cannot take over (a subcommand, print mode, an environment
// prefix) is launched as it is, as agy's own terminal UI, and followed by the
// fallback (the screen and a replay of its conversation).
func (a Adapter) PrepareLaunch(command string) (agentevents.Launch, error) {
	if work.IsAntigravitySessionCommand(command) {
		return agentevents.Launch{Command: command}, nil
	}
	if launched, ok := work.AntigravitySessionLaunchCommand(command, a.Mewla, a.StateDir); ok {
		return agentevents.Launch{Command: launched}, nil
	}
	return agentevents.Launch{Command: command}, nil
}
