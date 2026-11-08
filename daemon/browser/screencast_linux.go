//go:build linux

package browser

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// screencast is the live view of a window: Chrome's own screencast of the
// page the window shows, as the frames the viewer takes, one at a time. Chrome
// sends the next frame only after the previous one is acknowledged, so the
// viewer's pace is the page's pace. When the window shows another page, the
// view moves to it.
type screencast struct {
	w      *linuxWindow
	out    chan []byte
	fail   chan error
	stop   chan struct{}
	once   sync.Once
	mu     sync.Mutex
	conn   *websocket.Conn
	seq    uint64
	acks   map[uint64]int // our sequence number to Chrome's frame session id
	writeM sync.Mutex
}

func (r *linuxWindow) Stream(ctx context.Context) (Stream, error) {
	if !r.Alive() {
		return nil, ErrUnavailable
	}
	s := &screencast{w: r, out: make(chan []byte, 2), fail: make(chan error, 1), stop: make(chan struct{}), acks: map[uint64]int{}}
	r.mu.Lock()
	if r.streams == nil {
		r.streams = map[*screencast]bool{}
	}
	r.streams[s] = true
	r.mu.Unlock()
	go s.run()
	return s, nil
}

// retarget drops the connection to the old page; run opens the new one.
func (s *screencast) retarget() {
	s.mu.Lock()
	if s.conn != nil {
		_ = s.conn.Close()
	}
	s.mu.Unlock()
}

func (s *screencast) run() {
	failures := 0
	for {
		select {
		case <-s.stop:
			return
		default:
		}
		if !s.w.Alive() {
			s.finish(errors.New("The browser stopped"))
			return
		}
		frames, err := s.attach()
		if frames > 0 {
			failures = 0
		} else {
			failures++
		}
		if failures > 40 {
			s.finish(err)
			return
		}
		select {
		case <-s.stop:
			return
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func (s *screencast) finish(err error) {
	if err == nil {
		err = errors.New("The live view ended")
	}
	select {
	case s.fail <- err:
	default:
	}
}

// attach shows the window's current page until the connection ends, and says
// how many frames it delivered.
func (s *screencast) attach() (int, error) {
	target := s.w.Target()
	dctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	conn, _, err := websocket.DefaultDialer.DialContext(dctx, s.w.pageEndpoint(target), nil)
	cancel()
	if err != nil {
		return 0, err
	}
	conn.SetReadLimit(8 << 20)
	s.mu.Lock()
	select {
	case <-s.stop:
		s.mu.Unlock()
		conn.Close()
		return 0, nil
	default:
	}
	s.conn = conn
	s.acks = map[uint64]int{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if s.conn == conn {
			s.conn = nil
		}
		s.mu.Unlock()
		conn.Close()
	}()
	if err := s.send(conn, 1, "Page.startScreencast", map[string]any{"format": "jpeg", "quality": 65, "maxWidth": 1280, "maxHeight": 800, "everyNthFrame": 1}); err != nil {
		return 0, err
	}
	frames := 0
	for {
		var msg struct {
			Method string `json:"method"`
			Params struct {
				Data      string          `json:"data"`
				Metadata  json.RawMessage `json:"metadata"`
				SessionID int             `json:"sessionId"`
			} `json:"params"`
		}
		if err := conn.ReadJSON(&msg); err != nil {
			return frames, err
		}
		if msg.Method != "Page.screencastFrame" {
			continue
		}
		var meta struct {
			DeviceWidth  float64 `json:"deviceWidth"`
			DeviceHeight float64 `json:"deviceHeight"`
		}
		_ = json.Unmarshal(msg.Params.Metadata, &meta)
		s.mu.Lock()
		s.seq++
		seq := s.seq
		s.acks[seq] = msg.Params.SessionID
		s.mu.Unlock()
		frame, _ := json.Marshal(map[string]any{"type": "frame", "seq": seq, "data": msg.Params.Data, "metadata": json.RawMessage(msg.Params.Metadata)})
		select {
		case s.out <- frame:
			frames++
		case <-s.stop:
			return frames, nil
		}
	}
}

func (s *screencast) send(conn *websocket.Conn, id int, method string, params any) error {
	s.writeM.Lock()
	defer s.writeM.Unlock()
	_ = conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	return conn.WriteJSON(map[string]any{"id": id, "method": method, "params": params})
}

func (s *screencast) Read() ([]byte, error) {
	select {
	case frame := <-s.out:
		return frame, nil
	case err := <-s.fail:
		return nil, err
	case <-s.stop:
		return nil, errors.New("The live view was closed")
	}
}

// Ack tells Chrome the viewer drew frame seq, so it sends the next.
func (s *screencast) Ack(seq uint64) error {
	s.mu.Lock()
	session, ok := s.acks[seq]
	delete(s.acks, seq)
	conn := s.conn
	s.mu.Unlock()
	if !ok || conn == nil {
		return nil // the page changed under the view; its frames start again
	}
	return s.send(conn, int(seq)+1, "Page.screencastFrameAck", map[string]any{"sessionId": session})
}

func (s *screencast) Close() error {
	s.once.Do(func() {
		close(s.stop)
		s.w.mu.Lock()
		delete(s.w.streams, s)
		s.w.mu.Unlock()
		s.mu.Lock()
		if s.conn != nil {
			_ = s.conn.Close()
		}
		s.mu.Unlock()
	})
	return nil
}
