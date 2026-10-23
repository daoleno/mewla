package work

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// DSHInteraction is one pending native approval or question, projected into
// the shape the App answers. RPCID is the native forwarded-event id.
type DSHInteraction struct {
	RPCID   string          `json:"rpcId"`
	Payload json.RawMessage `json:"payload"`
}
type dshInteractionOwner struct {
	mu        sync.Mutex
	sessionID string
	epoch     string
	connected bool
	clientID  string
	pending   map[string]DSHInteraction
	// rev counts every change to the snapshot; changed is closed and replaced
	// on each one so a waiting reader wakes without polling.
	rev     int64
	changed chan struct{}
}

func newDSHInteractionOwner(id string) *dshInteractionOwner {
	return &dshInteractionOwner{sessionID: id, pending: map[string]DSHInteraction{}, changed: make(chan struct{})}
}

// bump records a change to the snapshot. The caller holds o.mu.
func (o *dshInteractionOwner) bump() {
	o.rev++
	close(o.changed)
	o.changed = make(chan struct{})
}

// wait returns once the snapshot differs from the revision the caller last
// saw, or after the timeout or context end, whichever comes first. The caller
// then reads the snapshot, which carries the current revision.
func (o *dshInteractionOwner) wait(ctx context.Context, after int64, timeout time.Duration) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		o.mu.Lock()
		if o.rev != after {
			o.mu.Unlock()
			return
		}
		changed := o.changed
		o.mu.Unlock()
		select {
		case <-changed:
		case <-timer.C:
			return
		case <-ctx.Done():
			return
		}
	}
}

// reset starts a new answer generation. Pending items belong to the native
// event client that delivered them and never survive its stream.
func (o *dshInteractionOwner) reset(connected bool, clientID string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.epoch = uuid.NewString()
	o.connected = connected
	o.clientID = clientID
	o.pending = map[string]DSHInteraction{}
	o.bump()
}

// consume applies one forwarded-event frame from the native `$events` stream.
// It returns the event id of a waterfall Mewla does not answer, which the
// caller passes on to the next listener.
func (o *dshInteractionOwner) consume(value []byte) (pass string) {
	var frame struct {
		Type     string          `json:"type"`
		ClientID string          `json:"clientId"`
		Event    string          `json:"event"`
		EventID  string          `json:"eventId"`
		Request  json.RawMessage `json:"request"`
	}
	if json.Unmarshal(value, &frame) != nil {
		return ""
	}
	switch frame.Type {
	case "ready":
		o.reset(frame.ClientID != "", frame.ClientID)
	case "cancel":
		o.mu.Lock()
		if _, ok := o.pending[frame.EventID]; ok {
			delete(o.pending, frame.EventID)
			o.bump()
		}
		o.mu.Unlock()
	case "waterfall":
		if frame.EventID == "" {
			return ""
		}
		var request struct {
			ToolName  string          `json:"toolName"`
			CallID    string          `json:"callId"`
			Reason    string          `json:"reason"`
			Questions json.RawMessage `json:"questions"`
			Wait      struct {
				CallID string `json:"callId"`
			} `json:"wait"`
		}
		if json.Unmarshal(frame.Request, &request) != nil {
			return frame.EventID
		}
		payload := map[string]any{"sessionId": o.sessionID}
		switch frame.Event {
		case "approval/request":
			payload["type"] = "approval/requested"
			payload["approvalId"] = frame.EventID
			payload["toolName"] = request.ToolName
			if request.CallID != "" {
				payload["callId"] = request.CallID
			}
			if request.Reason != "" {
				payload["reason"] = request.Reason
			}
		case "user-questions/request":
			payload["type"] = "question/requested"
			payload["questions"] = request.Questions
			if request.Wait.CallID != "" {
				payload["callId"] = request.Wait.CallID
			}
		default:
			return frame.EventID
		}
		encoded, _ := json.Marshal(payload)
		o.mu.Lock()
		defer o.mu.Unlock()
		if len(o.pending) >= 32 {
			return frame.EventID
		}
		o.pending[frame.EventID] = DSHInteraction{RPCID: frame.EventID, Payload: encoded}
		o.bump()
	}
	return ""
}
func (o *dshInteractionOwner) snapshot() any {
	o.mu.Lock()
	defer o.mu.Unlock()
	items := make([]DSHInteraction, 0, len(o.pending))
	for _, item := range o.pending {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].RPCID < items[j].RPCID })
	return struct {
		Epoch     string           `json:"epoch"`
		Connected bool             `json:"connected"`
		Items     []DSHInteraction `json:"items"`
		Rev       int64            `json:"rev"`
	}{o.epoch, o.connected, items, o.rev}
}

