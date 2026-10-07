package watcher

import (
	"encoding/json"
	"log"
	"os"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/workerproc"
)

type PressureAverages struct {
	Avg10  float64 `json:"avg10"`
	Avg60  float64 `json:"avg60"`
	Avg300 float64 `json:"avg300"`
}
type PressureResource struct {
	Some *PressureAverages `json:"some,omitempty"`
	Full *PressureAverages `json:"full,omitempty"`
}
type PressureSnapshot struct {
	CPU    PressureResource `json:"cpu"`
	Memory PressureResource `json:"memory"`
	IO     PressureResource `json:"io"`
}
type CPUResource struct {
	Load1              float64   `json:"load1"`
	Load5              float64   `json:"load5"`
	Load15             float64   `json:"load15"`
	UtilizationPercent *float64  `json:"utilization_percent,omitempty"`
	PerCorePercent     []float64 `json:"per_core_percent,omitempty"`
}
type MemoryResource struct {
	TotalBytes     uint64 `json:"total_bytes"`
	AvailableBytes uint64 `json:"available_bytes"`
	UsedBytes      uint64 `json:"used_bytes"`
	CacheBytes     uint64 `json:"cache_bytes"`
	SharedBytes    uint64 `json:"shared_bytes"`
	SwapTotalBytes uint64 `json:"swap_total_bytes"`
	SwapUsedBytes  uint64 `json:"swap_used_bytes"`
}
type DiskResource struct {
	Mount               string   `json:"mount"`
	TotalBytes          uint64   `json:"total_bytes"`
	FreeBytes           uint64   `json:"free_bytes"`
	UsedBytes           uint64   `json:"used_bytes"`
	ReadBytesPerSecond  *float64 `json:"read_bytes_per_second,omitempty"`
	WriteBytesPerSecond *float64 `json:"write_bytes_per_second,omitempty"`
}
type ConsumerProcess struct {
	PID      int    `json:"pid"`
	Start    string `json:"start"`
	Command  string `json:"command"`
	RSSBytes uint64 `json:"rss_bytes"`
}
type ProcessConsumer struct {
	Processes    []ConsumerProcess `json:"processes,omitempty"`
	ID           string            `json:"id"`
	Owner        string            `json:"owner"`
	WorkerID     string            `json:"worker_id,omitempty"`
	WorkID       string            `json:"work_id,omitempty"`
	Title        string            `json:"title,omitempty"`
	Status       string            `json:"status"`
	Executor     string            `json:"executor,omitempty"`
	Cwd          string            `json:"cwd,omitempty"`
	AgeSeconds   int64             `json:"age_seconds,omitempty"`
	RSSBytes     uint64            `json:"rss_bytes"`
	CPUPercent   *float64          `json:"cpu_percent,omitempty"`
	ProcessCount int               `json:"process_count"`
	Kinds        []string          `json:"kinds"`
	Commands     []string          `json:"commands"`
}
type TelemetryHistoryPoint struct {
	SampledAt               time.Time `json:"sampled_at"`
	State                   string    `json:"state"`
	MemoryAvailableBytes    *uint64   `json:"memory_available_bytes,omitempty"`
	MemoryUsedBytes         *uint64   `json:"memory_used_bytes,omitempty"`
	SwapUsedBytes           *uint64   `json:"swap_used_bytes,omitempty"`
	Load15                  float64   `json:"load15"`
	CPUPercent              *float64  `json:"cpu_percent,omitempty"`
	PSICPUSomeAvg10         *float64  `json:"psi_cpu_some_avg10,omitempty"`
	PSIMemorySomeAvg10      *float64  `json:"psi_memory_some_avg10,omitempty"`
	PSIIOSomeAvg10          *float64  `json:"psi_io_some_avg10,omitempty"`
	DiskReadBytesPerSecond  *float64  `json:"disk_read_bytes_per_second,omitempty"`
	DiskWriteBytesPerSecond *float64  `json:"disk_write_bytes_per_second,omitempty"`
}

