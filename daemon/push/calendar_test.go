package push

import (
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"
)

type sentRecorder struct {
	mu   sync.Mutex
	sent []Sent
}

func (r *sentRecorder) NotificationSent(s Sent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, s)
}
func (r *sentRecorder) NotificationReceipt(Receipt) {}

func calendarAlertFixture() CalendarAlert {
	at := time.Date(2026, time.July, 14, 15, 30, 0, 0, time.UTC)
	return CalendarAlert{ID: "item-1", Kind: "event", Title: "Design review", At: at, AlertAt: at.Add(-10 * time.Minute), Now: at.Add(-10 * time.Minute)}
}

func sendCalendarAlert(t *testing.T, client *Client, alert CalendarAlert) map[string]any {
	t.Helper()
	var payload map[string]any
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		return pushResponse(http.StatusOK, `{"data":{"status":"ok","id":"ticket-1"}}`), nil
	})
	if err := client.NotifyCalendarAlert(alert); err != nil {
		t.Fatal(err)
	}
	return payload
}

func TestCalendarAlertTextSaysHowSoon(t *testing.T) {
	at := time.Date(2026, time.July, 14, 15, 30, 0, 0, time.UTC)
	for _, test := range []struct {
		alert       CalendarAlert
		title, body string
	}{
		{CalendarAlert{Kind: "event", Title: "Standup", At: at, Now: at.Add(-10 * time.Minute)}, "Standup", "Starts in 10 min"},
		{CalendarAlert{Kind: "event", Title: "Standup", Notes: "Room 4", At: at, Now: at.Add(-90 * time.Minute)}, "Standup", "Starts in 1 h 30 min · Room 4"},
		{CalendarAlert{Kind: "event", Title: "Standup", At: at, Now: at}, "Standup", "Starting now"},
		{CalendarAlert{Kind: "deadline", Title: "Tax", At: at, Now: at.Add(-30 * time.Minute)}, "Tax", "Due in 30 min"},
		{CalendarAlert{Kind: "reminder", Title: "Call mom", At: at, Now: at}, "Call mom", "Reminder"},
		{CalendarAlert{Kind: "reminder", Title: "Call mom", Notes: "She is at the airport", At: at, Now: at}, "Call mom", "Reminder · She is at the airport"},
	} {
		title, body := CalendarAlertText(test.alert)
		if title != test.title || body != test.body {
			t.Errorf("%+v = %q / %q, want %q / %q", test.alert, title, body, test.title, test.body)
		}
	}
}

func TestAndroidCalendarAlertIsDataOnlyForTheAppToPost(t *testing.T) {
	client := New()
	client.SetRegistration("phone", "token-1", "server-1")
	client.SetDevice("phone", "android", true)
	alert := calendarAlertFixture()
	payload := sendCalendarAlert(t, client, alert)
	for _, shown := range []string{"title", "body", "sound", "channelId"} {
		if _, has := payload[shown]; has {
			t.Fatalf("a data-only message carries %q: %#v", shown, payload)
		}
	}
	if payload["priority"] != "high" || payload["ttl"] != float64(calendarAlertTTL) {
		t.Fatalf("payload = %#v", payload)
	}
	data := payload["data"].(map[string]any)
	wantKey := calendarAlertKey("item-1", alert.AlertAt)
	if data["mewla_calendar"] != "1" || data["alert_key"] != wantKey || data["calendar_id"] != "item-1" ||
		data["title"] != "Design review" || data["body"] != "Starts in 10 min" || data["strong"] != "true" || data["server_id"] != "server-1" {
		t.Fatalf("data = %#v", data)
	}
}

func TestIOSCalendarAlertIsTimeSensitiveUnlessTurnedOff(t *testing.T) {
	for _, strong := range []bool{true, false} {
		client := New()
		client.SetRegistration("phone", "token-1", "server-1")
		client.SetDevice("phone", "ios", strong)
		payload := sendCalendarAlert(t, client, calendarAlertFixture())
		level, has := payload["interruptionLevel"]
		if strong && level != "time-sensitive" || !strong && has {
			t.Fatalf("strong=%v interruptionLevel = %#v", strong, payload["interruptionLevel"])
		}
		if payload["title"] != "Design review" || payload["body"] != "Starts in 10 min" || payload["sound"] != "default" {
			t.Fatalf("payload = %#v", payload)
		}
	}
}

func TestCalendarAlertWithoutAKnownPlatformIsAVisibleHighPriorityPush(t *testing.T) {
	client := New()
	client.SetRegistration("phone", "token-1", "server-1")
	payload := sendCalendarAlert(t, client, calendarAlertFixture())
	if payload["channelId"] != calendarChannel || payload["priority"] != "high" || payload["title"] != "Design review" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestCalendarAlertIsRecordedForWatchOnce(t *testing.T) {
	client := New()
	recorder := &sentRecorder{}
	client.SetObserver(recorder)
	client.SetRegistration("a", "token-a", "server-1")
	client.SetRegistration("b", "token-b", "server-1")
	client.SetDevice("a", "android", true)
	client.SetDevice("b", "ios", true)
	sendCalendarAlert(t, client, calendarAlertFixture())
	if len(recorder.sent) != 1 {
		t.Fatalf("records = %d", len(recorder.sent))
	}
	got := recorder.sent[0]
	if got.Kind != KindCalendarAlert || got.Outcome != OutcomeAccepted || got.Title != "Design review" || got.Body != "Starts in 10 min" || got.Priority != "high" {
		t.Fatalf("record = %#v", got)
	}
	if got.Refs["screen"] != "calendar" || got.Refs["calendar_id"] != "item-1" {
		t.Fatalf("refs = %#v", got.Refs)
	}
}

func TestCalendarAlertWithNoPhoneIsNotSentButRecorded(t *testing.T) {
	client := New()
	recorder := &sentRecorder{}
	client.SetObserver(recorder)
	if err := client.NotifyCalendarAlert(calendarAlertFixture()); err != ErrNoRegistration {
		t.Fatalf("err = %v", err)
	}
	if len(recorder.sent) != 1 || recorder.sent[0].Outcome != OutcomeNotSent {
		t.Fatalf("records = %#v", recorder.sent)
	}
}
