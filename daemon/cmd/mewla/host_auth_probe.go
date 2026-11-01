package main

import (
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/doctor"
	"github.com/daoleno/mewla/daemon/hostdeps"
)

// hostAuthProbeTTL keeps doctor's sign-in probes, which run each agent CLI's
// own status command, off the path of every snapshot.
const hostAuthProbeTTL = 30 * time.Second

// newHostAuthProbe is the sign-in probe Brain's host choice and host problem
// report use: the same answer mewla doctor gives, so the two cannot disagree
// about which agent is signed in.
func newHostAuthProbe(check func() doctor.ExecutorsCheck, now func() time.Time) func(string) brain.HostAuthState {
	var (
		mu      sync.Mutex
		at      time.Time
		results map[string]brain.HostAuthState
	)
	return func(id string) brain.HostAuthState {
		mu.Lock()
		defer mu.Unlock()
		if results == nil || now().Sub(at) > hostAuthProbeTTL {
			results = map[string]brain.HostAuthState{}
			for _, item := range check().Items {
				switch {
				case item.VerifiedAuthenticated:
					results[item.ID] = brain.HostAuthAuthenticated
				case item.BinaryFound && item.Auth == doctor.AuthUnauthenticated:
					results[item.ID] = brain.HostAuthUnauthenticated
				}
			}
			at = now()
		}
		if state, ok := results[id]; ok {
			return state
		}
		return brain.HostAuthUnknown
	}
}

func installHostAuthProbe() {
	options := doctor.Options{LookPath: hostdeps.LookPath}
	brain.SetHostAuthProbe(newHostAuthProbe(func() doctor.ExecutorsCheck {
		return doctor.CheckExecutors(options)
	}, time.Now))
}
