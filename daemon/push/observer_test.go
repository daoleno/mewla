package push

import (
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type recordingObserver struct {
	mu       sync.Mutex
	sent     []Sent
	receipts []Receipt
}

func (o *recordingObserver) NotificationSent(event Sent) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sent = append(o.sent, event)
}

func (o *recordingObserver) NotificationReceipt(receipt Receipt) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.receipts = append(o.receipts, receipt)
}

func (o *recordingObserver) snapshot() ([]Sent, []Receipt) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]Sent(nil), o.sent...), append([]Receipt(nil), o.receipts...)
}

func TestObserverHearsNotSentWithoutRegistration(t *testing.T) {
	client := New()
	observer := &recordingObserver{}
	client.SetObserver(observer)
	if err := client.NotifyWorkerDone("agent-1", "Agent", "finished the tests"); err != ErrNoRegistration {
		t.Fatalf("err = %v", err)
	}
	sent, _ := observer.snapshot()
	if len(sent) != 1 {
		t.Fatalf("sent = %#v", sent)
	}
	event := sent[0]
	if event.Kind != KindWorkerDone || event.Outcome != OutcomeNotSent || event.Reason != ReasonNoRegistration ||
		event.Title != "Agent finished" || event.Body != "finished the tests" || event.Refs["worker_id"] != "agent-1" {
		t.Fatalf("event = %#v", event)
	}
}

func TestObserverHearsTicketReceiptAndNeverTheToken(t *testing.T) {
	client := New()
	client.receiptDelays = []time.Duration{time.Millisecond}
	client.SetRegistration("ExponentPushToken[secret]", "server-1")
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() == expoPushURL {
			return pushResponse(http.StatusOK, `{"data":{"status":"ok","id":"ticket-1"}}`), nil
		}
		return pushResponse(http.StatusOK, `{"data":{"ticket-1":{"status":"error","message":"bad ExponentPushToken[secret]","details":{"error":"MessageRateExceeded"}}}}`), nil
	})
	observer := &recordingObserver{}
	client.SetObserver(observer)
	notify := client.BrainNotifier(KindBrainReply)
	if err := notify("thread-1", "Brain", "Ship it?"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, receipts := observer.snapshot(); len(receipts) > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	sent, receipts := observer.snapshot()
	if len(sent) != 1 || len(receipts) != 1 {
		t.Fatalf("sent=%#v receipts=%#v", sent, receipts)
	}
	event, receipt := sent[0], receipts[0]
	if event.Kind != KindBrainReply || event.Outcome != OutcomeAccepted || event.Refs["brain_thread_id"] != "thread-1" {
		t.Fatalf("event = %#v", event)
	}
	if _, leaked := event.Refs["server_id"]; leaked {
		t.Fatalf("server reference leaked into refs: %#v", event.Refs)
	}
	if receipt.ID != event.ID || receipt.OK || receipt.Reason != "MessageRateExceeded" {
		t.Fatalf("receipt = %#v, event id %q", receipt, event.ID)
	}
	for _, text := range []string{receipt.Detail, event.Detail, event.Title, event.Body} {
		if strings.Contains(text, "secret") {
			t.Fatalf("token leaked: %q", text)
		}
	}
}

func TestObserverHearsCredentialRejection(t *testing.T) {
	client := New()
	client.SetRegistration("ExponentPushToken[abc]", "server-1")
	client.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return pushResponse(http.StatusOK, `{"data":{"status":"error","message":"no key","details":{"error":"InvalidCredentials"}}}`), nil
	})
	observer := &recordingObserver{}
	client.SetObserver(observer)
	if err := client.NotifyScheduledResult("Report", "completed", "thread", "result"); err == nil {
		t.Fatal("expected an error")
	}
	sent, _ := observer.snapshot()
	if len(sent) != 1 || sent[0].Outcome != OutcomeFailed || sent[0].Reason != "InvalidCredentials" {
		t.Fatalf("sent = %#v", sent)
	}
}

func TestObserverHearsAnOkReceipt(t *testing.T) {
	client := New()
	client.receiptDelays = []time.Duration{time.Millisecond}
	client.SetRegistration("ExponentPushToken[abc]", "server-1")
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() == expoPushURL {
			return pushResponse(http.StatusOK, `{"data":{"status":"ok","id":"ticket-9"}}`), nil
		}
		return pushResponse(http.StatusOK, `{"data":{"ticket-9":{"status":"ok"}}}`), nil
	})
	observer := &recordingObserver{}
	client.SetObserver(observer)
	if err := client.NotifyWorkerDone("agent-1", "Agent", "done"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, receipts := observer.snapshot(); len(receipts) > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	sent, receipts := observer.snapshot()
	if len(sent) != 1 || len(receipts) != 1 || !receipts[0].OK || receipts[0].ID != sent[0].ID {
		t.Fatalf("sent=%#v receipts=%#v", sent, receipts)
	}
}
