package stats

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func rchar(t *testing.T) uint64 {
	raw, _ := os.ReadFile("/proc/self/io")
	for _, l := range strings.Split(string(raw), "\n") {
		if v, ok := strings.CutPrefix(l, "rchar: "); ok {
			n, _ := strconv.ParseUint(strings.TrimSpace(v), 10, 64)
			return n
		}
	}
	return 0
}

func TestZZ(t *testing.T) {
	home := os.Getenv("ZZHOME")
	if home == "" {
		t.Skip()
	}
	c := NewCollector()
	for i := 0; i < 3; i++ {
		var parts []string
		step := func(name string, f func()) {
			a, s := rchar(t), time.Now()
			f()
			parts = append(parts, name+"="+strconv.FormatUint((rchar(t)-a)>>20, 10)+"MB/"+time.Since(s).Round(time.Millisecond).String())
		}
		step("claude", func() { c.collectClaudeSessionStats(home) })
		step("grok", func() { c.collectGrokStats(home) })
		step("cursor", func() { c.collectCursorAgentStats(home) })
		step("opencode", func() { c.collectOpenCodeStats() })
		step("pi", func() { c.collectPiStats(home) })
		step("codex", func() { c.collectCodexStats(home) })
		t.Log(strings.Join(parts, " "))
	}
}
