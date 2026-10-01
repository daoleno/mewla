//go:build !linux

package watcher

import "time"

type cpuCounters struct{ Total, Idle uint64 }
type ioCounters struct{ Read, Write uint64 }

func (s *ResourceSampler) samplePlatform(now time.Time) MachineResourceSnapshot {
	return MachineResourceSnapshot{Disks: []DiskResource{}, Consumers: []ProcessConsumer{}, Unavailable: []string{"cpu", "memory", "psi", "disks", "processes"}}
}
