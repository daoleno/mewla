package agentuse

import (
	"encoding/json"
	"strings"
	"testing"
)

const sampleSnapshot = "### Page\n- Page URL: http://x.test/\n### Snapshot\n```yaml\n" +
	"- generic [active] [ref=e1]:\n" +
	"  - navigation [ref=e2]:\n" +
	"    - link \"Home\" [ref=e3] [cursor=pointer]:\n" +
	"      - /url: /\n" +
	"  - heading \"Inbox\" [level=1] [ref=e4]\n" +
	"  - paragraph [ref=e5]: Lots of body text that an agent does not need to act\n" +
	"  - textbox \"Search\" [ref=e6]: hello\n" +
	"  - checkbox \"Agree\" [checked] [ref=e7]\n" +
	"  - button \"Send\" [disabled] [ref=e8] [cursor=pointer]\n" +
	"  - text: loose text\n" +
	"```\n"

func TestAnInteractiveSnapshotKeepsControlsAndHeadingsWithTheirRefs(t *testing.T) {
	got := compactSnapshot(sampleSnapshot)
	for _, want := range []string{
		"### Page\n- Page URL: http://x.test/\n### Snapshot\n```yaml\n",
		"- link \"Home\" [ref=e3]\n",
		"- heading \"Inbox\" [level=1] [ref=e4]\n",
		"- textbox \"Search\" [ref=e6]: hello\n",
		"- checkbox \"Agree\" [checked] [ref=e7]\n",
		"- button \"Send\" [disabled] [ref=e8]\n",
		"(Controls and headings only: 5 of 8 elements.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in\n%s", want, got)
		}
	}
	for _, gone := range []string{"paragraph", "navigation", "generic", "cursor=pointer", "/url", "loose text"} {
		if strings.Contains(got, gone) {
			t.Fatalf("%q should be gone from\n%s", gone, got)
		}
	}
	if text := "### Error\nsomething broke"; compactSnapshot(text) != text {
		t.Fatal("text that is not a snapshot must pass unchanged")
	}
}

func TestTheInteractiveArgumentIsTakenOutBeforePlaywrightSeesIt(t *testing.T) {
	line := []byte(`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"browser_snapshot","arguments":{"interactive":true,"depth":3}}}` + "\n")
	out, interactive := takeInteractive(line)
	if !interactive {
		t.Fatal("interactive: true not noticed")
	}
	var msg struct {
		Params struct {
			Arguments map[string]any `json:"arguments"`
		} `json:"params"`
	}
	if err := json.Unmarshal(out, &msg); err != nil || len(msg.Params.Arguments) != 1 || msg.Params.Arguments["depth"] != float64(3) {
		t.Fatalf("arguments after: %s (%v)", out, err)
	}
	if out[len(out)-1] != '\n' {
		t.Fatal("a message is one line")
	}
	plain := []byte(`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"browser_snapshot","arguments":{"depth":3}}}` + "\n")
	if got, interactive := takeInteractive(plain); interactive || string(got) != string(plain) {
		t.Fatalf("a call without it is untouched: %s", got)
	}
	off := []byte(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"browser_snapshot","arguments":{"interactive":false}}}` + "\n")
	if got, interactive := takeInteractive(off); interactive || strings.Contains(string(got), "interactive") {
		t.Fatalf("interactive: false is removed and not applied: %s", got)
	}
}

func TestOnlyTheAnswerToAnInteractiveCallIsCompacted(t *testing.T) {
	s := &supervisor{compact: map[string]bool{"5": true}}
	answer := func(id int) []byte {
		raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"content": []any{map[string]any{"type": "text", "text": sampleSnapshot}}}})
		return append(raw, '\n')
	}
	if got := s.compacted(answer(6)); string(got) != string(answer(6)) {
		t.Fatal("another call's answer changed")
	}
	got := s.compacted(answer(5))
	if !strings.Contains(string(got), "Controls and headings only") || strings.Contains(string(got), "Lots of body text") {
		t.Fatalf("answer not compacted: %s", got)
	}
	if len(s.compact) != 0 {
		t.Fatal("the call stays marked after it is answered")
	}
}

func TestBrowserSnapshotAdvertisesTheInteractiveArgument(t *testing.T) {
	out := withInteractive(json.RawMessage(`{"type":"object","properties":{"depth":{"type":"number"}}}`))
	var schema struct {
		Properties map[string]map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(out, &schema); err != nil || schema.Properties["interactive"]["type"] != "boolean" || schema.Properties["depth"]["type"] != "number" {
		t.Fatalf("schema %s (%v)", out, err)
	}
	if bare := withInteractive(json.RawMessage(`{"type":"object"}`)); !strings.Contains(string(bare), `"interactive"`) {
		t.Fatalf("a schema with no properties gets one: %s", bare)
	}
}
