package work

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/opencodeapi/opencodeapitest"
)

func TestHardenOpenCodeDelegatedCommand(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"opencode", "opencode --auto"},
		{"opencode --auto", "opencode --auto"},
		{"env PATH=/opt -- opencode", "env PATH=/opt -- opencode --auto"},
		{"opencode --auto --pure", "opencode --auto --pure"},
	} {
		got, err := HardenOpenCodeDelegatedCommand(tc.in)
		if err != nil || got != tc.want {
			t.Fatalf("%q: got %q err=%v", tc.in, got, err)
		}
	}
	for _, bad := range []string{"opencode --auto=false", "opencode --auto=true", "opencode --auto --auto=false"} {
		if _, err := HardenOpenCodeDelegatedCommand(bad); err == nil {
			t.Fatalf("%q must be rejected", bad)
		}
	}
}

func TestOpenCodeOwnedSessionIDIgnoresContinue(t *testing.T) {
	if got := OpenCodeOwnedSessionID("opencode --continue"); got != "" {
		t.Fatalf("continue must not own: %q", got)
	}
	if got := OpenCodeOwnedSessionID("opencode -s ses_123"); got != "ses_123" {
		t.Fatalf("got %q", got)
	}
}

func TestDelegatedOpenCodeLaunchOwnsANewSession(t *testing.T) {
	got, err := PrepareDelegatedCommand(WorkerProviderOpenCode, "opencode")
	if err != nil {
		t.Fatal(err)
	}
	id := OpenCodeOwnedSessionID(got)
	if !strings.HasPrefix(got, "opencode --auto --session ses_") || id == "" {
		t.Fatalf("delegated launch = %q", got)
	}
	again, _ := PrepareDelegatedCommand(WorkerProviderOpenCode, "opencode")
	if OpenCodeOwnedSessionID(again) == id {
		t.Fatalf("two launches share session %s", id)
	}
	// An explicit session or --continue is kept, never doubled.
	for _, command := range []string{"opencode --auto -s ses_keep", "opencode --auto --continue"} {
		kept, err := PrepareDelegatedCommand(WorkerProviderOpenCode, command)
		if err != nil || kept != command {
			t.Fatalf("%q became %q err=%v", command, kept, err)
		}
	}
	// A resume token still lands on a hardened command without a second session.
	resumed, err := WithProviderResumeToken(WorkerProviderOpenCode, "opencode --auto", "ses_old")
	if err != nil || resumed != "opencode --auto -s ses_old" {
		t.Fatalf("resume = %q err=%v", resumed, err)
	}
}

func TestNewOpenCodeSessionIDMatchesOpenCodeFormat(t *testing.T) {
	// OpenCode's own id for a session created at this instant starts eb59f479.
	now := time.UnixMilli(1792249215078)
	id := NewOpenCodeSessionID(now)
	if !regexp.MustCompile(`^ses_[0-9a-f]{12}[0-9A-Za-z]{14}$`).MatchString(id) {
		t.Fatalf("id %q", id)
	}
	if !strings.HasPrefix(id, "ses_eb59f479") {
		t.Fatalf("clock part of %q", id)
	}
	if later := NewOpenCodeSessionID(now.Add(time.Second)); later >= id {
		t.Fatalf("newer session %q must sort before %q", later, id)
	}
}

func openCodeWorker(cwd, command string, started time.Time) classifier.Worker {
	return classifier.Worker{Cwd: cwd, Command: command, StartedAt: started}
}

func TestOpenCodeBindRejectsAmbiguousSameCWD(t *testing.T) {
	service := opencodeapitest.Install(t)
	started := time.Date(2026, 8, 6, 0, 0, 10, 0, time.UTC)
	for _, id := range []string{"ses_a", "ses_b"} {
		service.PutSession(opencodeapitest.Session{ID: id, Directory: "/repo", Created: started.Add(time.Second), Updated: started.Add(2 * time.Second)})
	}
	_, ok, err := NewProviderConversationReader().findOpenCodeSession(openCodeWorker("/repo", "opencode", started), started.Add(time.Minute))
	if err != nil || ok {
		t.Fatalf("ambiguous same-CWD sessions must refuse bind: ok=%v err=%v", ok, err)
	}
}

