package push

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
)

func capture(t *testing.T, client *Client, reply string) *map[string]any {
	t.Helper()
	var sent map[string]any
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != expoPushURL {
			t.Fatalf("url = %s", request.URL)
		}
		body, _ := io.ReadAll(request.Body)
		sent = map[string]any{}
		if err := json.Unmarshal(body, &sent); err != nil {
			t.Fatal(err)
		}
		return pushResponse(http.StatusOK, reply), nil
	})
	return &sent
}

func TestSendLiveIsDataOnlyHighPriorityAndStringly(t *testing.T) {
	client := New()
	client.SetRegistration("ExponentPushToken[abc]", "server-1")
	client.SetLiveCapable(true)
	sent := capture(t, client, `{"data":{"status":"ok","id":"t1"}}`)

	err := client.SendLive(map[string]any{"payload": `{"kind":"running"}`, "epoch": "1000", "seq": 7})
	if err != nil {
		t.Fatal(err)
	}
	body := *sent
	for _, forbidden := range []string{"title", "body", "sound", "subtitle", "badge", "channelId"} {
		if _, ok := body[forbidden]; ok {
			t.Fatalf("a live push must be data-only, found %q", forbidden)
		}
	}
	if body["to"] != "ExponentPushToken[abc]" || body["priority"] != "high" || body["ttl"] != float64(120) {
		t.Fatalf("body = %v", body)
	}
	data, ok := body["data"].(map[string]any)
	if !ok {
		t.Fatalf("data = %v", body["data"])
	}
	want := map[string]string{"mewla_live": "1", "server_ref": "server-1", "payload": `{"kind":"running"}`, "epoch": "1000", "seq": "7"}
	for key, value := range want {
		if data[key] != value {
			t.Fatalf("data[%q] = %v, want %q", key, data[key], value)
		}
	}
	for key, value := range data {
		if _, isString := value.(string); !isString {
			t.Fatalf("data[%q] is %T; FCM data values are strings", key, value)
		}
	}
}

func TestSendLiveIsGatedByTheDeviceCapability(t *testing.T) {
	client := New()
	calls := 0
	client.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return pushResponse(http.StatusOK, `{"data":{"status":"ok"}}`), nil
	})
	if err := client.SendLive(map[string]any{"seq": "1"}); !errors.Is(err, ErrNoRegistration) {
		t.Fatalf("without a device: %v", err)
	}
	client.SetRegistration("tok", "server-1")
	if err := client.SendLive(map[string]any{"seq": "1"}); !errors.Is(err, ErrNotLiveCapable) {
		t.Fatalf("a device that never declared live state must get nothing: %v", err)
	}
	client.SetLiveCapable(true)
	if err := client.SendLive(map[string]any{"seq": "1"}); err != nil {
		t.Fatal(err)
	}
	client.SetLiveCapable(false) // re-registering without the flag turns it off
	if err := client.SendLive(map[string]any{"seq": "2"}); !errors.Is(err, ErrNotLiveCapable) {
		t.Fatalf("flag cleared: %v", err)
	}
	if calls != 1 {
		t.Fatalf("HTTP calls = %d, want 1", calls)
	}
}

func TestSendLiveDropsDeadTokenAndReportsCredentials(t *testing.T) {
	client := New()
	client.SetRegistration("tok", "server-1")
	client.SetLiveCapable(true)
	capture(t, client, `{"data":{"status":"error","message":"gone","details":{"error":"DeviceNotRegistered"}}}`)
	if err := client.SendLive(map[string]any{"seq": "1"}); err == nil {
		t.Fatal("a rejected ticket is an error")
	}
	if client.HasToken() {
		t.Fatal("a dead token must be dropped")
	}

	client.SetRegistration("tok2", "server-1")
	capture(t, client, `{"data":{"status":"error","message":"no key","details":{"error":"InvalidCredentials"}}}`)
	if err := client.SendLive(map[string]any{"seq": "2"}); !errors.Is(err, ErrCredentials) {
		t.Fatalf("error = %v, want ErrCredentials", err)
	}
}
