package push

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// sentTo records every request's token and routing server reference, answering
// each with the ticket status the test chose for that token.
type sentTo struct {
	mu              sync.Mutex
	tokens          []string
	servers         map[string]string
	notificationIDs map[string]string
	response        map[string]string
}

func newSentTo(response map[string]string) *sentTo {
	return &sentTo{servers: map[string]string{}, notificationIDs: map[string]string{}, response: response}
}

func (s *sentTo) RoundTrip(request *http.Request) (*http.Response, error) {
	raw, _ := io.ReadAll(request.Body)
	var payload struct {
		To   string            `json:"to"`
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.tokens = append(s.tokens, payload.To)
	s.servers[payload.To] = payload.Data["server_id"]
	s.notificationIDs[payload.To] = payload.Data[dataNotificationID]
	body := s.response[payload.To]
	s.mu.Unlock()
	if body == "" {
		body = `{"data":{"status":"ok","id":"ticket-` + payload.To + `"}}`
	}
	return pushResponse(http.StatusOK, body), nil
}

func (s *sentTo) sorted() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]string(nil), s.tokens...)
	sort.Strings(out)
	return out
}

func TestEveryRegisteredDeviceGetsTheNotificationWithItsOwnServerRef(t *testing.T) {
	client := New()
	transport := newSentTo(nil)
	client.httpClient.Transport = transport
	observer := &recordingObserver{}
	client.SetObserver(observer)
	client.SetRegistration("phone", "token-phone", "server-a")
	client.SetRegistration("tablet", "token-tablet", "server-b")

	if err := client.NotifyWorker(KindWorkerDone, WorkerNotice{WorkerID: "w1", Window: "claude", Summary: "done"}); err != nil {
		t.Fatal(err)
	}
	if got := transport.sorted(); strings.Join(got, ",") != "token-phone,token-tablet" {
		t.Fatalf("sent to %v", got)
	}
	if transport.servers["token-phone"] != "server-a" || transport.servers["token-tablet"] != "server-b" {
		t.Fatalf("server refs = %v", transport.servers)
	}
	// Watch hears of one notification, not one per phone, and both phones'
	// copies carry its id so a tap on either answers it.
	sent, _ := observer.snapshot()
	if len(sent) != 1 || sent[0].Outcome != OutcomeAccepted || sent[0].Kind != KindWorkerDone || sent[0].Title != "claude finished" {
		t.Fatalf("events = %+v", sent)
	}
	for token, id := range transport.notificationIDs {
		if id != sent[0].ID {
			t.Fatalf("%s got notification id %q, the event's is %q", token, id, sent[0].ID)
		}
	}
	if len(transport.notificationIDs) != 2 {
		t.Fatalf("ids = %v", transport.notificationIDs)
	}
}

func TestADeviceRegisteringAgainReplacesItsOwnEntryOnly(t *testing.T) {
	client := New()
	transport := newSentTo(nil)
	client.httpClient.Transport = transport
	client.SetRegistration("phone", "token-old", "server-a")
	client.SetRegistration("tablet", "token-tablet", "server-b")
	client.SetRegistration("phone", "token-new", "server-a")

	if err := client.Send(Message{Title: "t"}); err != nil {
		t.Fatal(err)
	}
	if got := transport.sorted(); strings.Join(got, ",") != "token-new,token-tablet" {
		t.Fatalf("sent to %v", got)
	}
}

func TestATokenFollowsTheDeviceThatRegisteredItLast(t *testing.T) {
	client := New()
	transport := newSentTo(nil)
	client.httpClient.Transport = transport
	client.SetRegistration("old-pairing", "token-shared", "server-a")
	client.SetRegistration("new-pairing", "token-shared", "server-a")

	if err := client.Send(Message{Title: "t"}); err != nil {
		t.Fatal(err)
	}
	if got := transport.sorted(); len(got) != 1 {
		t.Fatalf("one install is one notification, sent to %v", got)
	}
	client.ForgetDevice("old-pairing")
	if !client.HasToken() {
		t.Fatal("forgetting the old pairing dropped the install's registration")
	}
}

func TestAForgottenDeviceHearsNothing(t *testing.T) {
	client := New()
	transport := newSentTo(nil)
	client.httpClient.Transport = transport
	client.SetRegistration("revoked", "token-revoked", "server-a")
	client.SetRegistration("kept", "token-kept", "server-b")
	client.ForgetDevice("revoked")
	client.ForgetDevice("never-registered")

	if err := client.Send(Message{Title: "t"}); err != nil {
		t.Fatal(err)
	}
	if got := transport.sorted(); strings.Join(got, ",") != "token-kept" {
		t.Fatalf("sent to %v", got)
	}
	client.ForgetDevice("kept")
	if client.HasToken() {
		t.Fatal("a token remains after every device was forgotten")
	}
	if err := client.Send(Message{Title: "t"}); !errors.Is(err, ErrNoRegistration) {
		t.Fatalf("err = %v, want ErrNoRegistration", err)
	}
}

