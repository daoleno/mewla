// Package procspawn creates the daemon's recurring child processes and counts
// them by call site. A daemon that forks while nothing is happening is the
// cost this package exists to make visible: the counts are served at
// /debug/spawns when the loopback diagnostics listener is on, and tests read
// them to prove that an idle loop stays quiet.
package procspawn

import (
	"context"
	"encoding/json"
	"net/http"
	"os/exec"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

var (
	mu      sync.RWMutex
	counts  = map[string]*atomic.Int64{}
	started = time.Now()
)

func count(label string) {
	mu.RLock()
	n, ok := counts[label]
	mu.RUnlock()
	if !ok {
		mu.Lock()
		if n, ok = counts[label]; !ok {
			n = new(atomic.Int64)
			counts[label] = n
		}
		mu.Unlock()
	}
	n.Add(1)
}

// Command is exec.Command that counts one spawn under label.
func Command(label, name string, args ...string) *exec.Cmd {
	count(label)
	return exec.Command(name, args...)
}

// CommandContext is exec.CommandContext that counts one spawn under label.
func CommandContext(ctx context.Context, label, name string, args ...string) *exec.Cmd {
	count(label)
	return exec.CommandContext(ctx, name, args...)
}

// Snapshot returns the spawn count of every label seen so far.
func Snapshot() map[string]int64 {
	mu.RLock()
	defer mu.RUnlock()
	out := make(map[string]int64, len(counts))
	for label, n := range counts {
		out[label] = n.Load()
	}
	return out
}

// Delta returns how many spawns happened per label between two snapshots.
func Delta(before, after map[string]int64) map[string]int64 {
	out := map[string]int64{}
	for label, n := range after {
		if d := n - before[label]; d > 0 {
			out[label] = d
		}
	}
	return out
}

// Total is the sum of all counts in a snapshot.
func Total(s map[string]int64) int64 {
	var total int64
	for _, n := range s {
		total += n
	}
	return total
}

// Handler serves the counts as JSON, largest first, with the seconds since
// the daemon started so a reader can turn them into a rate.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		snap := Snapshot()
		type row struct {
			Label string `json:"label"`
			Count int64  `json:"count"`
		}
		rows := make([]row, 0, len(snap))
		for label, n := range snap {
			rows = append(rows, row{label, n})
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].Count > rows[j].Count })
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"uptime_seconds": int64(time.Since(started).Seconds()),
			"total":          Total(snap),
			"spawns":         rows,
		})
	})
}

// Label names a tmux spawn by its subcommand, skipping any leading flags.
func Label(program string, args []string) string {
	for _, arg := range args {
		if len(arg) > 0 && arg[0] != '-' {
			return program + " " + arg
		}
	}
	return program
}
