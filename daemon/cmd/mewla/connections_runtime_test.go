package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/connections"
	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/link"
	"github.com/daoleno/mewla/daemon/server"
)

// Opt-in candidate runtime: production auth, native WebSocket and local CLI
// dispatch with disposable state. No Brain bootstrap, scheduler, host discovery,
// current daemon replacement, or writes to external services.
func TestPluginsOwnedRuntime(t *testing.T) {
	root := os.Getenv("MEWLA_PLUGINS_RUNTIME_DIR")
	if root == "" {
		t.Skip("owned runtime only")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	port := 19881
	if value := os.Getenv("MEWLA_PLUGINS_RUNTIME_PORT"); value != "" {
		var parseErr error
		port, parseErr = strconv.Atoi(value)
		if parseErr != nil || port < 1024 || port > 65535 {
			t.Fatal("invalid owned runtime port")
		}
	}
	m, err := connections.New(root)
	if err != nil {
		t.Fatal(err)
	}
	// Real loopback API for exercising an explicitly trusted custom account
	// through the production dialer, control socket and native client.
	localAPI := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/items/42" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 42, "source": "owned local adapter", "read_only": true})
	}))
	localAPI.Listener.Close()
	address := "127.0.0.1:0"
	if previous, err := os.ReadFile(filepath.Join(root, "local-api-endpoint")); err == nil {
		if u, err := url.Parse(string(previous)); err == nil && u.Hostname() == "127.0.0.1" {
			address = u.Host
		}
	}
	localAPI.Listener, err = net.Listen("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	localAPI.Start()
	defer localAPI.Close()
	browser := newPluginsBrowserFixture(t, m, root)
	defer browser.Close()
	if err := os.WriteFile(filepath.Join(root, "local-api-endpoint"), []byte(localAPI.URL), 0600); err != nil {
		t.Fatal(err)
	}
	a, err := auth.NewManager(root)
	if err != nil {
		t.Fatal(err)
	}
	s := server.New(a, nil, nil, nil, nil, nil, nil)
	s.SetConnections(m)
	stop := make(chan struct{}, 1)
	mux := http.NewServeMux()
	mux.Handle("/", s.Handler())
	mux.HandleFunc("/fixture/stop", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		select {
		case stop <- struct{}{}:
		default:
		}
	})
	h := httptest.NewUnstartedServer(mux)
	h.Listener.Close()
	h.Listener, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	h.StartTLS()
	defer h.CloseClientConnections()
	defer h.Close()
	token, err := a.IssuePairingToken(20 * time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var route [16]byte
	_, _ = rand.Read(route[:])
	pin := sha256.Sum256(h.Certificate().RawSubjectPublicKeyInfo)
	payload := link.PairingPayload{Version: link.PairingVersion, DaemonID: a.DaemonID(), DaemonPublicKey: a.PublicKeyHex(), EnrollmentToken: token.Value, RouteID: hex.EncodeToString(route[:]), TransportPin: hex.EncodeToString(pin[:]), Candidates: []link.PairingCandidate{{Name: "Plugins candidate", AdmissionURL: fmt.Sprintf("https://10.0.2.2:%d", port), StableURL: fmt.Sprintf("https://10.0.2.2:%d", port)}}, ExpiresAtMS: time.Now().Add(20 * time.Minute).UnixMilli()}
	payload.Signature = a.CreateLinkPairingSignature(link.PairingBindingPayload(payload))
	raw, _ := json.Marshal(payload)
	values := url.Values{}
	values.Set("v", fmt.Sprint(link.PairingVersion))
	values.Set("p", base64.RawURLEncoding.EncodeToString(raw))
	if err = os.WriteFile(filepath.Join(root, "pairing-link"), []byte("zen://settings?"+values.Encode()), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &control.Server{Path: filepath.Join(root, "run", control.SocketName), Handler: &controlApp{connections: m}}
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	t.Logf("Plugins candidate ready on isolated local control socket and TLS %d; pairing link remains private", port)
	select {
	case <-stop:
	case <-time.After(20 * time.Minute):
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestPluginsControlSocketPersistence(t *testing.T) {
	root := t.TempDir()
	m, err := connections.New(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &control.Server{Path: filepath.Join(root, "run", control.SocketName), Handler: &controlApp{connections: m}}
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	defer func() { cancel(); <-done }()
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(c.Path); err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	call := func(q connections.Request) control.Response {
		t.Helper()
		r, err := control.Call(c.Path, control.Request{Type: "connections", ConnectionRequest: &q})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	if r := call(connections.Request{Action: "list"}); !r.OK || len(r.Connections.Catalog) != 7 {
		t.Fatal("catalog unavailable")
	}
	r := call(connections.Request{Action: "add", Input: &connections.Input{Integration: "openapi", Name: "local fixture", Endpoint: "https://example.com", Spec: json.RawMessage(`{"openapi":"3.0.3","info":{"title":"test","version":"1"},"paths":{"/read":{"get":{"operationId":"read","responses":{"200":{"description":"ok"}}}}}}`)}})
	if !r.OK {
		t.Fatal(r.Error)
	}
	id := r.Connections.Account.ID
	if r := call(connections.Request{Action: "search", Query: "read"}); !r.OK || len(r.Connections.Matches) != 1 {
		t.Fatal("discovery unavailable")
	}
	if r := call(connections.Request{Action: "describe", ID: id, Tool: "read"}); !r.OK || len(r.Connections.Tool.InputSchema) == 0 {
		t.Fatal("description unavailable")
	}
	if r := call(connections.Request{Action: "invoke", ID: id, Tool: "read"}); r.OK {
		t.Fatal("ungranted tool executed")
	}
	reopened, err := connections.New(root)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := reopened.Handle(context.Background(), connections.Request{Action: "get", ID: id})
	if err != nil || snapshot.Account.Name != "local fixture" {
		t.Fatal("restart lost account")
	}
}
