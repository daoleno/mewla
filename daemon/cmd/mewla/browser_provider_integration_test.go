//go:build linux && browserfixture

package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/browser"
	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/modelprofiles"
	mewlaserver "github.com/daoleno/mewla/daemon/server"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
	"github.com/gorilla/websocket"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The normal launch compiler points at os.Executable. In this opt-in test the
// executable is the test binary, which dispatches the actual product MCP CLI.
func TestMain(m *testing.M) {
	if os.Getenv("MEWLA_BROWSER_PROVIDER") != "" && len(os.Args) > 1 && os.Args[1] == "browser" {
		if err := runBrowserCommand(os.Args[2:], os.Stderr); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	if os.Getenv("MEWLA_BROWSER_PROVIDER") == "probe" && len(os.Args) > 1 && os.Args[1] == "browser-test-provider" {
		// Deterministic protocol client for debugging the real process/launch
		// boundary without consuming another model request.
		var config struct {
			Servers map[string]struct {
				Command string   `json:"command"`
				Args    []string `json:"args"`
			} `json:"mcpServers"`
		}
		for i, arg := range os.Args {
			if arg == "--mcp-config" && i+1 < len(os.Args) {
				_ = json.Unmarshal([]byte(os.Args[i+1]), &config)
			}
		}
		server := config.Servers["mewla_browser"]
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		client := mcp.NewClient(&mcp.Implementation{Name: "launch-probe", Version: "1"}, nil)
		session, err := client.Connect(ctx, &mcp.CommandTransport{Command: exec.Command(server.Command, server.Args...)}, nil)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		for _, action := range []string{"control", "snapshot", "release"} {
			result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "browser", Arguments: map[string]any{"action": action}})
			if err != nil || result.IsError {
				raw, _ := json.Marshal(result)
				fmt.Fprintf(os.Stderr, "probe task=%s action=%s error=%v result=%s\n", os.Getenv("MEWLA_WORKER_ID"), action, err, raw)
				session.Close()
				os.Exit(1)
			}
		}
		session.Close()
		fmt.Println(`{"result":"BROWSER_SHARED_SESSION_OK"}`)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type browserObservedBroker struct {
	app       *controlApp
	mu        sync.Mutex
	snapshots map[string]string
}

func (b *browserObservedBroker) HandleControlRequest(q control.Request) control.Response {
	r := b.app.HandleControlRequest(q)
	if q.Type == "browser" && q.BrowserRequest != nil && q.BrowserRequest.Command != nil && q.BrowserRequest.Command.Kind == "snapshot" && r.OK && r.Browser != nil {
		b.mu.Lock()
		raw, _ := json.Marshal(r.Browser)
		b.snapshots[q.WorkerID] = string(raw)
		b.mu.Unlock()
	}
	return r
}

// Exactly two short provider prompts, only when explicitly opted in. Uses the
// authenticated create_session API, real tmux/watcher's liveness, and ordinary
// invocation-scoped provider MCP setup; no fixture task identity.
func browserNormalProviderProof(t *testing.T, manager *browser.Manager, id, target, root string, a *auth.Manager, signature func(string) string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	socket := filepath.Join(root, "provider-tmux.sock")
	scratch := filepath.Join(root, "provider-scratch")
	if err := os.MkdirAll(scratch, 0700); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("MEWLA_BROWSER_PROVIDER") == "probe" {
		bin := filepath.Join(root, "probe-bin")
		if err := os.MkdirAll(bin, 0700); err != nil {
			t.Fatal(err)
		}
		exe, _ := os.Executable()
		if err := os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\nexec '"+strings.ReplaceAll(exe, "'", "'\\''")+"' browser-test-provider \"$@\"\n"), 0700); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	}
	realTmux, err := exec.LookPath("tmux")
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(realTmux, "-S", socket, "-f", "/dev/null", "new-session", "-d", "-s", "browser-qa-seed").CombinedOutput(); err != nil {
		t.Fatalf("private tmux: %v %s", err, out)
	}
	defer exec.Command(realTmux, "-S", socket, "kill-server").Run()
	if err := exec.Command(realTmux, "-S", socket, "set-option", "-g", "remain-on-exit", "on").Run(); err != nil {
		t.Fatal(err)
	}
	w := watcher.New(100 * time.Millisecond)
	w.SetTmuxServer(socket, scratch)
	w.SetActivityProbe(classifier.DefaultActivityProbe())
	w.SetProviderActivityProbe(newWorkProviderActivityProbe())
	watchDone := make(chan struct{})
	go func() { defer close(watchDone); _ = w.Run(ctx) }()
	defer func() { cancel(); <-watchDone }()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-w.Events():
			}
		}
	}()
	app := &controlApp{browsers: manager, watcher: w, stateDir: a.StorageDir()}
	observed := &browserObservedBroker{app: app, snapshots: map[string]string{}}
	cs := &control.Server{Path: filepath.Join(a.StorageDir(), "run", "control.sock"), Handler: observed}
	controlDone := make(chan error, 1)
	go func() { controlDone <- cs.Run(ctx) }()
	defer func() { cancel(); <-controlDone }()
	for n := 0; n < 100; n++ {
		if _, err := os.Stat(cs.Path); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	s := mewlaserver.New(a, w, nil, nil, nil, nil, nil)
	s.SetBrowser(manager)
	// Reuse the normal configured Provider selection through an isolated Owner.
	// Its catalog/credentials are private copies; route/gateway state is fresh.
	if os.Getenv("MEWLA_BROWSER_PROVIDER") != "probe" {
		profilesPath, err := work.DefaultModelProfilesPath()
		if err != nil {
			t.Fatal(err)
		}
		credentialsPath, err := work.DefaultProviderCredentialsPath()
		if err != nil {
			t.Fatal(err)
		}
		configHashes := map[string]string{}
		for _, item := range []struct{ source, name string }{{profilesPath, "profiles.toml"}, {credentialsPath, "credentials.json"}} {
			data, err := os.ReadFile(item.source)
			if err != nil {
				t.Fatal(err)
			}
			configHashes[item.source] = fmt.Sprintf("%x", sha256.Sum256(data))
			if err := os.WriteFile(filepath.Join(root, item.name), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
		credentials, err := modelprofiles.NewFileCredentialStore(filepath.Join(root, "credentials.json"))
		if err != nil {
			t.Fatal(err)
		}
		owner, err := modelprofiles.StartOwner(modelprofiles.OwnerConfig{ProfilesPath: filepath.Join(root, "profiles.toml"), RoutesPath: filepath.Join(root, "provider-routes.json"), ListenerPath: filepath.Join(root, "provider-listener.json"), Credentials: credentials, PreferAddr: "127.0.0.1:0"})
		if err != nil {
			t.Fatal(err)
		}
		defer owner.Close()
		s.SetModelProfiles(owner)
		defer func() {
			for path, before := range configHashes {
				data, err := os.ReadFile(path)
				if err != nil || fmt.Sprintf("%x", sha256.Sum256(data)) != before {
					t.Errorf("global Provider configuration changed: %s", path)
				}
			}
		}()
	}
	host := httptest.NewServer(s.Handler())
	defer host.Close()
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(host.URL, "http")+"/ws", http.Header{"Authorization": {signature("mewla-connect")}})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	// Print mode is a bounded real task; config/auth use the configured provider.
	// No cwd/global MCP files are written and no coding delegation is requested.
	prompt := "Use ONLY the provided mewla_browser MCP browser tool. Call control, then snapshot. Confirm the page says Persistent authenticated true and Session authenticated true. Call release. Reply BROWSER_SHARED_SESSION_OK if both are true. Do not navigate, use shell or other tools, access accounts, or change files."
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	providerExecutable := "claude"
	if os.Getenv("MEWLA_BROWSER_PROVIDER") == "probe" {
		providerExecutable = quote(filepath.Join(root, "probe-bin", "claude"))
	}
	command := providerExecutable + " --print " + quote(prompt) + " --output-format json --max-turns 5 --no-session-persistence --setting-sources user"
	var ids []string
	taskCount := 2
	if count := os.Getenv("MEWLA_BROWSER_PROVIDER_TASKS"); count != "" {
		n, err := strconv.Atoi(count)
		if err != nil || n < 1 || n > 2 {
			t.Fatal("provider task budget must be 1 or 2")
		}
		taskCount = n
	}
	for n := 0; n < taskCount; n++ {
		reqID := fmt.Sprintf("browser-provider-%d", n)
		if err := ws.WriteJSON(map[string]any{"type": "create_session", "request_id": reqID, "name": reqID, "command": command, "cwd": scratch, "browser_id": id}); err != nil {
			t.Fatal(err)
		}
		workerID := ""
		_ = ws.SetReadDeadline(time.Now().Add(15 * time.Second))
		for workerID == "" {
			var msg struct {
				Type      string `json:"type"`
				RequestID string `json:"request_id"`
				WorkerID  string `json:"worker_id"`
				Message   string `json:"message"`
			}
			if err := ws.ReadJSON(&msg); err != nil {
				t.Fatal(err)
			}
			if msg.RequestID != reqID {
				continue
			}
			if msg.Type == "error" {
				t.Fatal(msg.Message)
			}
			workerID = msg.WorkerID
		}
		ids = append(ids, workerID)
		deadline := time.Now().Add(75 * time.Second)
		var pane string
		for {
			out, _ := exec.Command(realTmux, "-S", socket, "capture-pane", "-J", "-p", "-S", "-", "-t", workerID).Output()
			pane = string(out)
			observed.mu.Lock()
			snapshot := observed.snapshots[workerID]
			observed.mu.Unlock()
			state, _ := manager.Handle(ctx, browser.Owner{Kind: "human", ID: "test"}, browser.Request{Action: "status", ID: id})
			worker := w.GetWorker(workerID)
			dead, _ := exec.Command(realTmux, "-S", socket, "display-message", "-p", "-t", workerID, "#{pane_dead}").Output()
			if strings.TrimSpace(string(dead)) == "1" && strings.Contains(pane, `"result"`) && strings.Contains(pane, "BROWSER_SHARED_SESSION_OK") && strings.Contains(snapshot, "Persistent authenticated true") && strings.Contains(snapshot, target) && state.Resource != nil && state.Resource.Control == "idle" && worker != nil && worker.BrowserID == id {
				break
			}
			if strings.TrimSpace(string(dead)) == "1" && strings.Contains(pane, `"is_error":true`) {
				t.Fatal("provider returned an error before proving Browser attachment (output retained only in private fixture pane)")
			}
			if os.Getenv("MEWLA_BROWSER_PROVIDER") == "probe" && strings.TrimSpace(string(dead)) == "1" && !strings.Contains(pane, "BROWSER_SHARED_SESSION_OK") {
				t.Fatalf("local probe failed: %s", pane)
			}
			if time.Now().After(deadline) {
				t.Fatalf("provider task did not prove attachment; worker_present=%t observed_snapshot=%t output_has_result=%t", worker != nil, snapshot != "", strings.Contains(pane, `"result"`))
			}
			time.Sleep(250 * time.Millisecond)
		}
		if err := w.KillSession(workerID); err != nil {
			t.Fatal(err)
		}
		state, _ := manager.Handle(ctx, browser.Owner{Kind: "human", ID: "test"}, browser.Request{Action: "status", ID: id})
		if state.Resource == nil || state.Resource.State != "running" {
			t.Fatal("task finish destroyed user browser")
		}
	}
	if evidence := os.Getenv("MEWLA_BROWSER_EVIDENCE"); evidence != "" {
		raw, _ := json.MarshalIndent(map[string]any{"normal_authenticated_create_session": true, "real_provider": os.Getenv("MEWLA_BROWSER_PROVIDER") == "1", "provider": "claude", "tasks": len(ids), "real_watcher": true, "same_authenticated_target": true, "task_finish_preserves_browser": true, "global_mcp_configuration_modified": false}, "", "  ")
		_ = os.WriteFile(filepath.Join(evidence, "normal-provider-result.json"), raw, 0600)
	}
}
