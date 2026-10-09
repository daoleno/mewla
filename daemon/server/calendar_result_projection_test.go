package server

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/calendar"
	"github.com/daoleno/mewla/daemon/work"
)

// Calendar results reach Brain through its thread conversation. The snapshot,
// which every client loads on connect and on each Calendar change, carries
// none of their bodies and reading it writes nothing.
func TestBrainSnapshotCarriesNoCalendarResultsAndDoesNotWrite(t *testing.T) {
	brainStore, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := brainStore.SetChatState(brain.ChatState{
		ThreadID: "thread-current", ThreadIDs: []string{"thread-history"},
	}); err != nil {
		t.Fatal(err)
	}
	service := brain.NewService(brainStore, nil, nil)
	calendarStore, err := calendar.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	large := strings.Repeat("briefing ", 16<<10)
	_ = finishScheduledResult(t, calendarStore, "item-current", "Current", "thread-current", large, "")
	_ = finishScheduledResult(t, calendarStore, "item-history", "History", "thread-history", "", "historical failure")

	snapshot, err := service.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	calendarRaw, calendarInfo := fileIdentity(t, calendarStore.Path())
	registryRaw, registryInfo := fileIdentity(t, brainStore.ChatStatePath())
	srv := &Server{brain: service, calendar: calendarStore}

	for range 2 {
		wire, err := srv.brainSnapshotWire(snapshot)
		if err != nil {
			t.Fatal(err)
		}
		payload, ok := wire.(map[string]any)
		if !ok {
			t.Fatalf("snapshot wire = %T", wire)
		}
		if _, ok := payload["scheduled_results"]; ok {
			t.Fatalf("brain snapshot carries scheduled results")
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(raw, []byte("briefing")) {
			t.Fatalf("brain snapshot carries a calendar result body (%d bytes)", len(raw))
		}
	}
	assertFileIdentity(t, calendarStore.Path(), calendarRaw, calendarInfo)
	assertFileIdentity(t, brainStore.ChatStatePath(), registryRaw, registryInfo)
}

func TestHistoricalBrainThreadUsesOnlyItsCalendarProjection(t *testing.T) {
	service, calendarStore := newBrainCalendarFixture(t, "thread-current", "thread-history")
	result := finishScheduledResult(t, calendarStore, "item-history", "History", "thread-history", "historical result", "")
	srv := &Server{brain: service, calendar: calendarStore}
	conversation := work.CodexConversation{
		Available: true,
		Activity:  &work.ProviderActivity{ID: "current-activity", Status: work.ProviderActivityRunning},
		Events:    []work.CodexConversationEvent{{ID: "current-provider-event", Kind: "assistant_message"}},
	}

	got := srv.brainScopedConversation("brain-thread:thread-history", conversation, time.Now())
	if !got.Available || got.Reason != "" || got.Activity != nil || len(got.Events) != 1 || got.Events[0].ID != result.ID {
		t.Fatalf("historical projection = %#v", got)
	}
}

func TestUnknownBrainThreadIsRejectedBeforeCalendarProjection(t *testing.T) {
	service, calendarStore := newBrainCalendarFixture(t, "thread-current")
	_ = finishScheduledResult(t, calendarStore, "item-unknown", "Unknown", "thread-unknown", "hidden", "")
	srv := &Server{brain: service, calendar: calendarStore}
	conversation := work.CodexConversation{
		Available: true,
		Activity:  &work.ProviderActivity{ID: "provider-activity", Status: work.ProviderActivityRunning},
		Events:    []work.CodexConversationEvent{{ID: "provider-event", Kind: "assistant_message"}},
	}

	got := srv.brainScopedConversation("brain-thread:thread-unknown", conversation, time.Now())
	if got.Available || got.Reason != "brain_thread_unknown" || got.Activity != nil || len(got.Events) != 0 {
		t.Fatalf("unknown thread projection = %#v", got)
	}
}

func fileIdentity(t *testing.T, path string) ([]byte, os.FileInfo) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw, info
}

func assertFileIdentity(t *testing.T, path string, want []byte, before os.FileInfo) {
	t.Helper()
	got, after := fileIdentity(t, path)
	if !bytes.Equal(got, want) || after.Mode() != before.Mode() ||
		!after.ModTime().Equal(before.ModTime()) || !os.SameFile(before, after) {
		t.Fatalf("file changed during projection: %s", path)
	}
}
