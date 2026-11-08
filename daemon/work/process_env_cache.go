package work

import (
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/workerproc"
)

// processEnvTTL bounds how long a process tree's environment lookup is trusted.
// A process's environment is fixed when it starts, so the answer for a given
// process only changes when the tree below it does; the bound covers that. The
// lookup walks every descendant and reads each one's whole environment, so a
// provider process with a dozen tool children costs hundreds of read syscalls
// per call, and the watcher asks for each Worker on every poll.
const processEnvTTL = 20 * time.Second

type processEnvKey struct {
	pid int
	key string
}

type processEnvEntry struct {
	start string
	value string
	at    time.Time
}

var processEnvCache struct {
	sync.Mutex
	entries map[processEnvKey]processEnvEntry
}

// cachedProcessTreeEnvironValue is firstProcessTreeEnvironValue for callers on
// a poll path. The answer, an unset variable included, is reused for the same
// process (pid and start time, so pid reuse is a miss) until it is processEnvTTL
// old.
func cachedProcessTreeEnvironValue(processID int, key string) string {
	if processID <= 0 {
		return ""
	}
	start, ok := workerproc.StartToken(processID)
	if !ok {
		return firstProcessTreeEnvironValue(processID, key)
	}
	now := time.Now()
	k := processEnvKey{processID, key}
	processEnvCache.Lock()
	entry, ok := processEnvCache.entries[k]
	processEnvCache.Unlock()
	if ok && entry.start == start && now.Sub(entry.at) < processEnvTTL {
		return entry.value
	}
	value := firstProcessTreeEnvironValue(processID, key)
	processEnvCache.Lock()
	defer processEnvCache.Unlock()
	if processEnvCache.entries == nil {
		processEnvCache.entries = map[processEnvKey]processEnvEntry{}
	}
	if len(processEnvCache.entries) >= 512 {
		for old, e := range processEnvCache.entries {
			if now.Sub(e.at) >= processEnvTTL {
				delete(processEnvCache.entries, old)
			}
		}
		if len(processEnvCache.entries) >= 512 {
			processEnvCache.entries = map[processEnvKey]processEnvEntry{}
		}
	}
	processEnvCache.entries[k] = processEnvEntry{start: start, value: value, at: now}
	return value
}