func TestOpenCodeBindsRootNotChildAndNeverPreStart(t *testing.T) {
	service := opencodeapitest.Install(t)
	started := time.Date(2026, 8, 6, 0, 0, 10, 0, time.UTC)
	service.PutSession(opencodeapitest.Session{ID: "ses_prev", Directory: "/repo", Created: started.Add(-time.Hour), Updated: started.Add(time.Minute)})
	service.PutMessage("ses_prev", opencodeapitest.User("msg_prev", started.Add(-time.Hour), "previous session history"))
	reader := NewProviderConversationReader()
	worker := openCodeWorker("/repo", "opencode", started)

	conversation, err := reader.Load(worker, WorkerProviderOpenCode, started.Add(5*time.Second))
	if err != nil || conversation.Available || conversation.Reason != "session_not_found" {
		t.Fatalf("pre-start session must not bind: %+v err=%v", conversation, err)
	}

	service.PutSession(opencodeapitest.Session{ID: "ses_own", Directory: "/repo", Created: started.Add(2 * time.Second), Updated: started.Add(3 * time.Second)})
	service.PutSession(opencodeapitest.Session{ID: "ses_child", ParentID: "ses_own", Directory: "/repo", Created: started.Add(2 * time.Second), Updated: started.Add(4 * time.Second)})
	service.PutMessage("ses_own", opencodeapitest.User("msg_own", started.Add(2*time.Second), "own session history"))
	conversation, err = reader.Load(worker, WorkerProviderOpenCode, started.Add(10*time.Second))
	if err != nil || !conversation.Available || conversation.SessionID != "ses_own" {
		t.Fatalf("own root must bind: %+v err=%v", conversation, err)
	}
	if len(conversation.Events) != 1 || conversation.Events[0].Body != "own session history" {
		t.Fatalf("events %#v", conversation.Events)
	}
}

func TestOpenCodeLaunchSessionBindsOnceOpenCodeCreatesIt(t *testing.T) {
	service := opencodeapitest.Install(t)
	started := time.Date(2026, 8, 6, 0, 0, 10, 0, time.UTC)
	reader := NewProviderConversationReader()
	worker := openCodeWorker("/repo", "opencode --auto --session ses_mine", started)
	// A concurrent session in the same directory never steals the binding.
	service.PutSession(opencodeapitest.Session{ID: "ses_other", Directory: "/repo", Created: started.Add(time.Second), Updated: started.Add(time.Second)})

	conversation, _ := reader.Load(worker, WorkerProviderOpenCode, started.Add(time.Second))
	if conversation.Available {
		t.Fatalf("launch session does not exist yet: %+v", conversation)
	}
	service.PutSession(opencodeapitest.Session{ID: "ses_mine", Directory: "/repo", Created: started.Add(2 * time.Second), Updated: started.Add(2 * time.Second)})
	service.PutMessage("ses_mine", opencodeapitest.User("msg_1", started.Add(2*time.Second), "task"))
	conversation, err := reader.Load(worker, WorkerProviderOpenCode, started.Add(3*time.Second))
	if err != nil || conversation.SessionID != "ses_mine" || len(conversation.Events) != 1 {
		t.Fatalf("launch session must bind: %+v err=%v", conversation, err)
	}
}

func TestOpenCodeServiceUnavailableIsAReasonNotAnError(t *testing.T) {
	service := opencodeapitest.Install(t)
	service.Unavailable = true
	conversation, err := NewProviderConversationReader().Load(openCodeWorker("/repo", "opencode", time.Now()), WorkerProviderOpenCode, time.Now())
	if err != nil || conversation.Reason != "service_unavailable" {
		t.Fatalf("got %+v err=%v", conversation, err)
	}
}

func TestOpenCodeProjectsTurnAndSettlesOnIdle(t *testing.T) {
	service := opencodeapitest.Install(t)
	started := time.Date(2026, 8, 6, 0, 0, 10, 0, time.UTC)
	at := func(seconds int) time.Time { return started.Add(time.Duration(seconds) * time.Second) }
	service.PutSession(opencodeapitest.Session{ID: "ses_1", Directory: "/repo", Created: started, Updated: at(1)})
	service.PutMessage("ses_1", opencodeapitest.User("msg_u", at(1), "fix it"))
	service.PutMessage("ses_1", opencodeapitest.Assistant("msg_a", at(2), time.Time{},
		opencodeapitest.Reasoning("thinking"),
		opencodeapitest.Tool("call_1", "bash", "running", map[string]any{"command": "make"}, ""),
		opencodeapitest.Tool("call_2", "read", "streaming", map[string]any{"path": "a"}, "")))
	reader := NewProviderConversationReader()
	worker := openCodeWorker("/repo", "opencode -s ses_1", started)

	live, err := reader.Load(worker, WorkerProviderOpenCode, at(3))
	if err != nil {
		t.Fatal(err)
	}
	if live.Activity == nil || live.Activity.Status != ProviderActivityRunning {
		t.Fatalf("running turn activity %+v", live.Activity)
	}
	if len(live.Events) != 4 || !live.Events[1].Partial || live.Events[2].Status != "running" || live.Events[3].Input != `{"path":"a"}` {
		t.Fatalf("live events %#v", live.Events)
	}

	service.PutMessage("ses_1", opencodeapitest.Assistant("msg_a", at(2), at(4),
		opencodeapitest.Reasoning("thinking"),
		opencodeapitest.Tool("call_1", "bash", "completed", map[string]any{"command": "make"}, "built"),
		opencodeapitest.Tool("call_2", "read", "error", map[string]any{"path": "a"}, "no such file")))
	service.PutMessage("ses_1", opencodeapitest.Assistant("msg_b", at(5), at(6), opencodeapitest.Text("done")))
	service.PutMessage("ses_1", opencodeapitest.Idle("msg_i", at(6), "succeeded"))
	done, err := reader.Load(worker, WorkerProviderOpenCode, at(7))
	if err != nil {
		t.Fatal(err)
	}
	if done.Activity == nil || done.Activity.Status != ProviderActivityCompleted {
		t.Fatalf("settled activity %+v", done.Activity)
	}
	want := []struct{ kind, status, body string }{
		{"user_message", "", "fix it"}, {"reasoning", "", "thinking"},
		{"tool_call", "completed", ""}, {"tool_call", "failed", ""}, {"assistant_message", "", "done"},
	}
	if len(done.Events) != len(want) {
		t.Fatalf("events %#v", done.Events)
	}
	for index, event := range done.Events {
		if event.Kind != want[index].kind || event.Status != want[index].status || event.Partial ||
			(want[index].body != "" && event.Body != want[index].body) {
			t.Fatalf("event %d = %#v", index, event)
		}
	}
	if done.Events[2].Output != "built" || done.Events[3].Output != "no such file" {
		t.Fatalf("tool outputs %#v", done.Events[2:4])
	}
}

