package agentuse

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestFakePlaywrightHelper is a stand-in Playwright MCP: browser_navigate
// opens a tab, browser_tabs close index 0 closes the first one or fails when
// none is left. It writes what it saw when its stdin ends.
func TestFakePlaywrightHelper(t *testing.T) {
	if os.Getenv("MEWLA_FAKE_PLAYWRIGHT") != "1" {
		return
	}
	tabs, closes := 0, 0
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &msg) != nil || msg.ID == nil {
			continue
		}
		result := map[string]any{"content": []any{}}
		if msg.Method == "tools/list" {
			result = map[string]any{"tools": []any{
				map[string]any{"name": "browser_snapshot", "description": "Capture accessibility snapshot of the current page.", "inputSchema": map[string]any{"type": "object"}},
				map[string]any{"name": "browser_click", "description": "Perform click on a web page", "inputSchema": map[string]any{"type": "object"}},
			}}
		}
		switch msg.Params.Name {
		case "browser_navigate":
			tabs++
		case "browser_tabs":
			closes++
			if tabs == 0 {
				continue // like Playwright: asked with no tab left, it waits
			}
			tabs--
			list := "### Result\nNo open tabs. Navigate to a URL to create one."
			if tabs > 0 {
				list = "### Result"
				for i := 0; i < tabs; i++ {
					list += fmt.Sprintf("\n- %d: [page](http://example.test/%d)", i, i)
				}
			}
			result["content"] = []any{map[string]any{"type": "text", "text": list}}
		}
		raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "result": result})
		fmt.Println(string(raw))
	}
	_ = os.WriteFile(os.Getenv("MEWLA_FAKE_PLAYWRIGHT_LOG"), []byte(fmt.Sprintf("tabs=%d closes=%d", tabs, closes)), 0o600)
	os.Exit(0)
}

type supervised struct {
	in  io.WriteCloser
	out *bufio.Scanner
	ret chan error
	log string
}

func fakePlaywright(t *testing.T) ([]string, []string, string) {
	exe, _ := os.Executable()
	log := filepath.Join(t.TempDir(), "log")
	env := append(os.Environ(), "MEWLA_FAKE_PLAYWRIGHT=1", "MEWLA_FAKE_PLAYWRIGHT_LOG="+log)
	return []string{exe, "-test.run=^TestFakePlaywrightHelper$"}, env, log
}

func superviseFake(t *testing.T) *supervised {
	t.Helper()
	argv, env, log := fakePlaywright(t)
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	s := &supervised{in: inW, out: bufio.NewScanner(outR), ret: make(chan error, 1), log: log}
	go func() {
		s.ret <- SuperviseMCP(argv, env, inR, outW, os.Stderr)
		_ = outW.Close()
	}()
	return s
}

func (s *supervised) call(t *testing.T, id any, tool string) string {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": "tools/call", "params": map[string]any{"name": tool, "arguments": map[string]any{}}})
	if _, err := s.in.Write(append(raw, '\n')); err != nil {
		t.Fatal(err)
	}
	if !s.out.Scan() {
		t.Fatal("no answer")
	}
	return s.out.Text()
}

// rest is everything else the agent was sent, once the supervisor is done.
func (s *supervised) finish(t *testing.T) (string, string) {
	t.Helper()
	var rest []string
	for s.out.Scan() {
		rest = append(rest, s.out.Text())
	}
	select {
	case err := <-s.ret:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the supervisor did not finish")
	}
	log, _ := os.ReadFile(s.log)
	return string(log), strings.Join(rest, "\n")
}

func TestAnAgentsTabsCloseWhenItDisconnects(t *testing.T) {
	s := superviseFake(t)
	if got := s.call(t, 1, "browser_navigate"); !strings.Contains(got, `"id":1`) {
		t.Fatalf("answer %s", got)
	}
	s.call(t, "a", "browser_navigate")
	s.call(t, 3, "browser_navigate")
	_ = s.in.Close() // the MCP host disconnects
	log, rest := s.finish(t)
	// Three tabs closed, and no fourth request once none is left.
	if log != "tabs=0 closes=3" {
		t.Fatalf("Playwright saw %s", log)
	}
	if rest != "" {
		t.Fatalf("the agent was sent the cleanup's answers: %s", rest)
	}
}

// TestRelayHelper plays both processes an agent's MCP host starts: the
// relay, and the detached supervisor it starts.
func TestRelayHelper(t *testing.T) {
	switch os.Getenv("MEWLA_RELAY_ROLE") {
	case "relay":
		exe, _ := os.Executable()
		env := append(os.Environ(), "MEWLA_RELAY_ROLE=supervisor")
		_ = RelayMCP([]string{exe, "-test.run=^TestRelayHelper$"}, env, os.Stdin, os.Stdout, os.Stderr)
		os.Exit(0)
	case "supervisor":
		argv, env := strings.Fields(os.Getenv("MEWLA_RELAY_FAKE")), os.Environ()
		_ = SuperviseMCP(argv, append(env, "MEWLA_RELAY_ROLE="), os.Stdin, os.Stdout, os.Stderr)
		os.Exit(0)
	}
}

func TestAgentsAreToldToScriptAndReadOnlyWhatTheyNeed(t *testing.T) {
	s := superviseFake(t)
	if _, err := s.in.Write([]byte(`{"jsonrpc":"2.0","id":7,"method":"tools/list"}` + "\n")); err != nil {
		t.Fatal(err)
	}
	if !s.out.Scan() {
		t.Fatal("no answer")
	}
	var answer struct {
		ID     int `json:"id"`
		Result struct {
			Tools []struct {
				Name        string         `json:"name"`
				Description string         `json:"description"`
				InputSchema map[string]any `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(s.out.Bytes(), &answer); err != nil || answer.ID != 7 || len(answer.Result.Tools) != 2 {
		t.Fatalf("answer %s (%v)", s.out.Text(), err)
	}
	snapshot, click := answer.Result.Tools[0], answer.Result.Tools[1]
	if want := "Capture accessibility snapshot of the current page.\n\n" + toolAdvice["browser_snapshot"]; snapshot.Description != want || snapshot.InputSchema["type"] != "object" {
		t.Fatalf("browser_snapshot is %+v", snapshot)
	}
	if click.Description != "Perform click on a web page" {
		t.Fatalf("browser_click is %+v", click)
	}
	// Tool answers pass unchanged.
	if got := s.call(t, 8, "some_other_tool"); got != `{"id":8,"jsonrpc":"2.0","result":{"content":[]}}` {
		t.Fatalf("tool answer %s", got)
	}
	_ = s.in.Close()
	s.finish(t)
}

func TestAnAgentThatNeverUsedTheBrowserLeavesChromeAlone(t *testing.T) {
	s := superviseFake(t)
	s.call(t, 1, "some_other_tool")
	_ = s.in.Close()
	if log, _ := s.finish(t); log != "tabs=0 closes=0" {
		t.Fatalf("Playwright saw %s", log)
	}
}
