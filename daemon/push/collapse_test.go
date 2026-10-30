package push

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type payloadCapture struct{ payloads []map[string]any }

func (c *payloadCapture) RoundTrip(request *http.Request) (*http.Response, error) {
	raw, _ := io.ReadAll(request.Body)
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	c.payloads = append(c.payloads, payload)
	return pushResponse(http.StatusOK, `{"data":{"status":"ok","id":"ticket"}}`), nil
}

// A Worker's notifications replace one another on the phone: a second "needs
// input" must not stack beside the first (Expo collapseId on iOS, tag on
// Android, threadId to group on iOS).
func TestWorkerNotificationsCollapsePerWorker(t *testing.T) {
	client := New()
	capture := &payloadCapture{}
	client.httpClient.Transport = capture
	client.SetRegistration("phone", "token-phone", "server-a")
	notice := WorkerNotice{WorkerID: "%763", Window: "friends", Summary: "Waiting for Claude permission"}
	for _, kind := range []string{KindWorkerBlocked, KindWorkerBlocked, KindWorkerFailed} {
		if err := client.NotifyWorker(kind, notice); err != nil {
			t.Fatal(err)
		}
	}
	if len(capture.payloads) != 3 {
		t.Fatalf("payloads = %d", len(capture.payloads))
	}
	for _, payload := range capture.payloads {
		if payload["collapseId"] != "worker:%763" || payload["tag"] != "worker:%763" || payload["threadId"] != "worker:%763" {
			t.Fatalf("the notification does not replace its predecessor: %v", payload)
		}
	}
}

func TestCollapseKeyFitsAPNsLimit(t *testing.T) {
	if got := collapseKey("worker:%7"); got != "worker:%7" {
		t.Fatalf("short key changed: %q", got)
	}
	long := "worker:" + strings.Repeat("x", 200)
	a, b := collapseKey(long), collapseKey(long+"y")
	if len(a) > 64 || a == b {
		t.Fatalf("long key %q (%d) / %q", a, len(a), b)
	}
}

// Live-surface pushes are posted by the app itself; they carry no collapse
// fields of their own.
func TestDataOnlyMessagesCarryNoCollapseFields(t *testing.T) {
	client := New()
	capture := &payloadCapture{}
	client.httpClient.Transport = capture
	client.SetRegistration("phone", "token-phone", "server-a")
	if err := client.Send(Message{Title: "t", DataOnly: true, CollapseID: "x", ThreadID: "y"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := capture.payloads[0]["collapseId"]; ok {
		t.Fatalf("data-only message collapsed: %v", capture.payloads[0])
	}
}
