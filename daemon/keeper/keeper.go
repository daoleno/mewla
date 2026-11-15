// Package keeper runs a program the daemon starts for agents (the agent
// screen's Xvnc, the agent browser's Chrome) in a process of its own that
// outlives the daemon, so a restart or an update of Mewla keeps what agents
// left open for the person. The daemon attaches to the keeper over a private
// Unix socket; a new daemon attaches again and carries on. A keeper nobody has
// attached to for its linger time stops its program and ends, so a Mewla that
// is gone for good leaves nothing running.
//
// For Chrome the keeper also holds the debugging pipe (Chrome ends when that
// pipe closes) and relays it to the attached daemon.
package keeper

import (
	"encoding/json"
	"time"
)

// SpecEnv names the file a keeper process reads its Spec from: the mewla
// binary checks it before anything else and runs Main.
const SpecEnv = "MEWLA_KEEPER_SPEC"

// Spec is what one keeper runs.
type Spec struct {
	// Socket is where the keeper listens, mode 0600; its directory must be
	// private to the user.
	Socket string `json:"socket"`
	// Linger is how long the keeper waits with nobody attached before it
	// stops the program.
	Linger time.Duration `json:"linger"`
	// CDP gives the program Chrome's debugging pipe on descriptors 3 and 4
	// and relays it to the attached client; stopping asks Chrome to close
	// first, so it saves its profile.
	CDP  bool     `json:"cdp,omitempty"`
	Argv []string `json:"argv"`
	Env  []string `json:"env"`
	// OwnerEnv, when set, is given to the program as OwnerEnv=<keeper pid>,
	// so stray-process sweeps see a live owner.
	OwnerEnv string `json:"owner_env,omitempty"`
	// Lock is a file the keeper holds an exclusive lock on while it runs.
	Lock string `json:"lock,omitempty"`
	// Log takes the program's output (kept short) and the keeper's own words.
	Log string `json:"log,omitempty"`
	// Cleanup are paths removed once the program has ended.
	Cleanup []string `json:"cleanup,omitempty"`
}

func (s Spec) encode() ([]byte, error) { return json.Marshal(s) }
