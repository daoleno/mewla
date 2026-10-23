package telegram

import (
	"bytes"
	"log"
	"os"
	"strings"
	"sync"
	"testing"
)

type logSink struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logSink) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *logSink) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

func captureLog(t *testing.T) *logSink {
	t.Helper()
	sink := &logSink{}
	log.SetOutput(sink)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return sink
}

func TestSwallowedStoreWritesAreReportedOncePerCause(t *testing.T) {
	manager, _, _, _ := configuredManager(t)
	bindOwner(t, manager, 1, 10, 10)
	failWrites(t, manager.store)
	sink := captureLog(t)

	for range 3 {
		manager.recordError("Telegram receive is temporarily unavailable.")
		manager.enqueueText("ack:1", "hello", 0)
		manager.enqueueTopicText("topic:ack:1", "hello", 55, 0)
	}
	out := sink.String()
	for _, scope := range []string{"telegram record-error", "telegram enqueue-text", "telegram enqueue-topic-text"} {
		if n := strings.Count(out, "WARN "+scope+":"); n != 1 {
			t.Fatalf("%s logged %d times, want once:\n%s", scope, n, out)
		}
	}
}

func TestReportedStoreWriteFailureClearsAfterASuccess(t *testing.T) {
	manager, _, _, _ := configuredManager(t)
	bindOwner(t, manager, 1, 10, 10)
	restore := failWrites(t, manager.store)
	sink := captureLog(t)
	manager.recordError("one")
	restore()
	manager.recordError("two") // succeeds and re-arms the report
	failWrites(t, manager.store)
	manager.recordError("three")
	if n := strings.Count(sink.String(), "WARN telegram record-error:"); n != 2 {
		t.Fatalf("a failure after a recovery must be reported again; logged %d:\n%s", n, sink.String())
	}
}

func TestDeliveryOutcomeThatCannotBeSavedIsReported(t *testing.T) {
	manager, _, api, _ := configuredManager(t)
	bindOwner(t, manager, 1, 10, 10)
	manager.enqueueText("ack:out", "hello", 0)
	api.nextSendErr = &APIError{Code: 400, description: "Bad Request: chat not found"}
	api.sendStarted = make(chan struct{})
	api.blockSend = make(chan struct{})
	sink := captureLog(t)
	done := make(chan error, 1)
	go func() { done <- manager.deliverOne(t.Context()) }()
	<-api.sendStarted
	// The row is already marked dispatching; saving its terminal state fails.
	failWrites(t, manager.store)
	close(api.blockSend)
	<-done
	if !strings.Contains(sink.String(), "WARN telegram record-delivery-outcome:") {
		t.Fatalf("a lost delivery outcome was silent:\n%s", sink.String())
	}
}
