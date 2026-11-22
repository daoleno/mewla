package watcher

import (
	"sync"
	"time"
)

// oomSamples keeps the kernel's count of out-of-memory kills as each poll saw
// it, for about two minutes, so a Worker killed by a signal can be said to
// have been stopped by the OOM killer when the count rose around its end.
type oomSamples struct {
	mu      sync.Mutex
	samples []oomSample
	read    func() (uint64, bool)
}

type oomSample struct {
	at    time.Time
	count uint64
}

const oomSampleWindow = 2 * time.Minute

func (o *oomSamples) sample(at time.Time) {
	o.mu.Lock()
	defer o.mu.Unlock()
	read := o.read
	if read == nil {
		read = readOOMKills
	}
	count, ok := read()
	if !ok {
		return
	}
	o.samples = append(o.samples, oomSample{at: at, count: count})
	cut := 0
	for cut < len(o.samples)-1 && at.Sub(o.samples[cut].at) > oomSampleWindow {
		cut++
	}
	o.samples = o.samples[cut:]
}

// killedAround reports whether the OOM kill count rose between the last
// sample before endedAt and the first one at or after it (or now).
func (o *oomSamples) killedAround(endedAt, now time.Time) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	var before, after *oomSample
	for index := range o.samples {
		sample := &o.samples[index]
		if !sample.at.After(endedAt) {
			before = sample
		} else if after == nil {
			after = sample
		}
	}
	if before == nil {
		return false
	}
	if after == nil {
		read := o.read
		if read == nil {
			read = readOOMKills
		}
		count, ok := read()
		if !ok {
			return false
		}
		after = &oomSample{at: now, count: count}
	}
	return after.count > before.count
}
