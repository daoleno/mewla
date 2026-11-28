package main

import (
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/doctor"
	"github.com/daoleno/mewla/daemon/hostdeps"
)

const (
	hostVersionProbeTTL   = 30 * time.Second
	hostVersionSettledTTL = 15 * time.Minute
)

type hostProbeResult struct {
	at          time.Time
	ttl         time.Duration
	unsupported string
}

// newHostProbes caches whether an installed agent's version runs here. Snapshot
// reads use the cache; only a selected, launched or recovering agent probes,
// and a probe checks just that agent. Sign-in is never checked.
func newHostProbes(check func(string) doctor.ExecutorsCheck, now func() time.Time) (func(string), func(string) string) {
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
		result := hostProbeResult{ttl: hostVersionProbeTTL}
		for _, item := range check(id).Items {
			if item.ID != id {
				continue
			}
			if item.BinaryFound && !item.Runnable {
				result.unsupported = item.Summary
			}
			if item.Runnable {
				result.ttl = hostVersionSettledTTL
			}
			break
		}
		result.at = now()
		results[id] = result
	}
	unsupported := func(id string) string {
		mu.Lock()
		defer mu.Unlock()
		return results[id].unsupported
	}
	return probe, unsupported
}

func installHostVersionProbe() {
	options := doctor.Options{LookPath: hostdeps.LookPath}
	probe, unsupported := newHostProbes(func(id string) doctor.ExecutorsCheck {
		options.ExecutorID = id
		return doctor.CheckExecutors(options)
	}, time.Now)
	brain.SetHostActiveProbe(probe)
	brain.SetHostUnsupportedProbe(unsupported)
}
