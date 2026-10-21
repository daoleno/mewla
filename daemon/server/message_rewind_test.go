package server

import (
	"errors"
	"fmt"
	"testing"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
)

func rewindConversation() work.CodexConversation {
	return work.CodexConversation{Available: true, Events: []work.CodexConversationEvent{
		{ID: "u1", Kind: "user_message", Role: "user", Body: "first"},
		{ID: "a1", Kind: "assistant_message", Role: "assistant", Body: "ok"},
		{ID: "u2", Kind: "user_message", Role: "user", Body: "second"},
		{ID: "t2", Kind: "tool", ToolName: "Bash"},
		{ID: "a2", Kind: "assistant_message", Role: "assistant", Body: "done"},
		{ID: "u3", Kind: "user_message", Role: "user", Body: "third"},
		{ID: "a3", Kind: "assistant_message", Role: "assistant", Body: "done again"},
	}}
}

func loadOf(conversation work.CodexConversation) func() (work.CodexConversation, error) {
	return func() (work.CodexConversation, error) { return conversation, nil }
}

func TestRewindMessage_DrivesTheCLIFromTheTranscript(t *testing.T) {
	for provider, want := range map[string]string{
		work.WorkerProviderClaude: watcher.MessageRewindClaude,
		work.WorkerProviderCodex:  watcher.MessageRewindCodex,
		work.WorkerProviderPi:     watcher.MessageRewindPi,
	} {
		var drove watcher.MessageRewindRequest
		var droveWorker string
		srv := &Server{rewindMessageOverride: func(workerID string, request watcher.MessageRewindRequest) error {
			droveWorker, drove = workerID, request
			return nil
		}}
		if clean, err := srv.rewindMessage("%4", provider, rewindMessageRequest{EventID: "u2"}, loadOf(rewindConversation())); err != nil || !clean {
			t.Fatalf("%s: clean = %v, err = %v", provider, clean, err)
		}
		// "second" is followed by one more human prompt ("third"); tool rows and
		// replies in between are not prompts.
		if droveWorker != "%4" || drove != (watcher.MessageRewindRequest{Provider: want, Text: "second", Back: 1}) {
			t.Fatalf("%s: drove %q %+v", provider, droveWorker, drove)
		}
	}
}

func TestRewindMessage_RefusesWhatItCannotProve(t *testing.T) {
	running := rewindConversation()
	running.Activity = &work.ProviderActivity{ID: "turn", Status: work.ProviderActivityRunning}
	for name, tc := range map[string]struct {
		provider string
		event    string
		conv     work.CodexConversation
	}{
		"an assistant reply":      {work.WorkerProviderClaude, "a1", rewindConversation()},
		"a missing event":         {work.WorkerProviderClaude, "gone", rewindConversation()},
		"a running turn":          {work.WorkerProviderCodex, "u1", running},
		"an agent without rewind": {work.WorkerProviderDSH, "u1", rewindConversation()},
		"cursor":                  {work.WorkerProviderCursor, "u1", rewindConversation()},
	} {
		called := false
		srv := &Server{rewindMessageOverride: func(string, watcher.MessageRewindRequest) error { called = true; return nil }}
		_, err := srv.rewindMessage("%4", tc.provider, rewindMessageRequest{EventID: tc.event}, loadOf(tc.conv))
		if !errors.Is(err, watcher.ErrMessageRewindRefused) || called {
			t.Fatalf("%s: err = %v, drove the CLI = %v", name, err, called)
		}
	}
}

func TestRewindMessage_PropagatesDriverRefusalAndKeepsTheConversationUntouched(t *testing.T) {
	srv := &Server{rewindMessageOverride: func(string, watcher.MessageRewindRequest) error {
		return errors.New("boom")
	}}
	if _, err := srv.rewindMessage("%4", work.WorkerProviderClaude, rewindMessageRequest{EventID: "u2"}, loadOf(rewindConversation())); err == nil {
		t.Fatal("driver failure was swallowed")
	}
	got := srv.rewound.apply("%4", rewindConversation())
	if len(got.Events) != len(rewindConversation().Events) {
		t.Fatalf("a failed rewind hid %d events", len(rewindConversation().Events)-len(got.Events))
	}
}

// Until the provider writes the edited prompt, its transcript still holds the
// rewound turns; the projection hides them, and stops once the provider drops
// the prompt itself.
func TestRewoundMessages_HidesRewoundTurnsUntilTheProviderDropsThem(t *testing.T) {
	var marks rewoundMessages
	marks.set("%4", "u2")
	hidden := marks.apply("%4", rewindConversation())
	if len(hidden.Events) != 2 || hidden.Events[1].ID != "a1" {
		t.Fatalf("rewound projection = %+v", hidden.Events)
	}
	other := marks.apply("%9", rewindConversation())
	if len(other.Events) != len(rewindConversation().Events) {
		t.Fatal("a rewind in one Session hid turns in another")
	}
	moved := work.CodexConversation{Available: true, Events: []work.CodexConversationEvent{
		{ID: "u1", Kind: "user_message", Role: "user", Body: "first"},
		{ID: "a1", Kind: "assistant_message", Role: "assistant", Body: "ok"},
		{ID: "u4", Kind: "user_message", Role: "user", Body: "second, edited"},
	}}
	if got := marks.apply("%4", moved); len(got.Events) != 3 {
		t.Fatalf("projection after the provider dropped the prompt = %+v", got.Events)
	}
	if again := marks.apply("%4", rewindConversation()); len(again.Events) != len(rewindConversation().Events) {
		t.Fatal("the mark outlived the rewound prompt")
	}
}

func TestWorkerSessionWireAdvertisesMessageRewindOnlyForRewindableAgents(t *testing.T) {
	srv := &Server{}
	for command, want := range map[string]bool{
		"claude":          true,
		"codex":           true,
		"pi":              true,
		"opencode --auto": false,
		"cursor-agent":    false,
	} {
		wire := srv.workerSessionWire(&classifier.Worker{ID: command, Command: command})
		if wire == nil || wire.Capabilities.MessageRewind != want {
			t.Fatalf("%s message_rewind = %+v, want %v", command, wire, want)
		}
	}
}

// The CLI stepped back but its input box is not clean: that is a rewind, with
// the turns hidden, reported as not clean so nothing is sent into the box.
func TestRewindMessage_ReportsARewoundButDirtyInputBox(t *testing.T) {
	srv := &Server{rewindMessageOverride: func(string, watcher.MessageRewindRequest) error {
		return fmt.Errorf("%w: it still holds text", watcher.ErrMessageRewindInputNotClean)
	}}
	clean, err := srv.rewindMessage("%4", work.WorkerProviderCodex, rewindMessageRequest{EventID: "u2"}, loadOf(rewindConversation()))
	if err != nil || clean {
		t.Fatalf("clean = %v, err = %v; want a rewind reported not clean", clean, err)
	}
	if got := srv.rewound.apply("%4", rewindConversation()); len(got.Events) != 2 {
		t.Fatalf("the rewound turns are still shown: %d events", len(got.Events))
	}
}
