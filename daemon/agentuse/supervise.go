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
// relaying stdio unchanged. When the agent goes (its stdin ends) and it used
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
	s := &supervisor{childIn: childIn, stdout: stdout, pending: map[string]chan json.RawMessage{}}
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
			if json.Unmarshal(line, &msg) == nil && msg.Method == "tools/call" && strings.HasPrefix(msg.Params.Name, "browser_") {
				s.mu.Lock()
				s.used = true
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
			_, _ = s.stdout.Write(line)
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
