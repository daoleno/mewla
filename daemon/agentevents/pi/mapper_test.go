package pi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/agentevents"
)

func mapLines(t *testing.T, lines ...string) []agentevents.Event {
	t.Helper()
	m := NewMapper("w1")
	var out []agentevents.Event
	at := time.Unix(1000, 0)
	for _, line := range lines {
		msg, err := ParseMessage([]byte(line))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, m.Map(msg, at)...)
		at = at.Add(time.Millisecond)
	}
	return out
}

func TestACompletedTurnFromARealRun(t *testing.T) {
	events := mapLines(t,
		`{"t":"input","source":"extension","sha":"abc"}`,
		`{"t":"turn_start","turn":"s:1"}`,
		`{"t":"progress","turn":"s:1"}`,
		`{"t":"turn_end","turn":"s:1","aborted":false,"stop":"stop"}`,
	)
	got := []agentevents.EventKind{}
	for _, e := range events {
		got = append(got, e.Kind)
	}
	want := []agentevents.EventKind{agentevents.KindUserMessage, agentevents.KindTurnStarted, agentevents.KindProgress, agentevents.KindTurnFinished}
	if len(got) != len(want) {
		t.Fatalf("kinds = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("kinds = %v, want %v", got, want)
		}
	}
	if events[0].InputSHA256 != "abc" || events[3].TurnID != "s:1" || events[3].Status != agentevents.TurnCompleted {
		t.Fatalf("events = %+v", events)
	}
}

func TestAbortedAndFailedTurnsAreToldApart(t *testing.T) {
	aborted := mapLines(t, `{"t":"turn_end","turn":"s:2","aborted":true,"stop":"error","error":"This operation was aborted"}`)
	if aborted[0].Status != agentevents.TurnInterrupted || aborted[0].Error != nil {
		t.Fatalf("aborted = %+v", aborted[0])
	}
	failed := mapLines(t, `{"t":"turn_end","turn":"s:3","aborted":false,"stop":"error","error":"429 rate limit, top up your credits"}`)
	if failed[0].Status != agentevents.TurnFailed || failed[0].Error == nil ||
		failed[0].Error.Class != agentevents.ErrorUnknown || !strings.Contains(failed[0].Error.Message, "429") {
		t.Fatalf("failed = %+v", failed[0])
	}
}

func TestBindingKnowsTheFileItWasLaunchedWith(t *testing.T) {
	msg := Message{T: "hello", Session: "/s/a.jsonl", ID: "uuid-1"}
	if b := Binding(msg, "/s/a.jsonl"); b.By != "launch" || b.ConversationID != "uuid-1" || b.Ref != "/s/a.jsonl" {
		t.Fatalf("binding = %+v", b)
	}
	if b := Binding(msg, "/s/other.jsonl"); b.By != "agent" {
		t.Fatalf("binding = %+v", b)
	}
}

func TestPrepareLaunchLoadsTheExtensionAndOwnsTheSession(t *testing.T) {
	a := Adapter{ExtensionPath: "/state/pi-extension.js"}
	launch, err := a.PrepareLaunch("pi")
	if err != nil || !strings.Contains(launch.Command, "--session /") || !strings.HasSuffix(launch.Command, " -e /state/pi-extension.js") {
		t.Fatalf("launch = %+v, %v", launch, err)
	}
	opted := "pi --no-session"
	if got, _ := a.PrepareLaunch(opted); got.Command != opted {
		t.Fatalf("a launch without a session was changed to %q", got.Command)
	}
	if got, _ := (Adapter{}).PrepareLaunch("pi"); strings.Contains(got.Command, " -e ") {
		t.Fatalf("no extension to load, yet %q", got.Command)
	}
}

func TestWriteExtensionBakesInTheSocketAndKeepsAnUnchangedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pi-extension.js")
	if err := WriteExtension(path, `/state/pi "events".sock`); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), `const SOCKET = "/state/pi \"events\".sock";`) || strings.Contains(string(body), "__MEWLA_SOCKET__") {
		t.Fatalf("extension = %q", string(body)[:200])
	}
	before, _ := os.Stat(path)
	time.Sleep(10 * time.Millisecond)
	if err := WriteExtension(path, `/state/pi "events".sock`); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(path)
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("an unchanged extension was rewritten")
	}
}
