package agentuse

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Tab cleanup bounds: each close waits this long, and all of them together
// at most cleanupTotal, so an agent's MCP host is never held up for long.
var (
	cleanupStep  = 5 * time.Second
	cleanupTotal = 15 * time.Second
	cleanupMax   = 50
)

// RelayMCP is what an agent's MCP host runs. It starts helper (which runs
// SuperviseMCP) detached, in a session of its own, writing to the host
// directly, and passes it the host's stdin. However this relay ends, a quit,
// a hang-up of the Worker's terminal or a kill (Claude kills its MCP servers
// within half a second), the helper only sees its stdin end, and still has
// the time to close the agent's tabs.
func RelayMCP(helper, env []string, stdin io.Reader, stdout, stderr *os.File) error {
	cmd := exec.Command(helper[0], helper[1:]...)
	cmd.Env = env
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.SysProcAttr = detachedProcAttr()
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() {
		_, _ = io.Copy(in, stdin)
		_ = in.Close()
	}()
	return cmd.Wait()
}

// SuperviseMCP runs the browser MCP server between an agent and Playwright,
// relaying stdio, with Mewla's advice added to a few tools. When the agent goes (its stdin ends) and it used
// the browser, it closes the agent's own tabs first: in extension mode those
// are the only tabs Playwright sees, and Chrome drops the emptied tab group.
// (Left alone, the extension only ungroups them.) A 24/7 computer then never
// piles up tabs.
func SuperviseMCP(argv, env []string, stdin io.Reader, stdout, stderr io.Writer) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = env
	cmd.Stderr = stderr
	childIn, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	childOut, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	s := &supervisor{childIn: childIn, stdout: stdout, pending: map[string]chan json.RawMessage{}, lists: map[string]bool{}}
	exited := make(chan error, 1)
	relayed := make(chan struct{})
	go func() {
		defer close(relayed)
		s.relayOut(childOut)
	}()
	go func() {
		<-relayed
		exited <- cmd.Wait()
	}()
	agentGone := make(chan struct{})
	go func() {
		defer close(agentGone)
		s.relayIn(stdin)
	}()

	select {
	case err := <-exited:
		return err
	case <-agentGone:
	}
	if s.usedBrowser() {
		s.closeTabs()
	}
	_ = childIn.Close()
	select {
	case err := <-exited:
		return err
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		return <-exited
	}
}

type supervisor struct {
	mu      sync.Mutex
	childIn io.Writer
	stdout  io.Writer
	used    bool
	pending map[string]chan json.RawMessage
	next    int
	// lists are the agent's tools/list requests still unanswered.
	lists map[string]bool
}

// toolAdvice is added to these tools' descriptions. The page an action
// leaves is not sent back (--snapshot-mode none), and every tool call is a
// model turn, which takes far longer than the browser does: agents get a
// task done fastest by scripting it, and reading only what they need.
// Codex doesn't show an MCP server's instructions, so the advice goes where
// every agent reads it.
var toolAdvice = map[string]string{
	"browser_run_code_unsafe": "Each tool call costs a model turn, which takes far longer than the browser. Once you know the controls a flow needs (from browser_find), do the rest of it (click, fill, wait for the result) in one script, using getByRole with the names you found and a short timeout on each wait, such as { timeout: 5000 }, so a wrong guess fails fast. Return just what you need to read. A send, submit or payment goes in the script only if it is already approved; otherwise stop the script before it, ask, then finish.",
	"browser_snapshot":        "Actions don't return the page, so take a snapshot only when you need to see it. A busy app's whole page (chat, mail, docs) can run to hundreds of thousands of tokens: prefer browser_find, or pass target to snapshot one part.",
	"browser_find":            "The cheap way to learn the controls a flow needs, or to check that it worked: search for the few names you need, not for every button or textbox.",
}

type rpcMessage struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params struct {
		Name string `json:"name"`
	} `json:"params"`
}

// relayIn passes the agent's messages to Playwright, noting whether it ever
// used a browser tool.
func (s *supervisor) relayIn(r io.Reader) {
	reader := bufio.NewReaderSize(r, 1<<20)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			var msg rpcMessage
			if json.Unmarshal(line, &msg) == nil {
				s.mu.Lock()
				if msg.Method == "tools/call" && strings.HasPrefix(msg.Params.Name, "browser_") {
					s.used = true
				}
				if msg.Method == "tools/list" && len(msg.ID) > 0 {
					s.lists[string(msg.ID)] = true
				}
				s.mu.Unlock()
			}
			if s.write(line) != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// relayOut passes Playwright's messages to the agent, keeping the answers
// to the supervisor's own requests.
func (s *supervisor) relayOut(r io.Reader) {
	reader := bufio.NewReaderSize(r, 1<<20)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 && !s.answered(line) {
			_, _ = s.stdout.Write(s.advised(line))
		}
		if err != nil {
			return
		}
	}
}

