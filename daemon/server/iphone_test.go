package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/agentuse"
)

func TestIPhoneSwitchStopAndDoneAreSignedOverTheirBody(t *testing.T) {
	a, priv, device := sessionFileAuthFixture(t)
	use, err := agentuse.New(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	stops, dones := 0, 0
	use.SetIPhone(agentuse.IPhoneHooks{
		Status:      func() agentuse.IPhoneStatus { return agentuse.IPhoneStatus{State: agentuse.IPhoneReady, Summary: "ready"} },
		Stop:        func() { stops++ },
		HandoffDone: func() bool { dones++; return true },
	})
	s := New(a, nil, nil, nil, nil, nil, nil)
	s.SetAgentUse(use)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	request := func(method, purpose, body string) (int, agentuse.IPhoneStatus) {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+"/iphone", strings.NewReader(body))
		req.Header.Set("Authorization", calendarAuthHeader(priv, a.DaemonID(), device, purpose))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var row agentuse.IPhoneStatus
		_ = json.NewDecoder(resp.Body).Decode(&row)
		return resp.StatusCode, row
	}
	post := func(body string) (int, agentuse.IPhoneStatus) {
		sum := sha256.Sum256([]byte(body))
		return request(http.MethodPost, "mewla-iphone:POST:/iphone:sha256="+hex.EncodeToString(sum[:]), body)
	}
	if code, row := request(http.MethodGet, "mewla-iphone:GET:/iphone", ""); code != http.StatusOK || row.Enabled || row.State != agentuse.IPhoneOff {
		t.Fatalf("GET: %d %+v", code, row)
	}
	if code, _ := request(http.MethodGet, "mewla-phone:GET:/phone", ""); code != http.StatusUnauthorized {
		t.Fatalf("another purpose: %d", code)
	}
	sum := sha256.Sum256([]byte(`{"enabled":false}`))
	if code, _ := request(http.MethodPost, "mewla-iphone:POST:/iphone:sha256="+hex.EncodeToString(sum[:]), `{"enabled":true}`); code != http.StatusUnauthorized {
		t.Fatalf("body swapped under a signature: %d", code)
	}
	if code, row := post(`{"enabled":true}`); code != http.StatusOK || !row.Enabled || row.State != agentuse.IPhoneReady || !use.IPhoneReady() {
		t.Fatalf("on: %d %+v", code, row)
	}
	if code, _ := post(`{"handoff_done":true}`); code != http.StatusOK || dones != 1 {
		t.Fatalf("done: %d %d", code, dones)
	}
	if code, _ := post(`{"stop_agent":true}`); code != http.StatusOK || stops != 1 {
		t.Fatalf("stop: %d %d", code, stops)
	}
	for _, bad := range []string{`{}`, `{"enabled":true,"stop_agent":true}`, `{"other":1}`, `nope`} {
		if code, _ := post(bad); code != http.StatusBadRequest {
			t.Fatalf("%s: %d", bad, code)
		}
	}
	if code, row := post(`{"enabled":false}`); code != http.StatusOK || row.Enabled || stops != 2 {
		t.Fatalf("off: %d %+v stops %d", code, row, stops)
	}
}
