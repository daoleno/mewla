package opencode

import (
	"bufio"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/agentevents"
)

// frames reads the data lines of a captured event stream.
func frames(t *testing.T, name string) []Frame {
	t.Helper()
	file, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var out []Frame
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		line, ok := strings.CutPrefix(scanner.Text(), "data: ")
		if !ok {
			continue
		}
		f, err := ParseFrame([]byte(line))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, f)
	}
	return out
}

func mapAll(m *Mapper, fs []Frame) []agentevents.Event {
	var out []agentevents.Event
	at := time.Unix(1000, 0)
	for _, f := range fs {
		out = append(out, m.Map(f, at)...)
		at = at.Add(time.Millisecond)
	}
	return out
}

func kinds(events []agentevents.Event) []agentevents.EventKind {
	out := make([]agentevents.EventKind, len(events))
	for i, e := range events {
		out[i] = e.Kind
	}
	return out
}

const session = "ses_e95b3bedeffeksoIfo0W13KI2z"

func TestToolTurnIsOneTurnWithItsMessageAndProgress(t *testing.T) {
	events := mapAll(NewMapper("w1", session), frames(t, "tool-turn.sse"))
	var msg, started, done *agentevents.Event
	progress := 0
	for i := range events {
		switch events[i].Kind {
		case agentevents.KindUserMessage:
			msg = &events[i]
		case agentevents.KindTurnStarted:
			started = &events[i]
		case agentevents.KindTurnFinished:
			done = &events[i]
		case agentevents.KindProgress:
			progress++
		}
	}
	if msg == nil || started == nil || done == nil {
		t.Fatalf("kinds = %v", kinds(events))
	}
	if msg.ClientID != "msg_mewla0002" || msg.InputSHA256 == "" || msg.InputSHA256 != msg.InputUnwrappedSHA256 {
		t.Fatalf("message = %+v", msg)
	}
	if started.TurnID == "" || done.TurnID != started.TurnID || done.Status != agentevents.TurnCompleted {
		t.Fatalf("turn started %+v finished %+v", started, done)
	}
	if progress < 4 {
		t.Fatalf("progress events = %d, want the steps and the tool call", progress)
	}
}

func TestPermissionIsAnApprovalWithTheServicesOwnOptions(t *testing.T) {
	m := NewMapper("w1", session)
	var asked, resolved *agentevents.Event
	events := mapAll(m, frames(t, "permission-turn.sse"))
	for i := range events {
		switch events[i].Kind {
		case agentevents.KindApprovalRequest:
			if asked == nil {
				asked = &events[i]
			}
		case agentevents.KindApprovalResolved:
			if resolved == nil {
				resolved = &events[i]
			}
		}
	}
	if asked == nil || asked.Approval == nil {
		t.Fatalf("kinds = %v", kinds(events))
	}
	a := asked.Approval
	if a.ID != "per_16a4f383e001ihcCYXGnAqKeBl" || a.Kind != "command" || a.Tool != "shell" ||
		a.Summary != "echo OC-PERM > /tmp/oc-proj/perm.txt" || a.ConversationID != session {
		t.Fatalf("approval = %+v", a)
	}
	if len(a.Options) != 3 || a.Options[0].ID != "once" || a.Options[2].ID != "reject" || a.Options[2].Scope != "deny" {
		t.Fatalf("options = %+v", a.Options)
	}
	if resolved == nil || resolved.ApprovalID != a.ID || resolved.By != "agent" || resolved.OptionID != "once" {
		t.Fatalf("resolved = %+v", resolved)
	}
	if m.Pending(a.ID) {
		t.Fatal("a replied approval is still pending")
	}
}

func TestInterruptAndFailureEndTheirTurns(t *testing.T) {
	all := frames(t, "interrupt-and-failure.sse")
	interrupted := mapAll(NewMapper("w1", session), all)
	var last agentevents.Event
	for _, e := range interrupted {
		if e.Kind == agentevents.KindTurnFinished {
			last = e
		}
	}
	if last.Status != agentevents.TurnInterrupted || last.TurnID == "" {
		t.Fatalf("interrupted turn = %+v", last)
	}

	other := "ses_e95b0162fffeIf33JZZ3FNOCUp"
	failed := mapAll(NewMapper("w2", other), all)
	var end *agentevents.Event
	for i := range failed {
		if failed[i].Kind == agentevents.KindTurnFinished {
			end = &failed[i]
		}
	}
	if end == nil || end.Status != agentevents.TurnFailed || end.Error == nil ||
		end.Error.Class != agentevents.ErrorInvalidRequest || end.Error.Code != "provider.no-route" {
		t.Fatalf("failed turn = %+v", end)
	}
}

func TestFramesOfOtherSessionsAreIgnored(t *testing.T) {
	events := mapAll(NewMapper("w1", "ses_someone_else"), frames(t, "tool-turn.sse"))
	if len(events) != 0 {
		t.Fatalf("a Worker saw %v from another session", kinds(events))
	}
}

func TestAnEndWithoutItsStartStillEndsTheTurn(t *testing.T) {
	m := NewMapper("w1", session)
	f, _ := ParseFrame([]byte(`{"id":"evt_1","type":"session.execution.succeeded","data":{"sessionID":"` + session + `"}}`))
	events := m.Map(f, time.Unix(1, 0))
	if len(events) != 1 || events[0].Kind != agentevents.KindTurnFinished || events[0].Status != agentevents.TurnCompleted {
		t.Fatalf("events = %+v", events)
	}
}

func TestErrorClassesComeFromTheStructuredType(t *testing.T) {
	for typ, want := range map[string]agentevents.ErrorClass{
		"provider.auth":       agentevents.ErrorAuth,
		"provider.quota":      agentevents.ErrorBilling,
		"provider.rate-limit": agentevents.ErrorRateLimit,
		"provider.timeout":    agentevents.ErrorNetwork,
		"provider.internal":   agentevents.ErrorServer,
		"provider.no-route":   agentevents.ErrorInvalidRequest,
		"something.new":       agentevents.ErrorUnknown,
	} {
		if got := ErrorFrom(typ, "429 try again later, please top up your credits").Class; got != want {
			t.Errorf("%s -> %s, want %s", typ, got, want)
		}
	}
}

func TestDecisionTakesOnlyTheServicesAnswers(t *testing.T) {
	for _, id := range []string{"once", "always", "reject"} {
		if got, ok := Decision(id); !ok || got != id {
			t.Errorf("Decision(%q) = %q, %v", id, got, ok)
		}
	}
	if _, ok := Decision("allow"); ok {
		t.Fatal("an option the service did not offer was accepted")
	}
}

func TestPrepareLaunchOwnsTheSessionFromTheFirstMoment(t *testing.T) {
	launch, err := Adapter{}.PrepareLaunch("opencode")
	if err != nil || !strings.Contains(launch.Command, "--session ses_") {
		t.Fatalf("launch = %+v, %v", launch, err)
	}
	same, _ := Adapter{}.PrepareLaunch("opencode --session ses_mine")
	if same.Command != "opencode --session ses_mine" {
		t.Fatalf("a launch that names its session was changed to %q", same.Command)
	}
}
