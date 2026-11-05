package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/calendar"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
	"github.com/gorilla/websocket"
)

func TestAuthenticatedCalendarWebSocketCRUD(t *testing.T) {
	authManager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pairing, _ := authManager.IssuePairingToken(time.Minute)
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := authManager.EnrollDevice(pairing.Value, authManager.DaemonID(), authManager.PublicKeyHex(), "device-calendar", "phone", hex.EncodeToString(publicKey)); err != nil {
		t.Fatal(err)
	}
	store, _ := calendar.NewStore(t.TempDir())
	srv := New(authManager, watcher.New(time.Second), nil, nil, nil, nil, nil)
	srv.SetCalendar(store, calendar.NewScheduler(store, nil))
	httpServer := httptest.NewServer(http.HandlerFunc(srv.handleWS))
	defer httpServer.Close()
	header := http.Header{}
	header.Set("Authorization", calendarAuthHeader(privateKey, authManager.DaemonID(), "device-calendar", "mewla-connect"))
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(httpServer.URL, "http"), header)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	readType := func(want string) map[string]any {
		t.Helper()
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		for {
			_, raw, readErr := conn.ReadMessage()
			if readErr != nil {
				t.Fatal(readErr)
			}
			var payload map[string]any
			if err := json.Unmarshal(raw, &payload); err != nil {
				t.Fatal(err)
			}
			if payload["type"] == want {
				return payload
			}
		}
	}
	snapshot := readType("calendar_items_snapshot")
	if len(snapshot["calendar_items"].([]any)) != 0 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	notify := time.Date(2026, time.July, 15, 9, 30, 0, 0, time.FixedZone("CST", 8*3600))
	if err := conn.WriteJSON(map[string]any{"type": "create_calendar_item", "request_id": "create-1", "calendar_item": calendar.Item{Title: "Morning review", Kind: calendar.KindReminder, NotifyAt: &notify, Timezone: "Asia/Shanghai", Recurrence: calendar.RecurrenceNone}}); err != nil {
		t.Fatal(err)
	}
	created := readType("calendar_item_created")
	if created["request_id"] != "create-1" {
		t.Fatalf("created = %#v", created)
	}
	itemRaw, _ := json.Marshal(created["calendar_item"])
	var item calendar.Item
	if err := json.Unmarshal(itemRaw, &item); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(map[string]any{"type": "cancel_calendar_item", "request_id": "cancel-1", "id": item.ID, "revision": item.Revision}); err != nil {
		t.Fatal(err)
	}
	cancelled := readType("calendar_item_cancelled")
	raw, _ := json.Marshal(cancelled["calendar_item"])
	if err := json.Unmarshal(raw, &item); err != nil {
		t.Fatal(err)
	}
	if item.Status != calendar.StatusCancelled {
		t.Fatalf("item = %#v", item)
	}
}

func calendarAuthHeader(privateKey ed25519.PrivateKey, daemonID, deviceID, purpose string) string {
	timestamp := strconv.FormatInt(time.Now().UTC().UnixMilli(), 10)
	nonce := make([]byte, 16)
	_, _ = rand.Read(nonce)
	nonceHex := hex.EncodeToString(nonce)
	signature := ed25519.Sign(privateKey, auth.BuildSignaturePayload(purpose, daemonID, deviceID, timestamp, nonceHex))
	return auth.AuthorizationHeaderPrefix + "v1:" + deviceID + ":" + daemonID + ":" + timestamp + ":" + nonceHex + ":" + hex.EncodeToString(signature)
}

