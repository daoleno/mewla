package work

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/opencodeapi"
)

var updateToolGolden = flag.Bool("update-tool-golden", false, "rewrite testdata/tooldisplay/*.golden.json from the recorded transcripts")

// The recorded transcripts are real turns of each agent (owner 11-27), trimmed and scrubbed (home paths, private
// addresses and tokens), read by the agent's own reader and the wire projection, as the apps receive them.
func TestRecordedToolCallsReadTheSameWayForEveryAgent(t *testing.T) {
	dir := filepath.Join("testdata", "tooldisplay")
	cases := []struct {
		name string
		load func() (CodexConversation, error)
	}{
		{"claude", func() (CodexConversation, error) {
			return parseClaudeConversationFull(filepath.Join(dir, "claude.jsonl"))
		}},
		{"brain", func() (CodexConversation, error) {
			return parseClaudeConversationFull(filepath.Join(dir, "brain.jsonl"))
		}},
		{"codex", func() (CodexConversation, error) { return parseCodexConversation(filepath.Join(dir, "codex.jsonl")) }},
		{"cursor", func() (CodexConversation, error) { return parseCursorConversation(filepath.Join(dir, "cursor.jsonl")) }},
		{"pi", func() (CodexConversation, error) { return parsePiConversation(filepath.Join(dir, "pi.jsonl")) }},
		{"grok", func() (CodexConversation, error) { return buildGrokConversation(filepath.Join(dir, "grok"), nil) }},
		{"opencode", func() (CodexConversation, error) {
			var raws []json.RawMessage
			if err := readJSONFile(filepath.Join(dir, "opencode-messages.json"), &raws); err != nil {
				return CodexConversation{}, err
			}
			messages := make([]opencodeapi.Message, 0, len(raws))
			for _, raw := range raws {
				messages = append(messages, opencodeapi.Message{Raw: raw})
			}
			return buildOpenCodeConversation("ses_recorded", messages), nil
		}},
		{"antigravity", func() (CodexConversation, error) {
			var updates []antigravityACPUpdate
			if err := readJSONFile(filepath.Join(dir, "agy-updates.json"), &updates); err != nil {
				return CodexConversation{}, err
			}
			return projectAntigravityConversation("recorded", "/home/me/workspace/mewla", "", updates), nil
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			conversation, err := test.load()
			if err != nil {
				t.Fatal(err)
			}
			conversation = SanitizeConversationProjection(conversation)
			var got []recordedTool
			for _, event := range conversation.Events {
				if event.Tool == nil {
					continue
				}
				got = append(got, recordedTool{Call: firstNonEmpty(event.ToolName, event.Kind), Tool: *event.Tool})
				// Every call names what it worked on: no bare "Run" or "Read" (AUDIT.md P4–P7, C1–C3).
				switch event.Tool.Kind {
				case ToolKindWait, ToolKindPlan:
				default:
					if strings.TrimSpace(event.Tool.Target) == "" && event.Tool.Name == "" {
						t.Errorf("%s call %q (%s) has no target", test.name, event.ToolName, event.Kind)
					}
				}
				if event.Tool.Kind == ToolKindRead && strings.Contains(event.Tool.Target, ">") {
					t.Errorf("%s read target is a redirect: %q", test.name, event.Tool.Target)
				}
			}
			if len(got) < 5 {
				t.Fatalf("%s: only %d tool calls read from the recording", test.name, len(got))
			}
			golden := filepath.Join(dir, test.name+".golden.json")
			encoded, _ := json.MarshalIndent(got, "", "  ")
			encoded = append(encoded, '\n')
			if *updateToolGolden {
				if err := os.WriteFile(golden, encoded, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%v (run with -update-tool-golden)", err)
			}
			if string(want) != string(encoded) {
				t.Errorf("%s tools differ from %s (run with -update-tool-golden and review the diff):\n%s", test.name, golden, firstDifference(string(want), string(encoded)))
			}
		})
	}
}

type recordedTool struct {
	Call string           `json:"call"`
	Tool ConversationTool `json:"tool"`
}

func readJSONFile(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}

func firstDifference(want, got string) string {
	wantLines, gotLines := strings.Split(want, "\n"), strings.Split(got, "\n")
	for index := 0; index < len(wantLines) || index < len(gotLines); index++ {
		var w, g string
		if index < len(wantLines) {
			w = wantLines[index]
		}
		if index < len(gotLines) {
			g = gotLines[index]
		}
		if w != g {
			return "line " + itoa(index+1) + ":\n  want " + w + "\n  got  " + g
		}
	}
	return ""
}

func TestCommandToolReadsWhatTheCommandDoes(t *testing.T) {
	cases := []struct {
		command string
		want    ConversationTool
	}{
		// Setup is not what a command does (AUDIT.md C2).
		{"cd /home/me/w && git add a.json b.json && git commit -q -m \"Notes\"", ConversationTool{Kind: ToolKindCommand, Target: "git add a.json b.json && git commit -q -m \"Notes\""}},
		// A token fetched by a command is no secret; the row keeps its words (C1).
		{"export GH_TOKEN=$(gh auth token -u me); gh run view 4576 --repo me/x", ConversationTool{Kind: ToolKindCommand, Target: "gh run view 4576 --repo me/x"}},
		// A redirect is not a file (C3).
		{"cat SKILL.md 2>/dev/null", ConversationTool{Kind: ToolKindRead, Target: "SKILL.md"}},
		{"sed -n 1,80p daemon/work/a.go", ConversationTool{Kind: ToolKindRead, Target: "daemon/work/a.go", Result: "lines 1–80"}},
		{"sed -i 's/a/b/' x.go", ConversationTool{Kind: ToolKindCommand, Target: "sed -i 's/a/b/' x.go"}},
		{"rg -n \"computer use needs\" cmd/ | head -20", ConversationTool{Kind: ToolKindSearch, Target: "computer use needs", Scope: "cmd/"}},
		{"ls -la worklog/x", ConversationTool{Kind: ToolKindList, Target: "worklog/x"}},
		// A heredoc's body is not its name; writing one is an edit.
		{"W=$(cat $TMPDIR/w); cat > $W/dl.py <<'EOF'\nimport sys\nEOF", ConversationTool{Kind: ToolKindEdit, Target: "$W/dl.py"}},
		// A quoted argument decides nothing (C2).
		{"mewla worker send -id mh-1 -text \"grep for it\"", ConversationTool{Kind: ToolKindCommand, Target: "mewla worker send -id mh-1 -text \"grep for it\""}},
		{"head -12 a.go; grep -rln x client", ConversationTool{Kind: ToolKindCommand, Target: "head -12 a.go; grep -rln x client"}},
	}
	for _, test := range cases {
		got := commandTool(test.command)
		if got != test.want {
			t.Errorf("commandTool(%q)\n got  %+v\n want %+v", test.command, got, test.want)
		}
	}
}

func TestNamedToolReadsEveryAgentsSpelling(t *testing.T) {
	cases := []struct {
		name, input string
		want        ConversationTool
	}{
		{"StrReplace", `{"path":"/w/a.go","old_string":"x\ny","new_string":"z"}`, ConversationTool{Kind: ToolKindEdit, Target: "/w/a.go"}},
		{"search_replace", `{"file_path":"/w/a.go","old_string":"x","new_string":"y\nz"}`, ConversationTool{Kind: ToolKindEdit, Target: "/w/a.go"}},
		{"run_terminal_command", `{"command":"go test ./watcher/"}`, ConversationTool{Kind: ToolKindCommand, Target: "go test ./watcher/"}},
		{"Execute `tmux -S /tmp/s capture-pane -p`", ``, ConversationTool{Kind: ToolKindCommand, Target: "tmux -S /tmp/s capture-pane -p"}},
		{"Grep", `{"pattern":"foo","path":"daemon"}`, ConversationTool{Kind: ToolKindSearch, Target: "foo", Scope: "daemon"}},
		{"WebSearch", `{"search_term":"deepseek responses"}`, ConversationTool{Kind: ToolKindWebSearch, Target: "deepseek responses"}},
		{"sleep", `{"seconds":60}`, ConversationTool{Kind: ToolKindWait}},
		{"mcp__mewla_chrome__browser_click", `{"element":"Sign in"}`, ConversationTool{Kind: ToolKindBrowser, Name: "Click", Server: "mewla_chrome", Target: "Sign in"}},
		{"GetMcpTools", `{}`, ConversationTool{Kind: ToolKindOther, Name: "Get mcp tools"}},
		{"exec_command", "text(await tools.exec_command({cmd:`git status --short`}))", ConversationTool{Kind: ToolKindCommand, Target: "git status --short"}},
		{"exec_command", `text(await Promise.allSettled([["a","git log -3"],["b","ls docs"]]))`, ConversationTool{Kind: ToolKindCommand, Target: "git log -3", More: 1}},
		// A Write cut short by the reader keeps its path.
		{"Write", `{"path":"/w/b_test.go","contents":"package b\n\nimport (\n\t\"by`, ConversationTool{Kind: ToolKindEdit, Target: "/w/b_test.go"}},
	}
	for _, test := range cases {
		got := namedTool(test.name, CodexConversationEvent{Input: test.input})
		if got != test.want {
			t.Errorf("namedTool(%q)\n got  %+v\n want %+v", test.name, got, test.want)
		}
	}
}

func TestConversationToolResultStatusAndTime(t *testing.T) {
	one := 1
	event := CodexConversationEvent{Kind: "command", Command: "go test ./x", ExitCode: &one, Status: "failed", Timestamp: "2026-11-26T12:00:00Z", Source: claudeConversationSource, Body: "Run the tests"}
	event.settleTool("2026-11-26T12:00:02.5Z")
	AnnotateConversationTool(&event)
	want := ConversationTool{Kind: ToolKindCommand, Target: "go test ./x", Intent: "Run the tests", Status: ToolStatusFailed, Result: "exit 1", DurationMS: 2500}
	if event.Tool == nil || *event.Tool != want {
		t.Fatalf("tool = %+v, want %+v", event.Tool, want)
	}
	if event.Timestamp != "2026-11-26T12:00:02.5Z" {
		t.Fatalf("a settled call is placed at its result, as before: %s", event.Timestamp)
	}
	edit := CodexConversationEvent{Kind: "tool", ToolName: "Edit", Input: `{"file_path":"a.go","old_string":"a\nb","new_string":"c\nd\ne"}`, Status: "done"}
	AnnotateConversationTool(&edit)
	if edit.Tool.Added == nil || *edit.Tool.Added != 3 || *edit.Tool.Removed != 2 {
		t.Fatalf("edit counts = %+v", edit.Tool)
	}
	// A literal credential never reaches the row.
	secret := CodexConversationEvent{Kind: "command", Command: "curl -H 'Authorization: Bearer abcdefghijklmnopqrstuvwxyz' https://x.test", Status: "done"}
	AnnotateConversationTool(&secret)
	if strings.Contains(secret.Tool.Target, "abcdefghijklmnop") {
		t.Fatalf("secret kept: %q", secret.Tool.Target)
	}
	// A result whose call the reader never saw says nothing about what ran.
	orphan := CodexConversationEvent{Kind: "tool", ToolName: "tool", Output: "ok"}
	AnnotateConversationTool(&orphan)
	if orphan.Tool != nil {
		t.Fatalf("orphan result got a tool: %+v", orphan.Tool)
	}
}
