package watcher

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func pressureFixture(available uint64) MachineResourceSnapshot {
	return MachineResourceSnapshot{Memory: &MemoryResource{TotalBytes: 1000, AvailableBytes: available}}
}
func TestPressureDurationHysteresisEscalationRecoveryCooldown(t *testing.T) {
	now := time.Unix(1000, 0)
	m := NewPressureMachine(DefaultResourceThresholds())
	step := func(seconds int, available uint64, want PressureState, event bool) {
		t.Helper()
		state, _, notify := m.Step(pressureFixture(available), now.Add(time.Duration(seconds)*time.Second))
		if state != want || notify != event {
			t.Fatalf("t=%d available=%d state=%s event=%v", seconds, available, state, notify)
		}
	}
	step(0, 140, "normal", false)
	step(19, 140, "normal", false)
	step(20, 140, "elevated", true)
	step(21, 160, "elevated", false)
	step(60, 160, "elevated", false) // 16% is below recovery headroom.
	step(61, 60, "elevated", false)
	step(81, 60, "critical", true)
	step(82, 200, "critical", false)
	step(111, 200, "critical", false)
	step(112, 200, "normal", true)
	step(113, 60, "normal", false)
	step(133, 60, "normal", false)
	step(141, 60, "critical", true) // same-state cooldown
	step(142, 200, "critical", false)
	step(172, 200, "normal", true)
	step(300, 200, "normal", false)
}
func TestPressureBriefSpikeAndMissingMetricsDoNotAlert(t *testing.T) {
	m := NewPressureMachine(DefaultResourceThresholds())
	now := time.Now()
	m.Step(pressureFixture(50), now)
	state, _, notify := m.Step(pressureFixture(500), now.Add(10*time.Second))
	if state != "normal" || notify {
		t.Fatal(state, notify)
	}
	m.Step(MachineResourceSnapshot{}, now.Add(time.Minute))
	if m.State != "normal" {
		t.Fatal(m.State)
	}
}
func TestPressureUsesPSICPUAndDisk(t *testing.T) {
	cpu := 99.0
	s := MachineResourceSnapshot{CPU: &CPUResource{UtilizationPercent: &cpu}, Memory: &MemoryResource{TotalBytes: 1000, AvailableBytes: 50}, PSI: &PressureSnapshot{Memory: PressureResource{Full: &PressureAverages{Avg60: 6}}}, Disks: []DiskResource{{Mount: "/", TotalBytes: 1000, FreeBytes: 10}}}
	m := NewPressureMachine(DefaultResourceThresholds())
	now := time.Now()
	m.Step(s, now)
	state, signals, notify := m.Step(s, now.Add(20*time.Second))
	if state != "critical" || !notify || len(signals) != 4 {
		t.Fatal(state, signals, notify)
	}
}

// An hourly container that hits its own cgroup limit stalls reclaim for a few
// seconds while the host keeps over half of 30.5 GiB available. Those readings
// crossed the memory PSI thresholds overnight and must stay quiet now.
func TestMemoryPSIIdleContainerStallDoesNotAlert(t *testing.T) {
	m := NewPressureMachine(DefaultResourceThresholds())
	now := time.Now()
	idleStall := func() MachineResourceSnapshot {
		return MachineResourceSnapshot{
			Memory: &MemoryResource{TotalBytes: 30500 << 20, AvailableBytes: 16500 << 20},
			PSI: &PressureSnapshot{Memory: PressureResource{
				Some: &PressureAverages{Avg10: 9.6, Avg60: 8.9, Avg300: 4.2},
				Full: &PressureAverages{Avg10: 9.1, Avg60: 8.4, Avg300: 3.9},
			}},
		}
	}
	m.Step(idleStall(), now)
	state, signals, notify := m.Step(idleStall(), now.Add(20*time.Second))
	if state != "normal" || notify || len(signals) != 0 {
		t.Fatalf("idle container stall alerted: state=%s notify=%v signals=%v", state, notify, signals)
	}
}

// Sustained host memory pressure with little memory left still alerts.
func TestMemoryPSIRealPressureAlerts(t *testing.T) {
	m := NewPressureMachine(DefaultResourceThresholds())
	now := time.Now()
	realPressure := func() MachineResourceSnapshot {
		return MachineResourceSnapshot{
			Memory: &MemoryResource{TotalBytes: 30500 << 20, AvailableBytes: 900 << 20},
			PSI: &PressureSnapshot{Memory: PressureResource{
				Some: &PressureAverages{Avg10: 30, Avg60: 25, Avg300: 12},
				Full: &PressureAverages{Avg10: 28, Avg60: 22, Avg300: 10},
			}},
		}
	}
	m.Step(realPressure(), now)
	state, signals, notify := m.Step(realPressure(), now.Add(20*time.Second))
	if state != "critical" || !notify {
		t.Fatalf("real pressure stayed quiet: state=%s notify=%v signals=%v", state, notify, signals)
	}
	found := map[string]bool{}
	for _, signal := range signals {
		found[signal.Name] = true
	}
	for _, name := range []string{"memory.available_percent", "psi.memory.some.avg60", "psi.memory.full.avg60"} {
		if !found[name] {
			t.Fatalf("missing %s in %v", name, signals)
		}
	}
}

func TestPressureStateIsClosedAndOrdered(t *testing.T) {
	for _, state := range []PressureState{PressureNormal, PressureElevated, PressureCritical} {
		if !state.Valid() {
			t.Errorf("%q is not valid", state)
		}
	}
	for _, state := range []PressureState{"", "warn", "Normal", "severe"} {
		if state.Valid() {
			t.Errorf("%q is valid", state)
		}
	}
	if !(PressureNormal.rank() < PressureElevated.rank() && PressureElevated.rank() < PressureCritical.rank()) {
		t.Error("pressure states are not ordered normal < elevated < critical")
	}
	// The states are the words the persisted file and Brain's payload carry.
	raw, err := json.Marshal(ResourcePressureEvent{State: PressureElevated, PreviousState: PressureNormal})
	if err != nil || !strings.Contains(string(raw), `"state":"elevated"`) || !strings.Contains(string(raw), `"previous_state":"normal"`) {
		t.Fatalf("wire form = %s (%v)", raw, err)
	}
}