func TestOneDeadDeviceDoesNotStopOrHideTheOthers(t *testing.T) {
	client := New()
	transport := newSentTo(map[string]string{
		"token-dead": `{"data":{"status":"error","message":"gone","details":{"error":"DeviceNotRegistered"}}}`,
	})
	client.httpClient.Transport = transport
	observer := &recordingObserver{}
	client.SetObserver(observer)
	client.SetRegistration("dead", "token-dead", "server-a")
	client.SetRegistration("live", "token-live", "server-b")

	if err := client.Send(Message{Title: "t"}); err != nil {
		t.Fatalf("a notification one device took must succeed, got %v", err)
	}
	if len(transport.sorted()) != 2 {
		t.Fatalf("sent to %v", transport.sorted())
	}
	sent, _ := observer.snapshot()
	if len(sent) != 1 || sent[0].Outcome != OutcomeAccepted {
		t.Fatalf("a notification one device took is accepted, got %+v", sent)
	}

	// Only the dead device was dropped.
	next := newSentTo(nil)
	client.httpClient.Transport = next
	if err := client.Send(Message{Title: "t"}); err != nil {
		t.Fatal(err)
	}
	if got := next.sorted(); strings.Join(got, ",") != "token-live" {
		t.Fatalf("after the drop, sent to %v", got)
	}
}

func TestAllDevicesFailingReturnsEveryFailure(t *testing.T) {
	client := New()
	rejected := `{"data":{"status":"error","message":"no credentials","details":{"error":"InvalidCredentials"}}}`
	client.httpClient.Transport = newSentTo(map[string]string{"token-a": rejected, "token-b": rejected})
	client.SetRegistration("a", "token-a", "server-a")
	client.SetRegistration("b", "token-b", "server-b")

	err := client.Send(Message{Title: "t"})
	if !errors.Is(err, ErrCredentials) {
		t.Fatalf("err = %v, want ErrCredentials", err)
	}
	if strings.Contains(err.Error(), "token-a") || strings.Contains(err.Error(), "token-b") {
		t.Fatalf("a push token leaked into the error: %v", err)
	}
}

// A device that stays rejected must not turn every push into a log line while
// another device keeps receiving: its credential failure is reported once, by
// the client, and its gone token once, when dropped.
func TestAKnownBadDeviceIsNotLoggedAgainOnEveryPush(t *testing.T) {
	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previous)

	client := New()
	rejected := `{"data":{"status":"error","message":"no credentials","details":{"error":"InvalidCredentials"}}}`
	client.httpClient.Transport = newSentTo(map[string]string{"token-ios": rejected})
	client.SetRegistration("ios", "token-ios", "server-a")
	client.SetRegistration("android", "token-android", "server-b")

	for range 5 {
		if err := client.Send(Message{Title: "t"}); err != nil {
			t.Fatal(err)
		}
	}
	if got := strings.Count(logs.String(), "reported once per device"); got != 1 {
		t.Fatalf("the credential problem was logged %d times:\n%s", got, logs.String())
	}
	if strings.Contains(logs.String(), "did not get the notification") {
		t.Fatalf("a known bad device was logged on every push:\n%s", logs.String())
	}
}

func TestAllDevicesFailingIsOneFailedEventWithTheFirstReason(t *testing.T) {
	client := New()
	rejected := `{"data":{"status":"error","message":"no credentials","details":{"error":"InvalidCredentials"}}}`
	client.httpClient.Transport = newSentTo(map[string]string{"token-a": rejected, "token-b": rejected})
	observer := &recordingObserver{}
	client.SetObserver(observer)
	client.SetRegistration("a", "token-a", "server-a")
	client.SetRegistration("b", "token-b", "server-b")
	_ = client.Send(Message{Title: "t"})
	sent, _ := observer.snapshot()
	if len(sent) != 1 || sent[0].Outcome != OutcomeFailed || sent[0].Reason != "InvalidCredentials" {
		t.Fatalf("events = %+v", sent)
	}
}

func TestOnlyOneDeviceReportsTheNotificationsDeliveryReceipt(t *testing.T) {
	client := New()
	client.receiptDelays = []time.Duration{10 * time.Millisecond}
	var mu sync.Mutex
	posts := 0
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if strings.HasSuffix(request.URL.Path, "getReceipts") {
			return pushResponse(http.StatusOK, `{"data":{"ticket-a":{"status":"ok"},"ticket-b":{"status":"ok"}}}`), nil
		}
		mu.Lock()
		posts++
		id := "ticket-a"
		if posts == 2 {
			id = "ticket-b"
		}
		mu.Unlock()
		return pushResponse(http.StatusOK, `{"data":{"status":"ok","id":"`+id+`"}}`), nil
	})
	observer := &recordingObserver{}
	client.SetObserver(observer)
	client.SetRegistration("a", "token-a", "server-a")
	client.SetRegistration("b", "token-b", "server-b")
	if err := client.Send(Message{Title: "t"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, receipts := observer.snapshot(); len(receipts) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	sent, receipts := observer.snapshot()
	if len(sent) != 1 || len(receipts) != 1 || receipts[0].ID != sent[0].ID || !receipts[0].OK {
		t.Fatalf("sent=%+v receipts=%+v", sent, receipts)
	}
}
