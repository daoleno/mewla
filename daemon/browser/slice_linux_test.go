//go:build linux

package browser

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/agentuse"
)

// The agent Chrome carries the Mewla extension on a real kwin screen and the
// extension finds Mewla's native messaging host. Opt-in: it starts a real
// virtual screen and Chrome.
//
//	MEWLA_BROWSER_INTEGRATION=1 go test ./browser -run TestAgentChromeCarriesExtension -v -count=1
func TestAgentChromeCarriesExtension(t *testing.T) {
	if os.Getenv("MEWLA_BROWSER_INTEGRATION") != "1" {
		t.Skip("set MEWLA_BROWSER_INTEGRATION=1")
	}
	root := t.TempDir()
	state := filepath.Join(root, "state")
	svc, err := agentuse.New(state, false)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "host-started")
	stub := filepath.Join(root, "host.sh")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\necho \"$@\" >> "+marker+"\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	b := &LinuxBackend{Prepare: func(profile string) (string, error) {
		dir, err := svc.ExtensionDir()
		if err != nil {
			return "", err
		}
		hosts := filepath.Join(profile, "NativeMessagingHosts")
		if err := os.MkdirAll(hosts, 0o755); err != nil {
			return "", err
		}
		manifest, _ := json.Marshal(map[string]any{"name": agentuse.NativeHostName, "description": "test", "path": stub, "type": "stdio", "allowed_origins": []string{"chrome-extension://" + agentuse.MewlaExtensionID + "/"}})
		return dir, os.WriteFile(filepath.Join(hosts, agentuse.NativeHostName+".json"), manifest, 0o644)
	}}
	for launch := 1; launch <= 2; launch++ {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		host, err := b.Start(ctx, filepath.Join(root, "agent"))
		cancel()
		if err != nil {
			t.Fatalf("launch %d: %v", launch, err)
		}
		h := host.(*linuxHost)
		if h.extensionID != agentuse.MewlaExtensionID {
			t.Fatalf("launch %d: extension ID %q", launch, h.extensionID)
		}
		deadline := time.Now().Add(15 * time.Second)
		var started bool
		for time.Now().Before(deadline) {
			if raw, err := os.ReadFile(marker); err == nil && strings.Count(string(raw), "chrome-extension://") >= launch {
				started = true
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		raw, _ := os.ReadFile(marker)
		t.Logf("launch %d: extension %s loaded; host marker: %q", launch, h.extensionID, strings.TrimSpace(string(raw)))
		if !started {
			t.Fatalf("launch %d: Chrome never started Mewla's native host for the extension", launch)
		}
		tctx, c2 := context.WithTimeout(context.Background(), 5*time.Second)
		targets, _ := h.browserCDP(tctx, "Target.getTargets", map[string]any{})
		c2()
		t.Logf("targets: %d bytes", len(targets))
		host.Close()
	}
}
