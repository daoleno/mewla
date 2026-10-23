package push

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/diag"
)

func TestFormatNotificationWorkerLabel(t *testing.T) {
	tests := []struct {
		name       string
		workerName string
		workerID   string
		want       string
	}{
		{name: "clean shell path and session suffix", workerName: "./bin/mewla (main:7)", workerID: "main:7", want: "mewla"},
		{name: "fallback to agent id", workerName: "", workerID: "main:7", want: "main:7"},
		{name: "keep simple project name", workerName: "backend-api", workerID: "main:7", want: "backend-api"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatNotificationWorkerLabel(tt.workerName, tt.workerID); got != tt.want {
				t.Fatalf("formatNotificationAgentLabel() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeNotificationSummary(t *testing.T) {
	summary := "2026/04/02 14:37:37   permission denied writing dist/index.js"
	want := "permission denied writing dist/index.js"

	if got := normalizeNotificationSummary(summary); got != want {
		t.Fatalf("normalizeNotificationSummary() = %q, want %q", got, want)
	}
}

func TestBuildNotificationBodyFallback(t *testing.T) {
	if got := buildNotificationBody("", "Session finished."); got != "Session finished." {
		t.Fatalf("buildNotificationBody() = %q, want %q", got, "Session finished.")
	}
}

func TestShortPushRegistrationNeverPanicsOrLogsCredential(t *testing.T) {
	var logs bytes.Buffer
	previousWriter := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previousWriter)

	client := New()
	client.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return pushResponse(http.StatusOK, `{"data":{"status":"ok","id":"ticket-1"}}`), nil
	})
	client.SetRegistration("x", "server-1")
	if err := client.NotifyWorkerDone("agent-1", "Agent", "finished"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logs.String(), "x") {
		t.Fatalf("push credential leaked to logs: %q", logs.String())
	}
}

func TestNotifyWithoutRegistrationDoesNotAttemptHTTP(t *testing.T) {
	client := New()
	var requests atomic.Int64
	client.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return pushResponse(http.StatusOK, `{"data":{"status":"ok"}}`), nil
	})

	err := client.NotifyWorkerBlocked("agent-1", "Agent", "waiting")
	if !errors.Is(err, ErrNoRegistration) {
		t.Fatalf("error = %v, want ErrNoRegistration", err)
	}
	if requests.Load() != 0 {
		t.Fatalf("HTTP attempts = %d, want 0", requests.Load())
	}
}

func TestNotifyFailuresAreSingleAttempts(t *testing.T) {
	tests := []struct {
		name      string
		transport roundTripFunc
	}{
		{
			name: "transport",
			transport: func(*http.Request) (*http.Response, error) {
				return nil, errors.New("network unavailable")
			},
		},
		{
			name: "HTTP",
			transport: func(*http.Request) (*http.Response, error) {
				return pushResponse(http.StatusServiceUnavailable, `{}`), nil
			},
		},
		{
			name: "ticket",
			transport: func(*http.Request) (*http.Response, error) {
				return pushResponse(http.StatusOK, `{"data":{"status":"error","message":"rejected"}}`), nil
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := New()
			client.SetRegistration("credential-token", "server-1")
			var attempts atomic.Int64
			client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
				attempts.Add(1)
				return test.transport(request)
			})
			if err := client.NotifyWorkerFailed("agent-1", "Agent", "failed"); err == nil {
				t.Fatal("expected notification failure")
			} else if strings.Contains(err.Error(), "credential-token") {
				t.Fatalf("credential leaked in error: %v", err)
			}
			if attempts.Load() != 1 {
				t.Fatalf("attempts = %d, want 1", attempts.Load())
			}
		})
	}
}