// run keeps one native forwarded-event stream open on `/api/remote.mux`.
// Approvals and questions reach only clients holding this stream; without it
// DSH fails them closed.
func (o *dshInteractionOwner) run(ctx context.Context, native *dshNative) {
	for ctx.Err() == nil {
		o.reset(false, "")
		o.stream(ctx, native)
		o.reset(false, "")
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

func (o *dshInteractionOwner) stream(ctx context.Context, native *dshNative) {
	header := http.Header{}
	header.Set("Cookie", native.cookie)
	address := "ws" + strings.TrimPrefix(native.address, "http") + "/api/remote.mux"
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.DialContext(ctx, address, header)
	if err != nil {
		return
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	conn.SetReadLimit(4 << 20)
	const streamID = "mewla-events"
	if conn.WriteJSON(map[string]any{"type": "open", "streamId": streamID, "endpoint": "$events", "payload": map[string]any{"args": map[string]any{}}}) != nil {
		return
	}
	for {
		var message struct {
			Type     string          `json:"type"`
			StreamID string          `json:"streamId"`
			Value    json.RawMessage `json:"value"`
		}
		if conn.ReadJSON(&message) != nil || message.StreamID != streamID || message.Type != "item" {
			return
		}
		if pass := o.consume(message.Value); pass != "" {
			o.mu.Lock()
			clientID := o.clientID
			o.mu.Unlock()
			go func() {
				requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				defer cancel()
				_ = native.rpc(requestCtx, "$events/result", dshEventResult(clientID, pass, map[string]any{"kind": "next"}), nil)
			}()
		}
	}
}

func dshEventResult(clientID, eventID string, outcome map[string]any) map[string]any {
	return map[string]any{"clientId": clientID, "eventId": eventID, "outcome": outcome}
}

func (o *dshInteractionOwner) answer(ctx context.Context, native *dshNative, payload map[string]any) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	epoch, _ := payload["epoch"].(string)
	rpcID, _ := payload["rpcId"].(string)
	pending, ok := o.pending[rpcID]
	if !o.connected || epoch != o.epoch || !ok {
		return fmt.Errorf("This request is no longer pending. Refresh the Session.")
	}
	var request struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(pending.Payload, &request) != nil {
		return fmt.Errorf("invalid pending request")
	}
	var outcome map[string]any
	switch request.Type {
	case "approval/requested":
		decision, _ := payload["outcome"].(string)
		if decision != "allowed-once" && decision != "rejected" {
			return fmt.Errorf("choose Allow once or Reject")
		}
		outcome = map[string]any{"kind": "result", "value": decision}
	case "question/requested":
		if payload["cancel"] == true {
			outcome = map[string]any{"kind": "rejected", "error": map[string]any{"name": "AbortError", "message": "Cancelled by user"}}
		} else {
			answer, ok := payload["answer"].(map[string]any)
			if !ok {
				return fmt.Errorf("question answer is required")
			}
			outcome = map[string]any{"kind": "result", "value": answer}
		}
	default:
		return fmt.Errorf("unsupported interaction")
	}
	if err := native.rpc(ctx, "$events/result", dshEventResult(o.clientID, rpcID, outcome), nil); err != nil {
		return fmt.Errorf("Native DSH rejected the answer: %w", err)
	}
	delete(o.pending, rpcID)
	o.bump()
	return nil
}