type MachineResourceSnapshot struct {
	Version     int                     `json:"version"`
	SampledAt   time.Time               `json:"sampled_at"`
	State       string                  `json:"state"`
	CPU         *CPUResource            `json:"cpu,omitempty"`
	Memory      *MemoryResource         `json:"memory,omitempty"`
	PSI         *PressureSnapshot       `json:"psi,omitempty"`
	Disks       []DiskResource          `json:"disks"`
	Consumers   []ProcessConsumer       `json:"consumers"`
	History     []TelemetryHistoryPoint `json:"history,omitempty"`
	Endpoint    string                  `json:"endpoint"`
	Signals     []PressureSignal        `json:"signals"`
	Unavailable []string                `json:"unavailable,omitempty"`
}
type PressureSignal struct {
	Name      string  `json:"name"`
	Value     float64 `json:"value"`
	Threshold float64 `json:"threshold"`
	State     string  `json:"state"`
}
type ResourcePressureEvent struct {
	State            string                  `json:"state"`
	PreviousState    string                  `json:"previous_state"`
	SampledAt        time.Time               `json:"sampled_at"`
	Crossed          []PressureSignal        `json:"crossed"`
	Trend            []TelemetryHistoryPoint `json:"trend"`
	CPU              *CPUResource            `json:"cpu,omitempty"`
	Memory           *MemoryResource         `json:"memory,omitempty"`
	PSI              *PressureSnapshot       `json:"psi,omitempty"`
	Consumers        []ProcessConsumer       `json:"consumers"`
	Orphaned         []ProcessConsumer       `json:"orphaned"`
	Queued           int                     `json:"queued"`
	InFlight         int                     `json:"in_flight"`
	SnapshotEndpoint string                  `json:"snapshot_endpoint"`
}

type ResourceSampler struct {
	configPath, statePath string
	pending               []ResourcePressureEvent
	configModified        time.Time
	mu                    sync.RWMutex
	latest                MachineResourceSnapshot
	history               []TelemetryHistoryPoint
	previousCPU           map[string]cpuCounters
	previousProcesses     map[int]workerproc.Process
	previousIO            map[string]ioCounters
	previousAt            time.Time
	machine               PressureMachine
}

func NewResourceSampler() *ResourceSampler {
	return &ResourceSampler{machine: NewPressureMachine(ResourceThresholdsFromEnv())}
}
func (s *ResourceSampler) Snapshot() MachineResourceSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap := s.latest
	snap.History = append([]TelemetryHistoryPoint{}, s.history...)
	return snap
}

// Sampling is owned by the single daemon ticker, never by API reads.
func (s *ResourceSampler) Sample(w *Watcher, now time.Time) (MachineResourceSnapshot, *ResourcePressureEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.reloadResourceConfig(); err != nil {
		log.Printf("resource thresholds unchanged: %v", err)
	}
	snap := s.samplePlatform(now)
	snap.Version = 2
	snap.SampledAt = now
	snap.Endpoint = "get_resource_telemetry"
	snap.Consumers = attributeConsumers(snap.Consumers, w)
	previous := s.machine.State
	state, signals, notify := s.machine.Step(snap, now)
	snap.State = state
	snap.Signals = signals
	point := TelemetryHistoryPoint{SampledAt: now, State: state}
	if snap.CPU != nil {
		point.Load15 = snap.CPU.Load15
		point.CPUPercent = snap.CPU.UtilizationPercent
	}
	if snap.Memory != nil {
		point.MemoryAvailableBytes = &snap.Memory.AvailableBytes
		point.MemoryUsedBytes = &snap.Memory.UsedBytes
		point.SwapUsedBytes = &snap.Memory.SwapUsedBytes
	}
	if snap.PSI != nil {
		if snap.PSI.CPU.Some != nil {
			point.PSICPUSomeAvg10 = &snap.PSI.CPU.Some.Avg10
		}
		if snap.PSI.Memory.Some != nil {
			point.PSIMemorySomeAvg10 = &snap.PSI.Memory.Some.Avg10
		}
		if snap.PSI.IO.Some != nil {
			point.PSIIOSomeAvg10 = &snap.PSI.IO.Some.Avg10
		}
	}
	for _, d := range snap.Disks {
		if d.ReadBytesPerSecond != nil {
			if point.DiskReadBytesPerSecond == nil {
				v := 0.0
				point.DiskReadBytesPerSecond = &v
			}
			*point.DiskReadBytesPerSecond += *d.ReadBytesPerSecond
		}
		if d.WriteBytesPerSecond != nil {
			if point.DiskWriteBytesPerSecond == nil {
				v := 0.0
				point.DiskWriteBytesPerSecond = &v
			}
			*point.DiskWriteBytesPerSecond += *d.WriteBytesPerSecond
		}
	}
	s.history = append(s.history, point)
	if len(s.history) > 360 {
		s.history = append([]TelemetryHistoryPoint{}, s.history[len(s.history)-360:]...)
	}
	s.latest = snap
	s.previousAt = now
	if !notify {
		return snap, nil
	}
	top := snap.Consumers
	if len(top) > 8 {
		top = top[:8]
	}
	orphans := []ProcessConsumer{}
	for _, c := range snap.Consumers {
		if c.Owner == "orphaned_worker" && len(orphans) < 4 {
			orphans = append(orphans, c)
		}
	}
	trend := []TelemetryHistoryPoint{}
	for i := len(s.history) - 1; i >= 0 && len(trend) < 3; i -= 6 {
		trend = append(trend, s.history[i])
	}
	event := ResourcePressureEvent{State: state, PreviousState: previous, SampledAt: now, Crossed: signals, Trend: trend, CPU: snap.CPU, Memory: snap.Memory, PSI: snap.PSI, Consumers: top, Orphaned: orphans, SnapshotEndpoint: "get_resource_telemetry"}
	s.pending = append(s.pending, event)
	if err := s.saveResourceState(); err != nil {
		log.Printf("resource pressure event persistence: %v", err)
	}
	return snap, &event
}

