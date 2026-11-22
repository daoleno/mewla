package work

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"sync"
	"time"
)

// antigravityLink is a Session's connection to the daemon: it reports what
// agy does and takes messages, stops, answers and model changes back. While
// the daemon is away the Session carries on; the link connects again and says
// everything that is still true (the conversation, a running turn, pending
// decisions) in its hello.
type antigravityLink struct {
	socket  string
	id      string
	session *antigravitySession

	mu   sync.Mutex
	conn net.Conn
}

func newAntigravityLink(socket, id string, session *antigravitySession) *antigravityLink {
	return &antigravityLink{socket: socket, id: id, session: session}
}

// AntigravityLinkMessage is one line between a Session and the daemon.
type AntigravityLinkMessage struct {
	T            string           `json:"t"`
	ID           string           `json:"id,omitempty"`
	Req          string           `json:"req,omitempty"`
	Session      string           `json:"session,omitempty"`
	Conversation string           `json:"conversation,omitempty"`
	Log          string           `json:"log,omitempty"`
	Running      bool             `json:"running,omitempty"`
	Turn         string           `json:"turn,omitempty"`
	Text         string           `json:"text,omitempty"`
	SHA          string           `json:"sha,omitempty"`
	Tool         string           `json:"tool,omitempty"`
	Summary      string           `json:"summary,omitempty"`
	Stop         string           `json:"stop,omitempty"`
	Error        string           `json:"error,omitempty"`
	ErrorCode    string           `json:"error_code,omitempty"`
	HTTPStatus   int              `json:"http_status,omitempty"`
	Usage        json.RawMessage  `json:"usage,omitempty"`
	Ask          *AntigravityAsk  `json:"ask,omitempty"`
	Asks         []AntigravityAsk `json:"asks,omitempty"`
	Option       string           `json:"option,omitempty"`
	By           string           `json:"by,omitempty"`
	Model        string           `json:"model,omitempty"`
	Effort       string           `json:"effort,omitempty"`
	Mode         string           `json:"mode,omitempty"`
	OK           bool             `json:"ok,omitempty"`
	Models       json.RawMessage  `json:"models,omitempty"`
	AgentPID     int              `json:"agent_pid,omitempty"`
}

func (l *antigravityLink) run(ctx context.Context) {
	for ctx.Err() == nil {
		conn, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "unix", l.socket)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}
		l.mu.Lock()
		l.conn = conn
		l.mu.Unlock()
		l.hello()
		l.serve(ctx, conn)
		l.mu.Lock()
		if l.conn == conn {
			l.conn = nil
		}
		l.mu.Unlock()
		_ = conn.Close()
	}
}

func (l *antigravityLink) serve(ctx context.Context, conn net.Conn) {
	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()
	reader := bufio.NewReaderSize(conn, 1<<20)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return
		}
		var message AntigravityLinkMessage
		if json.Unmarshal(line, &message) != nil {
			continue
		}
		go l.handle(ctx, message)
	}
}

func (l *antigravityLink) handle(ctx context.Context, message AntigravityLinkMessage) {
	s := l.session
	reply := func(err error, models json.RawMessage) {
		answer := map[string]any{"t": "reply", "req": message.Req, "ok": err == nil}
		if err != nil {
			answer["error"] = err.Error()
		}
		if models != nil {
			answer["models"] = models
		}
		l.send(answer)
	}
	switch message.T {
	case "send":
		err := s.submit(message.ID, message.Text)
		if err != nil {
			l.send(map[string]any{"t": "send_failed", "id": message.ID, "error": err.Error()})
		} else {
			l.send(map[string]any{"t": "sent", "id": message.ID})
		}
	case "abort":
		s.cancel()
	case "decide":
		err := s.answer(message.ID, message.Option, firstNonEmpty(message.By, "mewla"))
		if message.Req != "" {
			reply(err, nil)
		}
	case "models":
		raw, _ := json.Marshal(s.modelsSnapshot())
		reply(nil, raw)
	case "set_model":
		callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := s.setChoices(callCtx, message.Model, message.Effort, message.Mode)
		cancel()
		raw, _ := json.Marshal(s.modelsSnapshot())
		reply(err, raw)
	}
}

// hello tells the daemon who this Session is and what is true now.
func (l *antigravityLink) hello() {
	facts := l.session.helloFacts()
	facts["asks"] = l.session.pendingAsks()
	l.send(facts)
}

// send writes one message; with no daemon connected it is dropped (the hello
// after a reconnect restates what still matters).
func (l *antigravityLink) send(v any) {
	raw, err := json.Marshal(v)
	if err != nil {
		return
	}
	l.mu.Lock()
	conn := l.conn
	l.mu.Unlock()
	if conn == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, _ = conn.Write(append(raw, '\n'))
}
