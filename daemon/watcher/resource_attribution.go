package watcher

import (
	"sort"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
)

func classifyProcess(command string) []string {
	c := strings.ToLower(command)
	kinds := []string{}
	switch {
	case strings.Contains(c, "qemu") || strings.Contains(c, "emulator"):
		kinds = append(kinds, "qemu")
	case strings.Contains(c, "java") || strings.Contains(c, "gradle"):
		kinds = append(kinds, "gradle")
	case strings.Contains(c, "chrome") || strings.Contains(c, "chromium"):
		kinds = append(kinds, "chrome")
	case strings.Contains(c, "node") || strings.Contains(c, "metro"):
		kinds = append(kinds, "node")
	}
	return kinds
}
func attributeConsumers(consumers []ProcessConsumer, w *Watcher) []ProcessConsumer {
	byID := map[string]*classifier.Worker{}
	byToken := map[string]*classifier.Worker{}
	if w != nil {
		manager := w.resourceManager()
		for _, worker := range w.Workers() {
			if worker == nil {
				continue
			}
			byID[worker.ID] = worker
			if token := manager.UnitForTarget(worker.ID); token != "" {
				byToken[token] = worker
			}
		}
	}
	for i := range consumers {
		c := &consumers[i]
		worker := byToken[c.ID]
		if worker == nil {
			worker = byID[c.WorkerID]
		}
		if worker == nil {
			if c.Owner == "worker" {
				c.Owner = "orphaned_worker"
				c.Status = "unknown"
			}
			continue
		}
		c.WorkerID = worker.ID
		c.Title = worker.Name
		c.Cwd = worker.Cwd
		c.Status = string(worker.State)
		c.Executor = normalizeCommand(worker.Command)
		if worker.Hidden {
			c.Owner = "brain"
		}
		if !worker.StartedAt.IsZero() {
			c.AgeSeconds = int64(time.Since(worker.StartedAt).Seconds())
		}
		if !worker.Hidden && (worker.State == classifier.StateDone || worker.State == classifier.StateRemoved) {
			c.Owner = "orphaned_worker"
		}
	}
	sort.Slice(consumers, func(i, j int) bool {
		a, b := consumers[i], consumers[j]
		var cpuA, cpuB float64
		if a.CPUPercent != nil {
			cpuA = *a.CPUPercent
		}
		if b.CPUPercent != nil {
			cpuB = *b.CPUPercent
		}
		scoreA := float64(a.RSSBytes)/(1024*1024) + cpuA*10
		scoreB := float64(b.RSSBytes)/(1024*1024) + cpuB*10
		if scoreA == scoreB {
			return a.ID < b.ID
		}
		return scoreA > scoreB
	})
	// Keep every attributed Mewla owner; bound only unowned user process groups.
	out := []ProcessConsumer{}
	others := 0
	for _, c := range consumers {
		if c.Owner == "user" || c.Owner == "docker" {
			if others >= 20 {
				continue
			}
			others++
		}
		out = append(out, c)
		if len(out) >= 64 {
			break
		}
	}
	return out
}