func TestCalendarWebSocketSnoozeAndDoneAnswerTheAlert(t *testing.T) {
	authManager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pairing, _ := authManager.IssuePairingToken(time.Minute)
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := authManager.EnrollDevice(pairing.Value, authManager.DaemonID(), authManager.PublicKeyHex(), "device-alert", "phone", hex.EncodeToString(publicKey)); err != nil {
		t.Fatal(err)
	}
	store, _ := calendar.NewStore(t.TempDir())
	srv := New(authManager, watcher.New(time.Second), nil, nil, nil, nil, nil)
	srv.SetCalendar(store, calendar.NewScheduler(store, nil))
	httpServer := httptest.NewServer(http.HandlerFunc(srv.handleWS))
	defer httpServer.Close()
	header := http.Header{}
	header.Set("Authorization", calendarAuthHeader(privateKey, authManager.DaemonID(), "device-alert", "mewla-connect"))
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(httpServer.URL, "http"), header)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	readType := func(want string) map[string]any {
		t.Helper()
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		for {
			_, raw, readErr := conn.ReadMessage()
			if readErr != nil {
				t.Fatal(readErr)
			}
			var payload map[string]any
			if err := json.Unmarshal(raw, &payload); err != nil {
				t.Fatal(err)
			}
			if payload["type"] == want || payload["type"] == "error" {
				return payload
			}
		}
	}
	at := time.Now().UTC().Add(time.Hour)
	item, err := store.Create(calendar.Item{Title: "Call", Kind: calendar.KindReminder, NotifyAt: &at, Timezone: "UTC"})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(map[string]any{"type": "snooze_calendar_item", "request_id": "s0", "id": item.ID, "snooze_minutes": 0}); err != nil {
		t.Fatal(err)
	}
	if bad := readType("calendar_item_updated"); bad["type"] != "error" {
		t.Fatalf("a zero snooze was accepted: %#v", bad)
	}
	if err := conn.WriteJSON(map[string]any{"type": "snooze_calendar_item", "request_id": "s1", "id": item.ID, "snooze_minutes": 15}); err != nil {
		t.Fatal(err)
	}
	snoozed := readType("calendar_item_updated")
	raw, _ := json.Marshal(snoozed["calendar_item"])
	var got calendar.Item
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.SnoozedUntil == nil || time.Until(*got.SnoozedUntil) < 14*time.Minute {
		t.Fatalf("snoozed = %#v", got)
	}
	if _, err := store.SetStatus(item.ID, calendar.StatusWaiting, ""); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(map[string]any{"type": "done_calendar_item", "request_id": "d1", "id": item.ID}); err != nil {
		t.Fatal(err)
	}
	done := readType("calendar_item_updated")
	raw, _ = json.Marshal(done["calendar_item"])
	got = calendar.Item{}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != calendar.StatusCompleted || got.SnoozedUntil != nil {
		t.Fatalf("done = %#v", got)
	}
}

