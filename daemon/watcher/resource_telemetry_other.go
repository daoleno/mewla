//go:build !linux && !darwin

package watcher

import "time"

func (s *ResourceSampler) samplePlatform(now time.Time) MachineResourceSnapshot {
	return MachineResourceSnapshot{Disks: []DiskResource{}, Consumers: []ProcessConsumer{}, Unavailable: []string{"cpu", "memory", "psi", "disks", "processes"}}
}