type ThresholdPair struct {
	Elevated float64 `json:"elevated"`
	Critical float64 `json:"critical"`
}
type ResourceThresholds struct {
	AvailableMemoryPercent ThresholdPair `json:"available_memory_percent"`
	CPUBusyPercent         ThresholdPair `json:"cpu_busy_percent"`
	CPUSome                ThresholdPair `json:"cpu_some"`
	MemorySome             ThresholdPair `json:"memory_some"`
	MemoryFull             ThresholdPair `json:"memory_full"`
	IOFull                 ThresholdPair `json:"io_full"`
	DiskFreePercent        ThresholdPair `json:"disk_free_percent"`
	SustainSeconds         int           `json:"sustain_seconds"`
	RecoverySeconds        int           `json:"recovery_seconds"`
	CooldownSeconds        int           `json:"cooldown_seconds"`
}

func DefaultResourceThresholds() ResourceThresholds {
	return ResourceThresholds{ThresholdPair{15, 7}, ThresholdPair{90, 98}, ThresholdPair{20, 60}, ThresholdPair{2, 10}, ThresholdPair{1, 5}, ThresholdPair{5, 20}, ThresholdPair{10, 3}, 20, 30, 60}
}

// A single JSON override keeps configuration typed and the complete default
// available for audit. Invalid overrides fall back as a whole.
func ResourceThresholdsFromEnv() ResourceThresholds {
	c := DefaultResourceThresholds()
	raw := os.Getenv("ZEN_RESOURCE_THRESHOLDS")
	if raw == "" {
		return c
	}
	if json.Unmarshal([]byte(raw), &c) != nil || !c.Valid() {
		return DefaultResourceThresholds()
	}
	return c
}
func (c ResourceThresholds) Valid() bool {
	if c.SustainSeconds < 15 || c.SustainSeconds > 600 || c.RecoverySeconds < 15 || c.RecoverySeconds > 600 || c.CooldownSeconds < 30 || c.CooldownSeconds > 3600 {
		return false
	}
	for _, p := range []ThresholdPair{c.CPUBusyPercent, c.CPUSome, c.MemorySome, c.MemoryFull, c.IOFull} {
		if p.Elevated <= 0 || p.Critical < p.Elevated || p.Critical > 100 {
			return false
		}
	}
	for _, p := range []ThresholdPair{c.AvailableMemoryPercent, c.DiskFreePercent} {
		if p.Critical <= 0 || p.Elevated < p.Critical || p.Elevated > 100 {
			return false
		}
	}
	return true
}

type PressureMachine struct {
	Config           ResourceThresholds
	State            string
	candidate        string
	since            time.Time
	lastNotification map[string]time.Time
}