func TestScheduledResultPushUsesOnlyRoutingAndGenericCopy(t *testing.T) {
	tests := []struct {
		status, wantTitle, wantBody, wantPriority string
	}{
		{
			status: "completed", wantTitle: "Frozen report is ready",
			wantBody: "Open Brain for the scheduled result.", wantPriority: "default",
		},
		{
			status: "failed", wantTitle: "Frozen report failed",
			wantBody: "Open Brain for the failure outcome.", wantPriority: "high",
		},
	}
	for _, test := range tests {
		t.Run(test.status, func(t *testing.T) {
			client := New()
			client.SetRegistration("token-1", "server-1")
			var payload map[string]any
			client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				return pushResponse(http.StatusOK, `{"data":{"status":"ok","id":"ticket-1"}}`), nil
			})

			if err := client.NotifyScheduledResult(
				"Frozen report", test.status, "thread-frozen", "calendar_result:item:run",
			); err != nil {
				t.Fatal(err)
			}
			if payload["channelId"] != "mewla-agents" || payload["to"] != "token-1" || payload["title"] != test.wantTitle ||
				payload["body"] != test.wantBody || payload["priority"] != test.wantPriority {
				t.Fatalf("payload = %#v", payload)
			}
			if len(payload) != 7 {
				t.Fatalf("unexpected scheduled payload fields: %#v", payload)
			}
			data, ok := payload["data"].(map[string]any)
			if !ok {
				t.Fatalf("data = %#v", payload["data"])
			}
			wantData := map[string]string{
				"screen": "brain", "server_id": "server-1",
				"brain_thread_id":  "thread-frozen",
				"brain_message_id": "calendar_result:item:run",
			}
			// The notification id is how the app reports a tap; it is not routing.
			if id, _ := data["notification_id"].(string); !strings.HasPrefix(id, "n_") {
				t.Fatalf("notification_id = %#v", data["notification_id"])
			}
			if len(data) != len(wantData)+1 {
				t.Fatalf("unexpected scheduled routing fields: %#v", data)
			}
			for key, want := range wantData {
				if data[key] != want {
					t.Fatalf("data[%q] = %#v, want %q", key, data[key], want)
				}
			}
			encoded, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), "private deliverable") || strings.Contains(string(encoded), "private failure") {
				t.Fatalf("terminal contents leaked: %s", encoded)
			}
		})
	}
}

