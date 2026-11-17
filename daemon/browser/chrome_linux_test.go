//go:build linux

package browser

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"
)

// The agent browser keeps the debugging pipe (the extension is loaded over
// it) but its pages are not marked as automated: Google refuses to sign in
// to a browser whose navigator.webdriver is true.
func TestChromeArgvKeepsThePipeWithoutMarkingPagesAutomated(t *testing.T) {
	argv := chromeArgv("/opt/google/chrome/chrome", "/p/profile", storeBasic)
	for _, want := range []string{"--remote-debugging-pipe", "--disable-blink-features=AutomationControlled", "--user-data-dir=/p/profile", "--password-store=basic"} {
		if !slices.Contains(argv, want) {
			t.Errorf("argv lacks %s: %v", want, argv)
		}
	}
	for _, flag := range argv {
		if flag == "--enable-automation" || strings.HasPrefix(flag, "--headless") || strings.HasPrefix(flag, "--remote-debugging-port") {
			t.Errorf("argv has %s, which marks the browser as automated or opens a port", flag)
		}
	}
}

// A real Chrome started the agent browser's way reports navigator.webdriver
// false to its pages. Set MEWLA_E2E_CHROME to a Chrome and DISPLAY to an X
// server (not headless: headless marks pages itself).
func TestChromeStartedTheAgentBrowsersWayIsNotMarkedAutomated(t *testing.T) {
	chrome := os.Getenv("MEWLA_E2E_CHROME")
	if chrome == "" || os.Getenv("DISPLAY") == "" {
		t.Skip("set MEWLA_E2E_CHROME and DISPLAY")
	}
	toChrome, ours, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	theirs, fromChrome, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	argv := chromeArgv(chrome, t.TempDir(), storeBasic)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.ExtraFiles = []*os.File{toChrome, fromChrome} // fds 3 and 4
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	_ = toChrome.Close()
	_ = fromChrome.Close()
	pipe := newPipeCDP(ours, theirs)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, _ = pipe.call(ctx, "Browser.close", nil)
		cancel()
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
		pipe.close()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// The page writes what it sees into its title; reading the title needs no
	// debugger attached to the page.
	page := "data:text/html,<script>document.title='webdriver='+navigator.webdriver</script>"
	if _, err := pipe.call(ctx, "Target.createTarget", map[string]any{"url": page, "newWindow": true}); err != nil {
		t.Fatal(err)
	}
	for {
		raw, err := pipe.call(ctx, "Target.getTargets", nil)
		if err != nil {
			t.Fatal(err)
		}
		var targets struct {
			TargetInfos []struct {
				Type  string `json:"type"`
				Title string `json:"title"`
			} `json:"targetInfos"`
		}
		_ = json.Unmarshal(raw, &targets)
		for _, target := range targets.TargetInfos {
			if target.Type != "page" || !strings.HasPrefix(target.Title, "webdriver=") {
				continue
			}
			if target.Title != "webdriver=false" {
				t.Fatalf("a page of the agent browser sees %s; Google refuses to sign in to it", target.Title)
			}
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("the page never reported navigator.webdriver")
		case <-time.After(200 * time.Millisecond):
		}
	}
}
