package watcher

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func tunnelFixture(t *testing.T) (*Watcher, SessionService, *httptest.Server, *atomic.Bool) {
	t.Helper()
	origin := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/ws" {
			connection, err := (&websocket.Upgrader{}).Upgrade(writer, request, nil)
			if err != nil {
				return
			}
			defer connection.Close()
			kind, data, err := connection.ReadMessage()
			if err == nil {
				_ = connection.WriteMessage(kind, data)
			}
			return
		}
		writer.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(writer, "MEWLA_TUNNEL_FIXTURE")
	}))
	t.Cleanup(origin.Close)
	port := origin.Listener.Addr().(*net.TCPAddr).Port
	live := &atomic.Bool{}
	live.Store(true)
	service := SessionService{ID: "fixture", PID: os.Getpid(), Port: port, Binds: []string{"127.0.0.1"}, Generation: ""}
	watcher := &Watcher{listSocketsFn: func() ([]listeningSocket, error) {
		if !live.Load() {
			return nil, nil
		}
		return []listeningSocket{{pid: service.PID, port: port, bind: "127.0.0.1", inode: "fixture-1"}}, nil
	}}
	sockets, _ := watcher.listeningSocketsForServices()
	service.Generation = serviceInstanceGeneration(service, sockets)
	return watcher, service, origin, live
}