func (s *supervisor) answered(line []byte) bool {
	if !bytes.Contains(line, []byte(`"mewla-cleanup-`)) {
		return false
	}
	var msg struct {
		ID json.RawMessage `json:"id"`
	}
	if json.Unmarshal(line, &msg) != nil {
		return false
	}
	var id string
	if json.Unmarshal(msg.ID, &id) != nil {
		return false
	}
	s.mu.Lock()
	reply, ok := s.pending[id]
	delete(s.pending, id)
	s.mu.Unlock()
	if ok {
		reply <- append(json.RawMessage(nil), line...)
	}
	return ok
}

// advised adds toolAdvice to the answer to an agent's tools/list. Anything
// else, or an answer it can't read, passes unchanged.
func (s *supervisor) advised(line []byte) []byte {
	s.mu.Lock()
	waiting := len(s.lists) > 0
	s.mu.Unlock()
	if !waiting || !bytes.Contains(line, []byte(`"tools"`)) {
		return line
	}
	var msg map[string]json.RawMessage
	if json.Unmarshal(line, &msg) != nil {
		return line
	}
	s.mu.Lock()
	asked := s.lists[string(msg["id"])]
	delete(s.lists, string(msg["id"]))
	s.mu.Unlock()
	var result map[string]json.RawMessage
	var tools []map[string]json.RawMessage
	if !asked || json.Unmarshal(msg["result"], &result) != nil || json.Unmarshal(result["tools"], &tools) != nil {
		return line
	}
	for _, tool := range tools {
		var name, description string
		_ = json.Unmarshal(tool["name"], &name)
		advice, ok := toolAdvice[name]
		if !ok {
			continue
		}
		_ = json.Unmarshal(tool["description"], &description)
		if description != "" {
			advice = description + "\n\n" + advice
		}
		tool["description"], _ = json.Marshal(advice)
	}
	var err error
	if result["tools"], err = json.Marshal(tools); err != nil {
		return line
	}
	if msg["result"], err = json.Marshal(result); err != nil {
		return line
	}
	out, err := json.Marshal(msg)
	if err != nil {
		return line
	}
	return append(out, '\n')
}

func (s *supervisor) write(line []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.childIn.Write(line)
	return err
}

func (s *supervisor) usedBrowser() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.used
}

// closeTabs closes the agent's tabs one by one until none is left. Each
// answer lists the tabs still open; asking again once none is left would
// wait on the extension, so it stops there.
func (s *supervisor) closeTabs() {
	ctx, cancel := context.WithTimeout(context.Background(), cleanupTotal)
	defer cancel()
	for i := 0; i < cleanupMax; i++ {
		remaining, err := s.call(ctx, "browser_tabs", map[string]any{"action": "close", "index": 0})
		if err != nil || remaining == 0 {
			return
		}
	}
}

// openTab is one line of Playwright's tab list: "- 0: (current) [title](url)".
var openTab = regexp.MustCompile(`(?m)^- \d+: `)

var errToolFailed = errors.New("tool failed")

// call runs one tool and counts the tabs its answer lists.
func (s *supervisor) call(ctx context.Context, tool string, args map[string]any) (int, error) {
	s.mu.Lock()
	s.next++
	id := "mewla-cleanup-" + strconv.Itoa(s.next)
	reply := make(chan json.RawMessage, 1)
	s.pending[id] = reply
	s.mu.Unlock()
	raw, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": id, "method": "tools/call",
		"params": map[string]any{"name": tool, "arguments": args},
	})
	if err := s.write(append(raw, '\n')); err != nil {
		return 0, err
	}
	step, cancel := context.WithTimeout(ctx, cleanupStep)
	defer cancel()
	select {
	case line := <-reply:
		var result struct {
			Error  json.RawMessage `json:"error"`
			Result struct {
				IsError bool `json:"isError"`
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"result"`
		}
		if json.Unmarshal(line, &result) != nil || len(result.Error) > 0 || result.Result.IsError {
			return 0, errToolFailed
		}
		tabs := 0
		for _, c := range result.Result.Content {
			tabs += len(openTab.FindAllString(c.Text, -1))
		}
		return tabs, nil
	case <-step.Done():
		return 0, step.Err()
	}
}