func TestConcurrentRegistrationAndNotificationUseCoherentSnapshot(t *testing.T) {
	client := New()
	pairs := map[string]string{
		"token-a": "server-a",
		"token-b": "server-b",
	}
	client.SetRegistration("token-a", "server-a")
	var attempts atomic.Int64
	errCh := make(chan error, 512)
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var payload struct {
			To   string            `json:"to"`
			Data map[string]string `json:"data"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			errCh <- err
		} else if pairs[payload.To] != payload.Data["server_id"] {
			errCh <- errors.New("token and server_ref came from different registrations")
		}
		attempts.Add(1)
		return pushResponse(http.StatusOK, `{"data":{"status":"ok","id":"ticket"}}`), nil
	})

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for index := range 250 {
			if index%2 == 0 {
				client.SetRegistration("token-a", "server-a")
			} else {
				client.SetRegistration("token-b", "server-b")
			}
		}
	}()
	go func() {
		defer wg.Done()
		for range 250 {
			if err := client.NotifyWorkerDone("agent-1", "Agent", "done"); err != nil {
				errCh <- err
			}
		}
	}()
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
	if attempts.Load() != 250 {
		t.Fatalf("attempts = %d, want 250", attempts.Load())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func pushResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestTicketDeviceNotRegisteredDropsOnlyTheSentToken(t *testing.T) {
	var logs bytes.Buffer
	previousWriter := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previousWriter)

	client := New()
	client.SetRegistration("ExponentPushToken[old]", "server-1")
	client.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		// The app re-registers while the send is in flight.
		client.SetRegistration("ExponentPushToken[new]", "server-1")
		return pushResponse(http.StatusOK, `{"data":{"status":"error","message":"\"ExponentPushToken[old]\" is not a registered push notification recipient","details":{"error":"DeviceNotRegistered"}}}`), nil
	})
	err := client.sendTo("ExponentPushToken[old]", Message{Title: "t"})
	if err == nil || !strings.Contains(err.Error(), "DeviceNotRegistered") {
		t.Fatalf("error = %v, want DeviceNotRegistered", err)
	}
	if strings.Contains(err.Error(), "ExponentPushToken") {
		t.Fatalf("token leaked in error: %v", err)
	}
	if !client.HasToken() {
		t.Fatal("a newer registration must survive a stale DeviceNotRegistered")
	}

	client.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return pushResponse(http.StatusOK, `{"data":{"status":"error","details":{"error":"DeviceNotRegistered"}}}`), nil
	})
	_ = client.sendTo("ExponentPushToken[new]", Message{Title: "t"})
	if client.HasToken() {
		t.Fatal("DeviceNotRegistered should drop the matching token")
	}
}

func TestHTTPFailureNamesExpoErrorWithoutToken(t *testing.T) {
	client := New()
	client.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return pushResponse(http.StatusBadRequest, `{"errors":[{"code":"PUSH_BAD_PROJECT","message":"bad project for credential-token"}]}`), nil
	})
	err := client.sendTo("credential-token", Message{Title: "t"})
	if err == nil || !strings.Contains(err.Error(), "400") || !strings.Contains(err.Error(), "PUSH_BAD_PROJECT") ||
		strings.Contains(err.Error(), "credential-token") {
		t.Fatalf("error = %v", err)
	}
}

func TestReceiptErrorIsLoggedAndDropsDeadDevice(t *testing.T) {
	logs := &lockedBuffer{}
	previousWriter := log.Writer()
	log.SetOutput(logs)
	defer log.SetOutput(previousWriter)

	after := diag.Last()
	client := New()
	client.receiptDelays = []time.Duration{time.Millisecond, time.Millisecond}
	client.SetRegistration("credential-token", "server-1")
	var receiptRequests atomic.Int64
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() == expoPushURL {
			return pushResponse(http.StatusOK, `{"data":{"status":"ok","id":"ticket-1"}}`), nil
		}
		// The first look finds nothing yet; the second finds the failure.
		if receiptRequests.Add(1) == 1 {
			return pushResponse(http.StatusOK, `{"data":{}}`), nil
		}
		return pushResponse(http.StatusOK, `{"data":{"ticket-1":{"status":"error","message":"gone","details":{"error":"DeviceNotRegistered"}}}}`), nil
	})
	if err := client.NotifyWorkerDone("agent-1", "Agent", "done"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for client.HasToken() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if client.HasToken() {
		t.Fatal("receipt DeviceNotRegistered should drop the token")
	}
	time.Sleep(20 * time.Millisecond) // the drop is logged right after it happens
	if got := logs.String(); !strings.Contains(got, "ticket-1") || !strings.Contains(got, "DeviceNotRegistered") || strings.Contains(got, "credential-token") {
		t.Fatalf("log = %q", got)
	}
	if receiptRequests.Load() != 2 {
		t.Fatalf("receipt lookups = %d, want 2", receiptRequests.Load())
	}
	events, _ := diag.After(after)
	if len(events) != 1 || events[0].Kind != "push.delivery" || !strings.Contains(events[0].Detail, "DeviceNotRegistered") {
		t.Fatalf("diag events = %+v, want one push.delivery", events)
	}
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestCredentialFailureIsReportedOncePerToken(t *testing.T) {
	logs := &lockedBuffer{}
	previousWriter := log.Writer()
	log.SetOutput(logs)
	defer log.SetOutput(previousWriter)

	after := diag.Last()
	client := New()
	client.SetRegistration("credential-token", "server-1")
	client.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return pushResponse(http.StatusOK, `{"data":{"status":"error","message":"Unable to retrieve the FCM server key","details":{"error":"InvalidCredentials"}}}`), nil
	})
	for range 3 {
		err := client.NotifyWorkerFailed("agent-1", "Agent", "failed")
		if !errors.Is(err, ErrCredentials) || !strings.Contains(err.Error(), "InvalidCredentials") {
			t.Fatalf("error = %v, want ErrCredentials naming InvalidCredentials", err)
		}
	}
	if got := strings.Count(logs.String(), "InvalidCredentials"); got != 1 {
		t.Fatalf("credential problem logged %d times, want 1: %q", got, logs.String())
	}
	if strings.Contains(logs.String(), "credential-token") {
		t.Fatalf("token leaked: %q", logs.String())
	}
	if events, _ := diag.After(after); len(events) != 1 || events[0].Kind != "push.credentials" || events[0].Severity != diag.SeverityError {
		t.Fatalf("diag events = %+v, want one push.credentials error", events)
	}
	if !client.HasToken() {
		t.Fatal("a credential problem is not the device's fault; keep the token")
	}
}
