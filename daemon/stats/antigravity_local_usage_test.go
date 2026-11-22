package stats

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/statedir"
)

func TestAntigravitySessionLogUsageFlowsIntoStats(t *testing.T) {
	setTestLocalLocation(t, time.UTC)
	home := t.TempDir()
	root := filepath.Join(statedir.Default(home), "provider-sessions", "antigravity")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	log := strings.Join([]string{
		`{"t":"session","at":"2026-11-22T05:20:29Z","conversation":"c1","model":"gemini-4-neon-low","effort":"low","cwd":"/home/u/proj/site"}`,
		`{"t":"user","at":"2026-11-22T05:20:30Z","turn":"c1:1","text":"secret words"}`,
		`{"t":"update","at":"2026-11-22T05:20:31Z","turn":"c1:1","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"more"}}}`,
		`{"t":"turn_end","at":"2026-11-22T05:20:32Z","turn":"c1:1","stop":"end_turn","usage":{"inputTokens":18031,"outputTokens":876,"cachedReadTokens":22096,"thoughtTokens":428,"totalTokens":41003}}`,
		// A model change from the app, then a turn on it.
		`{"t":"session","at":"2026-11-22T05:21:00Z","conversation":"c1","model":"gemini-4-argon-low"}`,
		`{"t":"user","at":"2026-11-22T05:21:01Z","turn":"c1:2","text":"x"}`,
		`{"t":"turn_end","at":"2026-11-22T05:21:05Z","turn":"c1:2","stop":"end_turn","usage":{"inputTokens":100,"outputTokens":20,"cachedReadTokens":0,"thoughtTokens":5,"totalTokens":120}}`,
		// A turn cancelled before agy reported usage counts nothing.
		`{"t":"turn_end","at":"2026-11-22T05:22:00Z","turn":"c1:3","stop":"cancelled"}`,
		`{"t":"turn_end","at":"2026-11-22T05:21:05Z","turn":"c1:2","stop":"end_turn","usage":{"inputTokens":100,"outputTokens":20,"totalTokens":120}}`,
		`{"t":"turn_end","at":"2026-11-22T05:2`,
	}, "\n")
	if err := os.WriteFile(filepath.Join(root, "c1.jsonl"), []byte(log), 0o600); err != nil {
		t.Fatal(err)
	}

	byDate := (&Collector{}).collectAntigravityStats(home)
	day := byDate["2026-11-22"]
	if day == nil {
		t.Fatal("missing the agy day")
	}
	neon := day.models[modelAggKey("antigravity", "gemini-4-neon-low")]
	if neon.totalTokens != 41003 || neon.inputTokens != 18031 || neon.outputTokens != 876 || neon.cacheRead != 22096 || neon.reasoning != 428 {
		t.Fatalf("neon = %+v", neon)
	}
	if !neon.costUnknown || neon.estimated {
		t.Fatalf("agy bills through the Google plan; no tariff: %+v", neon)
	}
	argon := day.models[modelAggKey("antigravity", "gemini-4-argon-low")]
	if argon.totalTokens != 120 {
		t.Fatalf("a repeated turn_end counts once; argon = %+v", argon)
	}
	if project := day.projects["site"]; project == nil || project.totalTokens != 41123 {
		t.Fatalf("project from the session cwd = %+v", day.projects)
	}
	for _, model := range buildModelStats(aggregateModelsByDate(byDate, "0000-00-00", "9999-99-99")) {
		if model.CostKnown || model.UnpricedReason != "plan" || model.Provider != "antigravity" {
			t.Fatalf("agy model stat = %+v", model)
		}
	}
}
