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

// hostAuthSettledTTL is how long the probes are kept once every configured
// agent has an answer nobody is waiting to change: found, runnable and not
// signed out. Each run starts every agent CLI, and the daemon is charged for
// what those processes read (a Node or Bun CLI reads several MB to start), so
// asking all of them again every 30 seconds was a burst of thousands of reads
// and tens of MB for nothing. While some agent is missing, signed out or
// unsupported a person may be fixing exactly that, and the short TTL stays.
const hostAuthSettledTTL = 15 * time.Minute

// hostProbesSettled reports whether no configured agent is waiting to be
// installed, signed in or upgraded.
func hostProbesSettled(items []doctor.ExecutorCheck) bool {
	for _, item := range items {
		if !item.BinaryFound || !item.Runnable || item.Auth == doctor.AuthUnauthenticated {
			return false
		}
	}
	return len(items) > 0
}

// newHostAuthProbe is the sign-in probe Brain's host choice and host problem
// report use: the same answer mewla doctor gives, so the two cannot disagree
// about which agent is signed in.
func newHostAuthProbe(check func() doctor.ExecutorsCheck, now func() time.Time) func(string) brain.HostAuthState {
	auth, _ := newHostProbes(check, now)
	return auth
}

// newHostProbes answers two questions from one cached doctor run: is the agent
// signed in, and, when doctor found its binary but will not run it (a version
// Mewla does not support), what doctor says about it.
func newHostProbes(check func() doctor.ExecutorsCheck, now func() time.Time) (func(string) brain.HostAuthState, func(string) string) {
	auth, unsupported, _ := newHostProbesWithAccounts(check, now)
	return auth, unsupported
}

// newHostProbesWithAccounts also answers who an agent is signed in as, from the
// same cached doctor run.
func newHostProbesWithAccounts(check func() doctor.ExecutorsCheck, now func() time.Time) (func(string) brain.HostAuthState, func(string) string, func(string) string) {
	var (
		accounts    map[string]string
		mu          sync.Mutex
		at          time.Time
		results     map[string]brain.HostAuthState
		unsupported map[string]string
		ttl         = hostAuthProbeTTL
	)
	refresh := func() {
		if results != nil && now().Sub(at) <= ttl {
			return
		}
		results = map[string]brain.HostAuthState{}
		unsupported = map[string]string{}
		accounts = map[string]string{}
		items := check().Items
		ttl = hostAuthProbeTTL
		if hostProbesSettled(items) {
			ttl = hostAuthSettledTTL
		}
		for _, item := range items {
			if item.Account != "" {
				accounts[item.ID] = item.Account
			}
			switch {
			case item.VerifiedAuthenticated:
				results[item.ID] = brain.HostAuthAuthenticated
			case item.BinaryFound && item.Auth == doctor.AuthUnauthenticated:
				results[item.ID] = brain.HostAuthUnauthenticated
			case item.BinaryFound && !item.Runnable:
				unsupported[item.ID] = item.Summary
			}
		}
		at = now()
	}
	auth := func(id string) brain.HostAuthState {
		mu.Lock()
		defer mu.Unlock()
		refresh()
		if state, ok := results[id]; ok {
			return state
		}
		return brain.HostAuthUnknown
	}
	unsupportedReason := func(id string) string {
		mu.Lock()
		defer mu.Unlock()
		refresh()
		return unsupported[id]
	}
	account := func(id string) string {
		mu.Lock()
		defer mu.Unlock()
		refresh()
		return accounts[id]
	}
	return auth, unsupportedReason, account
}

func installHostAuthProbe() {
	options := doctor.Options{LookPath: hostdeps.LookPath}
	auth, unsupported, account := newHostProbesWithAccounts(func() doctor.ExecutorsCheck {
		return doctor.CheckExecutors(options)
	}, time.Now)
	brain.SetHostAuthProbe(auth)
	brain.SetHostAccountProbe(account)
	brain.SetHostUnsupportedProbe(unsupported)
}
