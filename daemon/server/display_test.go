package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/vdisplay"
	"github.com/gorilla/websocket"
)

func TestDisplayListStopAndViewer(t *testing.T) {
	if err := vdisplay.Capability(); err != nil {
		t.Skip(err)
	}
	a, priv, device := sessionFileAuthFixture(t)
	service := vdisplay.NewService("")
	defer service.Close()
	s := New(a, nil, nil, nil, nil, nil, nil)
	s.SetDisplays(service)
	h := httptest.NewServer(s.Handler())
	defer h.Close()
	defer s.shutdownAuthenticatedClients()
	auth := func(purpose string) string { return calendarAuthHeader(priv, a.DaemonID(), device, purpose) }

	get, _ := http.NewRequest("GET", h.URL+"/display", nil)
	if resp, err := http.DefaultClient.Do(get); err != nil || resp.StatusCode == 200 {
		t.Fatalf("an unauthenticated list was served: %v", err)
	}
	if _, err := service.Act(context.Background(), "w1", vdisplay.Action{Action: "open", Command: []string{"xmessage", "-center", "hello"}}); err != nil {
		t.Fatal(err)
	}
	get, _ = http.NewRequest("GET", h.URL+"/display", nil)
	get.Header.Set("Authorization", auth("mewla-display"))
	resp, err := http.DefaultClient.Do(get)
	if err != nil {
		t.Fatal(err)
	}
	var listed struct {
		Displays  []vdisplay.Info `json:"displays"`
		Available bool            `json:"available"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&listed)
	resp.Body.Close()
	if len(listed.Displays) != 1 || listed.Displays[0].ID != "w1" || !listed.Available {
		t.Fatalf("listed = %+v", listed)
	}

	// A viewer's click reaches the display and is told to the agent as a count.
	header := http.Header{"Authorization": []string{auth("mewla-display-view")}}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(h.URL, "http")+"/display/viewer?worker=w1", header)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	readBinary := func() []byte {
		for {
			kind, data, err := conn.ReadMessage()
			if err != nil {
				t.Fatal(err)
			}
			if kind == websocket.BinaryMessage {
				return data
			}
		}
	}
	if v := readBinary(); !bytes.HasPrefix(v, []byte("RFB 003.008")) {
		t.Fatalf("server hello = %q", v)
	}
	_ = conn.WriteMessage(websocket.BinaryMessage, []byte("RFB 003.008\n"))
	readBinary() // security types
	_ = conn.WriteMessage(websocket.BinaryMessage, []byte{1})
	readBinary() // security result
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), `"ready"`) {
			break
		}
	}
	_ = conn.WriteMessage(websocket.BinaryMessage, []byte{1})                        // ClientInit
	_ = conn.WriteMessage(websocket.BinaryMessage, []byte{5, 1, 0, 40, 0, 40})       // click
	_ = conn.WriteMessage(websocket.BinaryMessage, []byte{4, 1, 0, 0, 0, 0, 0, 'q'}) // one key
	_ = conn.WriteMessage(websocket.BinaryMessage, []byte{5, 0, 0, 40, 0, 40})       // release
	deadline := time.Now().Add(5 * time.Second)
	for {
		r, err := service.Act(context.Background(), "w1", vdisplay.Action{Action: "screenshot"})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(r.Text, "1 click") && strings.Contains(r.Text, "1 character typed (not shown to you)") {
			if strings.Contains(r.Text, "q") && strings.Contains(r.Text, "typed q") {
				t.Fatal("content leaked")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the agent was never told: %q", r.Text)
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Stop from the HTTP side holds the agent off.
	post := func(body string) int {
		req, _ := http.NewRequest("POST", h.URL+"/display", strings.NewReader(body))
		req.Header.Set("Authorization", auth("mewla-display"))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return resp.StatusCode
	}
	if status := post(`{"action":"delete","worker":"w1"}`); status != 400 {
		t.Fatalf("an unknown action was accepted: %d", status)
	}
	if status := post(`{"action":"halt","worker":"w1"}`); status != 200 {
		t.Fatalf("halt = %d", status)
	}
	if _, err := service.Act(context.Background(), "w1", vdisplay.Action{Action: "screenshot"}); err == nil || !strings.Contains(err.Error(), "stopped you") {
		t.Fatalf("err = %v", err)
	}
}