func NewPressureMachine(c ResourceThresholds) PressureMachine {
	return PressureMachine{Config: c, State: "normal", lastNotification: map[string]time.Time{}}
}
func pressureRank(s string) int {
	switch s {
	case "critical":
		return 2
	case "elevated":
		return 1
	}
	return 0
}
func (m *PressureMachine) Step(s MachineResourceSnapshot, now time.Time) (string, []PressureSignal, bool) {
	signals := m.signals(s)
	// A failed platform read is not evidence of recovery.
	if s.Memory == nil && (s.CPU == nil || s.CPU.UtilizationPercent == nil) && len(s.Disks) == 0 && (s.PSI == nil || (s.PSI.CPU.Some == nil && s.PSI.Memory.Some == nil && s.PSI.Memory.Full == nil && s.PSI.IO.Full == nil)) {
		m.candidate = ""
		return m.State, signals, false
	}
	desired := "normal"
	for _, sig := range signals {
		if pressureRank(sig.State) > pressureRank(desired) {
			desired = sig.State
		}
	}
	if desired == m.State {
		m.candidate = ""
		return m.State, signals, false
	}
	if desired != m.candidate {
		m.candidate = desired
		m.since = now
		return m.State, signals, false
	}
	duration := time.Duration(m.Config.SustainSeconds) * time.Second
	if pressureRank(desired) < pressureRank(m.State) {
		duration = time.Duration(m.Config.RecoverySeconds) * time.Second
	}
	if now.Sub(m.since) < duration {
		return m.State, signals, false
	}
	// Recovery always gets one event; recurring escalations to the same state
	// wait out cooldown before publishing another transition.
	if desired != "normal" && !m.lastNotification[desired].IsZero() && now.Sub(m.lastNotification[desired]) < time.Duration(m.Config.CooldownSeconds)*time.Second {
		return m.State, signals, false
	}
	m.State = desired
	m.candidate = ""
	m.lastNotification[desired] = now
	return m.State, signals, true
}
func (m *PressureMachine) signals(s MachineResourceSnapshot) []PressureSignal {
	signals := []PressureSignal{}
	check := func(name string, value float64, p ThresholdPair, low bool) {
		// Hysteresis: exiting an active state requires 20% headroom.
		elevated, critical := p.Elevated, p.Critical
		if pressureRank(m.State) >= 1 {
			if low {
				elevated *= 1.2
			} else {
				elevated *= 0.8
			}
		}
		if m.State == "critical" {
			if low {
				critical *= 1.2
			} else {
				critical *= 0.8
			}
		}
		state := ""
		threshold := elevated
		if (low && value <= critical) || (!low && value >= critical) {
			state = "critical"
			threshold = critical
		} else if (low && value <= elevated) || (!low && value >= elevated) {
			state = "elevated"
		}
		if state != "" {
			signals = append(signals, PressureSignal{name, value, threshold, state})
		}
	}
	c := m.Config
	if s.Memory != nil && s.Memory.TotalBytes > 0 {
		check("memory.available_percent", 100*float64(s.Memory.AvailableBytes)/float64(s.Memory.TotalBytes), c.AvailableMemoryPercent, true)
	}
	if s.CPU != nil && s.CPU.UtilizationPercent != nil {
		check("cpu.busy_percent", *s.CPU.UtilizationPercent, c.CPUBusyPercent, false)
	}
	if s.PSI != nil {
		for _, x := range []struct {
			name string
			a    *PressureAverages
			p    ThresholdPair
		}{{"psi.cpu.some.avg10", s.PSI.CPU.Some, c.CPUSome}, {"psi.memory.some.avg10", s.PSI.Memory.Some, c.MemorySome}, {"psi.memory.full.avg10", s.PSI.Memory.Full, c.MemoryFull}, {"psi.io.full.avg10", s.PSI.IO.Full, c.IOFull}} {
			if x.a != nil {
				check(x.name, x.a.Avg10, x.p, false)
			}
		}
	}
	for _, d := range s.Disks {
		if d.TotalBytes > 0 {
			check("disk.free_percent:"+d.Mount, 100*float64(d.FreeBytes)/float64(d.TotalBytes), c.DiskFreePercent, true)
		}
	}
	return signals
}
