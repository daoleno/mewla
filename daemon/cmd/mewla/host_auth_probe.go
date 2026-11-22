package main

import (
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/doctor"
	"github.com/daoleno/mewla/daemon/hostdeps"
)

const (
	hostAuthProbeTTL   = 30 * time.Second
	hostAuthSettledTTL = 15 * time.Minute
)

type hostProbeResult struct {
	at          time.Time
	ttl         time.Duration
	auth        brain.HostAuthState
	unsupported string
	account     string
}

// Snapshot reads use cached evidence. Only a selected agent or explicit
// diagnostic calls probe, and a probe checks just that configured agent.
func newHostProbes(check func(string) doctor.ExecutorsCheck, now func() time.Time) (func(string), func(string) brain.HostAuthState, func(string) string, func(string) string) {
	var mu sync.Mutex
	results := make(map[string]hostProbeResult)
	probe := func(id string) {
		mu.Lock()
		defer mu.Unlock()
		if id == "" {
			return
		}
		if result, ok := results[id]; ok && now().Sub(result.at) <= result.ttl {
			return
		}
		result := hostProbeResult{at: now(), ttl: hostAuthProbeTTL, auth: brain.HostAuthUnknown}
		for _, item := range check(id).Items {
			if item.ID != id {
				continue
			}
			switch {
			case item.VerifiedAuthenticated:
				result.auth = brain.HostAuthAuthenticated
			case item.BinaryFound && item.Auth == doctor.AuthUnauthenticated:
				result.auth = brain.HostAuthUnauthenticated
			}
			if item.BinaryFound && !item.Runnable && item.Auth != doctor.AuthUnauthenticated {
				result.unsupported = item.Summary
			}
			result.account = item.Account
			if item.BinaryFound && item.Runnable && item.Auth != doctor.AuthUnauthenticated {
				result.ttl = hostAuthSettledTTL
			}
			break
		}
		result.at = now()
		results[id] = result
	}
	auth := func(id string) brain.HostAuthState {
		mu.Lock()
		defer mu.Unlock()
		if result, ok := results[id]; ok {
			return result.auth
		}
		return brain.HostAuthUnknown
	}
	unsupported := func(id string) string {
		mu.Lock()
		defer mu.Unlock()
		return results[id].unsupported
	}
	account := func(id string) string {
		mu.Lock()
		defer mu.Unlock()
		return results[id].account
	}
	return probe, auth, unsupported, account
}

func installHostAuthProbe() {
	options := doctor.Options{LookPath: hostdeps.LookPath}
	probe, auth, unsupported, account := newHostProbes(func(id string) doctor.ExecutorsCheck {
		options.ExecutorID = id
		return doctor.CheckExecutors(options)
	}, time.Now)
	brain.SetHostActiveProbe(probe)
	brain.SetHostAuthProbe(auth)
	brain.SetHostAccountProbe(account)
	brain.SetHostUnsupportedProbe(unsupported)
}