func startFixtureTunnel(t *testing.T, w *Watcher, service SessionService) (*quickTunnel, *serviceTunnels) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	tunnel := &quickTunnel{state: ServiceTunnel{Status: "starting", Generation: service.Generation}, cancel: cancel, done: make(chan struct{})}
	owner := w.tunnelOwner()
	owner.items[service.ID] = tunnel
	go w.runServiceTunnel(ctx, owner, tunnel, service)
	t.Cleanup(func() {
		cancel()
		select {
		case <-tunnel.done:
		case <-time.After(6 * time.Second):
			t.Error("tunnel cleanup timed out")
		}
	})
	return tunnel, owner
}
func waitTunnel(t *testing.T, tunnel *quickTunnel, owner *serviceTunnels) ServiceTunnel {
	t.Helper()
	deadline := time.Now().Add(70 * time.Second)
	for time.Now().Before(deadline) {
		owner.mu.Lock()
		state := tunnel.state
		owner.mu.Unlock()
		if state.Status == "running" || state.Status == "failed" || state.Status == "stopped" {
			return state
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal("tunnel did not settle")
	return ServiceTunnel{}
}

func TestQuickTunnelOriginLifetimeAndStopDoNotKillOrigin(t *testing.T) {
	directory := t.TempDir()
	binary := filepath.Join(directory, "cloudflared")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nMEWLA_FAKE_CLOUDFLARED=1 exec "+shellQuote(executable)+" -test.run='^TestFakeCloudflared$' -- \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	proxyFile := filepath.Join(directory, "proxy-url")
	t.Setenv("MEWLA_TUNNEL_TEST_PROXY", proxyFile)
	w, service, origin, live := tunnelFixture(t)
	w.tunnelResolveHost = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
	}
	tunnel, owner := startFixtureTunnel(t, w, service)
	if state := waitTunnel(t, tunnel, owner); state.Status != "running" {
		t.Fatalf("state=%+v", state)
	}
	proxyBytes, err := os.ReadFile(proxyFile)
	if err != nil {
		t.Fatal(err)
	}
	proxyURL := string(proxyBytes)
	proxyResponse, err := http.Get(proxyURL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(proxyResponse.Body)
	proxyResponse.Body.Close()
	if string(body) != "MEWLA_TUNNEL_FIXTURE" {
		t.Fatalf("proxy HTTP body=%q", body)
	}
	connection, _, err := (&websocket.Dialer{HandshakeTimeout: 3 * time.Second}).Dial("ws"+strings.TrimPrefix(proxyURL, "http")+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	connection.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := connection.WriteMessage(websocket.TextMessage, []byte("LOCAL_WS")); err != nil {
		t.Fatal(err)
	}
	_, echo, err := connection.ReadMessage()
	connection.Close()
	if err != nil || string(echo) != "LOCAL_WS" {
		t.Fatalf("proxy WebSocket=%q %v", echo, err)
	}
	live.Store(false)
	select {
	case <-tunnel.done:
	case <-time.After(4 * time.Second):
		t.Fatal("origin loss did not stop tunnel")
	}
	owner.mu.Lock()
	state := tunnel.state
	owner.mu.Unlock()
	if state.URL != "" || state.Status != "failed" {
		t.Fatalf("stale URL: %+v", state)
	}
	response, err := http.Get(origin.URL)
	if err != nil {
		t.Fatal("stopping tunnel killed origin", err)
	}
	response.Body.Close()
}

// TestFakeCloudflared is the test binary standing in for cloudflared: it
// records the --url it was given and serves the two metrics endpoints the
// daemon reads. It does nothing unless re-executed by the fake `cloudflared`
// script.
func TestFakeCloudflared(t *testing.T) {
	if os.Getenv("MEWLA_FAKE_CLOUDFLARED") != "1" {
		t.Skip("runs only as the fake cloudflared")
	}
	var metrics, origin string
	args := os.Args
	for index, arg := range args {
		switch arg {
		case "--metrics":
			metrics = args[index+1]
		case "--url":
			origin = args[index+1]
		}
	}
	if err := os.WriteFile(os.Getenv("MEWLA_TUNNEL_TEST_PROXY"), []byte(origin), 0600); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/quicktunnel", func(writer http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(writer, `{"hostname":"fixture-test.trycloudflare.com"}`)
	})
	mux.HandleFunc("/ready", func(writer http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(writer, `{"status":200,"readyConnections":1,"connectorId":"fixture"}`)
	})
	listener, err := net.Listen("tcp", metrics)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = http.Serve(listener, mux) }()
	time.Sleep(60 * time.Second)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func TestCloudflaredMetricsReadsHostnameAndReadiness(t *testing.T) {
	var readyBody, hostnameBody string
	var readyStatus int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/quicktunnel":
			fmt.Fprint(writer, hostnameBody)
		case "/ready":
			writer.WriteHeader(readyStatus)
			fmt.Fprint(writer, readyBody)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	metrics := newCloudflaredMetrics(strings.TrimPrefix(server.URL, "http://"))
	ctx := context.Background()

	for body, want := range map[string]string{
		`{"hostname":"hello-world.trycloudflare.com"}`: "https://hello-world.trycloudflare.com",
		`{"hostname":""}`: "",
		`{}`:              "",
	} {
		hostnameBody = body
		if got, err := metrics.publicURL(ctx); err != nil || got != want {
			t.Errorf("publicURL(%s) = %q, %v; want %q", body, got, err, want)
		}
	}
	for _, body := range []string{`{"hostname":"example.com"}`, `{"hostname":"evil.com/x.trycloudflare.com"}`, `not json`} {
		hostnameBody = body
		if got, err := metrics.publicURL(ctx); err == nil || got != "" {
			t.Errorf("publicURL(%s) = %q, %v; want an error", body, got, err)
		}
	}

	for _, tc := range []struct {
		status  int
		body    string
		want    bool
		wantErr bool
	}{
		{http.StatusServiceUnavailable, `{"status":503,"readyConnections":0}`, false, false},
		{http.StatusOK, `{"status":200,"readyConnections":2}`, true, false},
		{http.StatusOK, `{"status":200,"readyConnections":0}`, false, false},
		{http.StatusInternalServerError, ``, false, true},
		{http.StatusOK, `not json`, false, true},
	} {
		readyStatus, readyBody = tc.status, tc.body
		if got, err := metrics.ready(ctx); got != tc.want || (err != nil) != tc.wantErr {
			t.Errorf("ready(%d %s) = %v, %v", tc.status, tc.body, got, err)
		}
	}
}

func TestCloudflaredMetricsAddressIsFreeLoopback(t *testing.T) {
	address, err := reserveLoopbackAddress()
	if err != nil {
		t.Fatal(err)
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil || host != "127.0.0.1" || port == "0" {
		t.Fatalf("address = %q (%v)", address, err)
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("reserved address is not free: %v", err)
	}
	listener.Close()
}

func TestControlServicesAreNotPublishable(t *testing.T) {
	if !privateService(SessionService{Process: "mewla serve"}) || !privateService(SessionService{Command: "mewla dsh-session"}) {
		t.Fatal("control service publishable")
	}
}

func TestQuickTunnelLiveHTTPAndWebSocket(t *testing.T) {
	if os.Getenv("MEWLA_QUICK_TUNNEL_SMOKE") != "1" {
		t.Skip("explicit public-fixture smoke only")
	}
	w, service, origin, _ := tunnelFixture(t)
	tunnel, owner := startFixtureTunnel(t, w, service)
	state := waitTunnel(t, tunnel, owner)
	if state.Status != "running" {
		t.Fatalf("Cloudflare start: %+v", state)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	dialer := (&net.Dialer{Timeout: 5 * time.Second}).DialContext
	if os.Getenv("MEWLA_TUNNEL_SMOKE_DOH") == "1" {
		// Test-only DNS isolation: preserve the public hostname for TLS and HTTP.
		publicHost, _ := url.Parse(state.URL)
		address := ""
		dnsStatus := -1
		// Quick Tunnel DNS publication is asynchronous. Avoid treating the
		// first negative DNS response as a permanent publication failure.
		for deadline := time.Now().Add(60 * time.Second); time.Now().Before(deadline); {
			request, _ := http.NewRequest(http.MethodGet, "https://1.1.1.1/dns-query?type=A&name="+url.QueryEscape(publicHost.Hostname()), nil)
			request.Header.Set("Accept", "application/dns-json")
			response, err := client.Do(request)
			if err == nil {
				var answer struct {
					Status int
					Answer []struct {
						Type int
						Data string
					}
				}
				decodeErr := json.NewDecoder(response.Body).Decode(&answer)
				response.Body.Close()
				dnsStatus = answer.Status
				if decodeErr == nil {
					for _, record := range answer.Answer {
						if record.Type == 1 && net.ParseIP(record.Data) != nil {
							address = record.Data
							break
						}
					}
				}
			}
			if address != "" {
				break
			}
			time.Sleep(2 * time.Second)
		}
		if address == "" {
			t.Fatalf("Cloudflare DoH has no public A record after bounded publication wait: status=%d", dnsStatus)
		}

		dialer = func(ctx context.Context, network, target string) (net.Conn, error) {
			_, port, err := net.SplitHostPort(target)
			if err != nil {
				return nil, err
			}
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(address, port))
		}
		client.Transport = &http.Transport{DialContext: dialer}
		t.Log("Public hostname resolved through Cloudflare DoH; system DNS proof remains unavailable")
	}
	var body []byte
	var lastErr error
	for attempt := 0; attempt < 20; attempt++ {
		response, err := client.Get(state.URL)
		lastErr = err
		if err == nil {
			body, _ = io.ReadAll(io.LimitReader(response.Body, 1024))
			response.Body.Close()
			if string(body) == "MEWLA_TUNNEL_FIXTURE" {
				break
			}
		}
		time.Sleep(time.Second)
	}
	if string(body) != "MEWLA_TUNNEL_FIXTURE" {
		t.Fatalf("public HTTP failed: %v body=%q", lastErr, body)
	}
	connection, _, err := (&websocket.Dialer{HandshakeTimeout: 10 * time.Second, NetDialContext: dialer}).Dial("wss"+strings.TrimPrefix(state.URL, "https")+"/ws", nil)
	if err != nil {
		t.Fatal("public WebSocket", err)
	}
	defer connection.Close()
	connection.SetReadDeadline(time.Now().Add(5 * time.Second))
	if err := connection.WriteMessage(websocket.TextMessage, []byte("MEWLA_WS")); err != nil {
		t.Fatal(err)
	}
	_, echo, err := connection.ReadMessage()
	if err != nil || string(echo) != "MEWLA_WS" {
		t.Fatalf("WebSocket echo=%q err=%v", echo, err)
	}
	tunnel.cancel()
	select {
	case <-tunnel.done:
	case <-time.After(5 * time.Second):
		t.Fatal("stop timed out")
	}
	response, err := client.Get(origin.URL)
	if err != nil {
		t.Fatal("origin stopped with tunnel", err)
	}
	response.Body.Close()
	t.Log("Controlled public HTTP and WebSocket passed; tunnel stopped; origin remained alive. Quick Tunnels do not support SSE.")
}

func TestQuickTunnelGenerationChangesOnSamePIDPortReuse(t *testing.T) {
	service := SessionService{PID: os.Getpid(), Port: 12345}
	old := serviceInstanceGeneration(service, []listeningSocket{{pid: service.PID, port: service.Port, bind: "127.0.0.1", inode: "1"}})
	replacement := serviceInstanceGeneration(service, []listeningSocket{{pid: service.PID, port: service.Port, bind: "127.0.0.1", inode: "2"}})
	if old == "" || old == replacement {
		t.Fatal("listener replacement retained tunnel ownership")
	}
	if value := serviceInstanceGeneration(service, []listeningSocket{{pid: service.PID, port: service.Port, bind: "127.0.0.1"}}); value != "" {
		t.Fatal("missing listener identity accepted")
	}
}

func TestQuickTunnelStoppingFinishedOperationRemainsStopped(t *testing.T) {
	w := &Watcher{}
	done := make(chan struct{})
	close(done)
	owner := w.tunnelOwner()
	owner.items["service"] = &quickTunnel{state: ServiceTunnel{Status: "failed", Generation: "old", Error: "ended"}, cancel: func() {}, done: done}
	state, err := w.ServiceTunnelAction("service", "old", "stop")
	if err != nil || state.Status != "stopped" || state.URL != "" {
		t.Fatalf("stop=%+v %v", state, err)
	}
}

func TestQuickTunnelParentDeath(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux parent-death lifecycle")
	}
	if os.Getenv("MEWLA_TUNNEL_PARENT_TEST") == "1" {
		child := exec.Command("/bin/sleep", "60")
		if err := bindTunnelToDaemon(child); err != nil {
			t.Fatal(err)
		}
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		fmt.Println(child.Process.Pid)
		select {}
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	parent := exec.Command(executable, "-test.run=^TestQuickTunnelParentDeath$", "-test.timeout=15s")
	parent.Env = append(os.Environ(), "MEWLA_TUNNEL_PARENT_TEST=1")
	output, err := parent.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := parent.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = parent.Process.Kill(); _ = parent.Wait() }()
	scanner := bufio.NewScanner(output)
	if !scanner.Scan() {
		t.Fatal("missing child identity")
	}
	pid, err := strconv.Atoi(scanner.Text())
	if err != nil {
		t.Fatal(err)
	}
	childProcess, err := os.FindProcess(pid)
	if err != nil {
		t.Fatal(err)
	}
	defer childProcess.Kill()
	if err := parent.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = parent.Wait()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if os.IsNotExist(err) {
			return
		}
		fields := strings.Fields(string(stat)[strings.LastIndex(string(stat), ")")+1:])
		if len(fields) > 0 && fields[0] == "Z" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("owned tunnel child survived its parent")
}