// The phone saves scheduled actions through these WebSocket messages, not
// through the CLI: its route must be resolved and kept by the same store check.
func TestCalendarWebSocketResolvesWorkerRouteAndKeepsItOnEdit(t *testing.T) {
	authManager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pairing, _ := authManager.IssuePairingToken(time.Minute)
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := authManager.EnrollDevice(pairing.Value, authManager.DaemonID(), authManager.PublicKeyHex(), "device-route", "phone", hex.EncodeToString(publicKey)); err != nil {
		t.Fatal(err)
	}
	execs := work.NewExecutorConfig(map[string]work.Executor{
		"claude": {Name: "claude", Command: "claude --model claude-opus-5-5"},
		"codex":  {Name: "codex", Command: "codex"},
	})
	store, _ := calendar.NewStore(t.TempDir())
	store.SetRouteCheck(func(item calendar.Item) error {
		_, _, err := execs.ResolveScheduledRoute(work.LaunchRoute{Executor: item.ActionExecutor, Model: item.ActionModel, Reasoning: item.ActionReasoning})
		return err
	})
	srv := New(authManager, watcher.New(time.Second), nil, nil, nil, nil, nil)
	srv.SetCalendar(store, calendar.NewScheduler(store, nil))
	httpServer := httptest.NewServer(http.HandlerFunc(srv.handleWS))
	defer httpServer.Close()
	header := http.Header{}
	header.Set("Authorization", calendarAuthHeader(privateKey, authManager.DaemonID(), "device-route", "mewla-connect"))
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(httpServer.URL, "http"), header)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	read := func(want string) map[string]any {
		t.Helper()
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		for {
			_, raw, readErr := conn.ReadMessage()
			if readErr != nil {
				t.Fatal(readErr)
			}
			var payload map[string]any
			if err := json.Unmarshal(raw, &payload); err != nil {
				t.Fatal(err)
			}
			if payload["type"] == want {
				return payload
			}
		}
	}
	read("calendar_items_snapshot")
	due := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	action := func(extra map[string]any) map[string]any {
		item := map[string]any{"title": "Phone action", "kind": "scheduled_action", "due_at": due, "timezone": "UTC", "recurrence": "none",
			"action_instruction": "Say hi", "source_thread_id": "thread-1"}
		for key, value := range extra {
			item[key] = value
		}
		return item
	}
	send := func(msgType, id string, item map[string]any) {
		t.Helper()
		if err := conn.WriteJSON(map[string]any{"type": msgType, "request_id": id, "calendar_item": item, "revision": item["revision"]}); err != nil {
			t.Fatal(err)
		}
	}
	decode := func(payload map[string]any) calendar.Item {
		t.Helper()
		raw, _ := json.Marshal(payload["calendar_item"])
		var item calendar.Item
		if err := json.Unmarshal(raw, &item); err != nil {
			t.Fatal(err)
		}
		return item
	}

	// No route and no default configured: refused, nothing saved.
	send("create_calendar_item", "c0", action(nil))
	if refused := read("error"); !strings.Contains(refused["message"].(string), "no Worker route") || len(store.List()) != 0 {
		t.Fatalf("route-less create without a default: %#v", refused)
	}
	execs.ScheduledAction = work.LaunchRoute{Executor: "claude", Model: "claude-sonnet-5-5"}
	send("create_calendar_item", "c1", action(map[string]any{"action_executor": "nope"}))
	if refused := read("error"); !strings.Contains(refused["message"].(string), "not configured") || len(store.List()) != 0 {
		t.Fatalf("unknown executor: %#v", refused)
	}
	send("create_calendar_item", "c2", action(map[string]any{"action_executor": "claude", "action_reasoning": "ultra"}))
	if refused := read("error"); len(store.List()) != 0 {
		t.Fatalf("bad reasoning saved: %#v", refused)
	}
	// A phone-created item names no route; the default applies when it fires.
	send("create_calendar_item", "c3", action(nil))
	if phone := decode(read("calendar_item_created")); phone.ActionExecutor != "" {
		t.Fatalf("phone item = %#v", phone)
	}
	// An explicit Codex item survives an edit that sends the whole item back.
	send("create_calendar_item", "c4", action(map[string]any{"title": "Codex on request", "action_executor": "codex", "action_model": "gpt-6-astra", "action_reasoning": "max"}))
	explicit := decode(read("calendar_item_created"))
	edit := action(nil)
	raw, _ := json.Marshal(explicit)
	_ = json.Unmarshal(raw, &edit)
	edit["title"] = "Renamed on the phone"
	send("update_calendar_item", "u1", edit)
	edited := decode(read("calendar_item_updated"))
	if edited.Title != "Renamed on the phone" || edited.ActionExecutor != "codex" || edited.ActionModel != "gpt-6-astra" || edited.ActionReasoning != "max" {
		t.Fatalf("edit lost the route: %#v", edited)
	}
	edit["revision"], edit["action_executor"] = edited.Revision, "nope"
	send("update_calendar_item", "u2", edit)
	if refused := read("error"); !strings.Contains(refused["message"].(string), "not configured") {
		t.Fatalf("edit to an unknown executor: %#v", refused)
	}
}
