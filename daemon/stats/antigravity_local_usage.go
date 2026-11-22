package stats

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/daoleno/mewla/daemon/statedir"
)

// Antigravity (agy) usage comes from the logs of the Sessions Mewla runs
// over ACP (<state>/provider-sessions/antigravity/<conversation>.jsonl,
// written by `mewla agy-session`): every turn_end carries the token count agy
// reported for the turn. agy's own conversation store is not read. Counts
// follow agy's report: totalTokens = input + output + cache-read, and
// thoughtTokens is a subset of output. agy bills through the person's Google
// plan, so no tariff is applied (cost stays unknown), whichever model family
// the turn ran on. Only these metadata fields are decoded; message text is
// never read into Stats.

type antigravityUsage struct {
	InputTokens      int64 `json:"inputTokens"`
	OutputTokens     int64 `json:"outputTokens"`
	CachedReadTokens int64 `json:"cachedReadTokens"`
	CachedWriteToken int64 `json:"cachedWriteTokens"`
	ThoughtTokens    int64 `json:"thoughtTokens"`
	TotalTokens      int64 `json:"totalTokens"`
}

type antigravityLogLine struct {
	T     string            `json:"t"`
	At    string            `json:"at"`
	Turn  string            `json:"turn"`
	Model string            `json:"model"`
	Cwd   string            `json:"cwd"`
	Usage *antigravityUsage `json:"usage"`
}

const antigravityStatsProvider = "antigravity"

func (c *Collector) collectAntigravityStats(home string) map[string]*dateAgg {
	byDate := make(map[string]*dateAgg)
	root := filepath.Join(statedir.Default(home), "provider-sessions", "antigravity")
	files, err := os.ReadDir(root)
	if err != nil {
		return byDate
	}
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".jsonl") {
			continue
		}
		scanAntigravityLogCached(filepath.Join(root, file.Name()), byDate, c.usageCacheDir)
	}
	return byDate
}

func scanAntigravityLogCached(path string, byDate map[string]*dateAgg, cacheDir string) {
	lines, err := cachedUsageFile(cacheDir, "antigravity", path, func() ([]antigravityLogLine, error) {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
		lines := []antigravityLogLine{}
		for scanner.Scan() {
			raw := scanner.Bytes()
			// Only session and turn_end records matter (the program writes
			// "t" first); the rest are skipped undecoded, as updates carry
			// message text.
			if !bytes.HasPrefix(raw, []byte(`{"t":"session"`)) && !bytes.HasPrefix(raw, []byte(`{"t":"turn_end"`)) {
				continue
			}
			var line antigravityLogLine
			if json.Unmarshal(raw, &line) != nil {
				continue
			}
			lines = append(lines, line)
		}
		return lines, scanner.Err()
	})
	if err != nil {
		return
	}
	model, cwd := "", ""
	seen := map[string]bool{}
	var records []piLedgerRecord
	for _, line := range lines {
		switch line.T {
		case "session":
			model = firstNonEmptyString(strings.TrimSpace(line.Model), model)
			cwd = firstNonEmptyString(strings.TrimSpace(line.Cwd), cwd)
		case "turn_end":
			if line.Usage == nil || model == "" || (line.Turn != "" && seen[line.Turn]) {
				continue
			}
			seen[line.Turn] = true
			usage := line.Usage
			record, ok := piRecordFromUsage(line.At, antigravityStatsProvider, model, &piUsage{
				Input: usage.InputTokens, Output: usage.OutputTokens, CacheRead: usage.CachedReadTokens,
				CacheWrite: usage.CachedWriteToken, Reasoning: &usage.ThoughtTokens, TotalTokens: usage.TotalTokens,
			})
			if ok {
				records = append(records, record)
			}
		}
	}
	if len(records) > 0 {
		addLedgerRecords(byDate, records, piProjectName(cwd, ""))
	}
}