func TestOpenCodeIdleOutcomeMapsFailureAndInterrupt(t *testing.T) {
	for outcome, want := range map[string]ProviderActivityStatus{"failed": ProviderActivityFailed, "interrupted": ProviderActivityInterrupted} {
		service := opencodeapitest.Install(t)
		started := time.Date(2026, 8, 6, 0, 0, 10, 0, time.UTC)
		service.PutSession(opencodeapitest.Session{ID: "ses_x", Directory: "/repo", Created: started, Updated: started})
		service.PutMessage("ses_x", opencodeapitest.User("msg_u", started, "go"))
		service.PutMessage("ses_x", opencodeapitest.Assistant("msg_a", started, time.Time{},
			opencodeapitest.Tool("call", "bash", "running", map[string]any{}, "")))
		service.PutMessage("ses_x", opencodeapitest.Idle("msg_i", started.Add(time.Second), outcome))
		conversation, err := parseOpenCodeConversation("ses_x")
		if err != nil {
			t.Fatal(err)
		}
		if conversation.Activity == nil || conversation.Activity.Status != want {
			t.Fatalf("%s: activity %+v", outcome, conversation.Activity)
		}
		if tool := conversation.Events[1]; tool.Partial || tool.Status != "interrupted" {
			t.Fatalf("%s: unfinished tool %#v", outcome, tool)
		}
	}
}

func TestOpenCodeCacheSkipsAPIWhenStampUnchangedAndRefreshesTail(t *testing.T) {
	service := opencodeapitest.Install(t)
	started := time.Date(2026, 8, 6, 0, 0, 10, 0, time.UTC)
	service.PutSession(opencodeapitest.Session{ID: "ses_c", Directory: "/repo", Created: started, Updated: started})
	for index := range 120 {
		id := "msg_" + strings.Repeat("0", 3-len(itoa(index))) + itoa(index)
		service.PutMessage("ses_c", opencodeapitest.User(id, started.Add(time.Duration(index)*time.Millisecond), "m"+itoa(index)))
	}
	reader := NewProviderConversationReader()
	worker := openCodeWorker("/repo", "opencode -s ses_c", started)
	first, err := reader.Load(worker, WorkerProviderOpenCode, started)
	if err != nil || len(first.Events) != 120 {
		t.Fatalf("first load %d events err=%v", len(first.Events), err)
	}
	version := reader.ConversationVersion()

	calls := service.CallCount("session.message.list") + service.CallCount("session.get")
	if _, err := reader.Load(worker, WorkerProviderOpenCode, started); err != nil {
		t.Fatal(err)
	}
	if got := service.CallCount("session.message.list") + service.CallCount("session.get"); got != calls {
		t.Fatalf("unchanged stamp made %d API calls", got-calls)
	}
	if reader.ConversationVersion() != version {
		t.Fatal("unchanged poll must keep the content version")
	}

	// A new message and a reverted tail message: one newest-first page.
	pages := service.CallCount("session.message.list")
	service.RemoveMessage("ses_c", "msg_119")
	service.PutMessage("ses_c", opencodeapitest.User("msg_120", started.Add(time.Second), "newest"))
	next, err := reader.Load(worker, WorkerProviderOpenCode, started)
	if err != nil {
		t.Fatal(err)
	}
	if got := service.CallCount("session.message.list") - pages; got != 1 {
		t.Fatalf("tail refresh used %d pages", got)
	}
	if len(next.Events) != 120 || next.Events[119].Body != "newest" || next.Events[118].Body != "m118" {
		t.Fatalf("refreshed tail %d events, last %#v", len(next.Events), next.Events[len(next.Events)-1])
	}
	if reader.ConversationVersion() == version {
		t.Fatal("changed content must advance the version")
	}
	changed := strings.Join(reader.ChangedEventIDs(), ",")
	if !strings.Contains(changed, "msg_120") || !strings.Contains(changed, "msg_119") {
		t.Fatalf("changed ids %q", changed)
	}
}

func itoa(value int) string { return strconv.Itoa(value) }
