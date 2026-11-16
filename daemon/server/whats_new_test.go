package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/whatsnew"
	"github.com/gorilla/websocket"
)

// The app reads the record with every release's notes (an app older than its
// computer draws the computer's), adds what the person saw, and tells its own
// update; each answer is the record as it is then.
func TestWhatsNewAnswersTheRecordAndKeepsWhatWasSeen(t *testing.T) {
	store, err := whatsnew.Open(t.TempDir(), "0.4.9", true, time.UnixMilli(1_700_000_000_000))
	if err != nil {
		t.Fatal(err)
	}
	s := New(nil, nil, nil, nil, nil, nil, nil)
	s.SetWhatsNew(store)
	frames := []string{
		`{"type":"whats_new_get","request_id":"g"}`,
		`{"type":"whats_new_seen","request_id":"s","ids":["0.4.9-a","0.4.9-b"]}`,
		`{"type":"whats_new_note","request_id":"n","version":"0.4.10"}`,
	}
	upgrader := websocket.Upgrader{}
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		defer attachTestOutbox(s, conn)()
		for _, frame := range frames {
			s.handleClientMessage(conn, []byte(frame))
		}
	}))
	defer host.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(host.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	type answer struct {
		Type      string          `json:"type"`
		RequestID string          `json:"request_id"`
		Record    whatsnew.Record `json:"record"`
		Releases  json.RawMessage `json:"releases"`
	}
	read := func() answer {
		t.Helper()
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		var a answer
		if err := conn.ReadJSON(&a); err != nil {
			t.Fatal(err)
		}
		if a.Type != "whats_new" {
			t.Fatalf("answer %+v", a)
		}
		return a
	}
	got := read()
	if got.RequestID != "g" || got.Record.Baseline != "" || len(got.Releases) == 0 {
		t.Fatalf("get answered %+v", got)
	}
	got = read()
	if got.RequestID != "s" || !reflect.DeepEqual(got.Record.Seen, []string{"0.4.9-a", "0.4.9-b"}) || got.Releases != nil {
		t.Fatalf("seen answered %+v", got)
	}
	got = read()
	if got.RequestID != "n" || len(got.Record.Updates) != 2 || got.Record.Updates[1].Version != "0.4.10" {
		t.Fatalf("note answered %+v", got)
	}
}
