package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/addressbook"
	"github.com/daoleno/mewla/daemon/remote"
)

func TestRemoteLoginSendsNoHostnameAndLoginNameRenames(t *testing.T) {
	machine := func(label string) remote.Machine {
		return remote.Machine{
			MachineID: "mach_1", AccountLogin: "octo", Hostname: label + ".mewla.test", Label: label,
			RouteID: strings.Repeat("ab", 16), Grant: "grant", GrantExpiresAtMS: time.Now().Add(time.Hour).UnixMilli(),
			Relays: []remote.Relay{{Name: "r1", ControlAddress: "127.0.0.1:1", ControlServerName: "relay.test", ClientDomain: "mewla.test", ClientPort: 443}},
		}
	}
	var mu sync.Mutex
	var startNames, renames []string
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		defer mu.Unlock()
		switch r.Method + " " + r.URL.Path {
		case "POST /v1/login/start":
			startNames = append(startNames, body["machine_name"])
			_ = json.NewEncoder(w).Encode(map[string]any{"login_id": "l1", "user_code": "ABCD-EFGH", "verification_uri": "https://example.test/device", "interval_seconds": 1})
		case "POST /v1/login/poll":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "complete", "machine": machine("quiet-otter-1a2b")})
		case "PUT /v1/machine/name":
			renames = append(renames, body["name"])
			_ = json.NewEncoder(w).Encode(machine("studio-c0ffee"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer service.Close()

	stateDir := t.TempDir()
	var out bytes.Buffer
	config, err := remoteLogin(context.Background(), &out, stateDir, remoteLoginOptions{serviceURL: service.URL})
	if err != nil {
		t.Fatalf("login: %v\n%s", err, out.String())
	}
	if len(startNames) != 1 || startNames[0] != "" {
		t.Fatalf("login sent machine names %q; want one empty name so the service picks random words", startNames)
	}
	book, err := addressbook.New(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := book.Add(config.URL(), addressbook.SourceRemote); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	if err := runLoginCommand([]string{"-state-dir", stateDir, "-name", "Studio", "-no-browser"}, &out); err != nil {
		t.Fatalf("rename: %v\n%s", err, out.String())
	}
	if len(renames) != 1 || renames[0] != "Studio" || len(startNames) != 1 {
		t.Fatalf("renames=%q starts=%q; want one rename and no second sign-in", renames, startNames)
	}
	saved, ok, err := remote.LoadConfig(stateDir)
	if err != nil || !ok || saved.Machine.Label != "studio-c0ffee" || saved.ServiceURL != service.URL {
		t.Fatalf("saved config = %#v ok=%v err=%v", saved, ok, err)
	}
	book, err = addressbook.New(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if book.Contains(config.URL()) || !book.Contains(saved.URL()) {
		t.Fatalf("address book still lists %s or misses %s", config.URL(), saved.URL())
	}
	if !strings.Contains(out.String(), saved.URL()) || !strings.Contains(out.String(), "browsers pair again") {
		t.Fatalf("rename output:\n%s", out.String())
	}
}
