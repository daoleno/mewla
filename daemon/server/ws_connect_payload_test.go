package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/calendar"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
	"github.com/gorilla/websocket"
)

// What a client receives on connect must not grow with Calendar history. A
// daily scheduled action leaves a full result on its Calendar run and in a
// Work file every day; months of that once made the connect snapshots several
// megabytes, more than a phone or proxy link drains before its write deadline.
func TestConnectPayloadDoesNotGrowWithCalendarHistory(t *testing.T) {
	few := connectPayloadBytes(t, 3)
	many := connectPayloadBytes(t, 60)
	for _, kind := range []string{"work_items_snapshot", "calendar_items_snapshot", "brain_snapshot"} {
		if many[kind] == 0 {
			t.Fatalf("%s did not arrive on connect", kind)
		}
		if growth := many[kind] - few[kind]; growth > 1<<10 {
			t.Fatalf("%s grew by %d bytes from 3 to 60 finished runs (%d -> %d)", kind, growth, few[kind], many[kind])
		}
	}
	if total := many["total"]; total > 64<<10 {
		t.Fatalf("connect payload is %d bytes after 60 finished runs: %v", total, many)
	}
}

// connectPayloadBytes seeds one scheduled action with runs finished runs, each
// leaving a 32 KiB result and a finished Work file, then connects one client
// and sizes the read-model snapshots it is sent.
func connectPayloadBytes(t *testing.T, runs int) map[string]int {
	t.Helper()
	authManager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pairing, _ := authManager.IssuePairingToken(time.Minute)
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := authManager.EnrollDevice(pairing.Value, authManager.DaemonID(), authManager.PublicKeyHex(), "device-payload", "phone", hex.EncodeToString(publicKey)); err != nil {
		t.Fatal(err)
	}
	brainStore, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := brainStore.SetChatState(brain.ChatState{ThreadID: "thread-current"}); err != nil {
		t.Fatal(err)
	}
	workRoot := t.TempDir()
	workStore, err := work.NewStore(workRoot)
	if err != nil {
		t.Fatal(err)
	}
	calendarStore, err := calendar.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	due := time.Now().UTC().Add(time.Hour)
	item, err := calendarStore.Create(calendar.Item{
		ID: "daily-briefing", Title: "Daily briefing", Kind: calendar.KindScheduledAction,
		DueAt: &due, Timezone: "UTC", Recurrence: calendar.RecurrenceNone,
		ActionInstruction: "Brief me", SourceThreadID: "thread-current",
	})
	if err != nil {
		t.Fatal(err)
	}
	result := strings.Repeat("briefing ", 32<<10/9)
	for i := range runs {
		_, run, err := calendarStore.Claim(item.ID, true)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := calendarStore.FinishRun(item.ID, run.ID, result, ""); err != nil {
			t.Fatal(err)
		}
		done := time.Now().UTC()
		id := fmt.Sprintf("run-%03d", i)
		if _, err := workStore.Write(&work.Item{
			ID:      id,
			Path:    filepath.Join(workRoot, "calendar", id+".md"),
			Project: "calendar",
			Body:    "# Daily briefing\n\n" + result,
			Frontmatter: work.Frontmatter{
				ID: id, Kind: "calendar_action", Created: done, Done: &done, WorkerSession: "%9" + id,
			},
		}, time.Time{}); err != nil {
			t.Fatal(err)
		}
	}

	srv := New(authManager, watcher.New(time.Second), nil, nil, workStore, nil, brain.NewService(brainStore, nil, nil))
	srv.SetCalendar(calendarStore, calendar.NewScheduler(calendarStore, nil))
	httpServer := httptest.NewServer(http.HandlerFunc(srv.handleWS))
	defer httpServer.Close()
	header := http.Header{}
	header.Set("Authorization", calendarAuthHeader(privateKey, authManager.DaemonID(), "device-payload", "mewla-connect"))
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(httpServer.URL, "http"), header)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	sizes := map[string]int{}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for sizes["work_items_snapshot"] == 0 || sizes["calendar_items_snapshot"] == 0 || sizes["brain_snapshot"] == 0 {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("connect snapshots incomplete: %v (%v)", err, sizes)
		}
		var head struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &head); err != nil {
			t.Fatal(err)
		}
		sizes[head.Type] += len(raw)
		sizes["total"] += len(raw)
	}
	return sizes
}
