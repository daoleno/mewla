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
func SuperviseMCP(argv, env []string, gate Gate, stdin io.Reader, stdout, stderr io.Writer) error {
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
	s := &supervisor{childIn: childIn, stdout: stdout, gate: gate, pending: map[string]chan json.RawMessage{}, lists: map[string]bool{}, compact: map[string]bool{}, gated: map[string]bool{}, reads: map[string]bool{}}
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

// Gate is the daemon's say over each browser tool call of a Worker: Begin may
// refuse it (the person took the window over, or stopped the Worker), and End
// returns what the person did in the window meanwhile, for the Worker to hear
// with the result. A Gate that cannot reach the daemon lets the call through.
type Gate interface {
	Begin() error
	End() []string
}

type supervisor struct {
	gate Gate
	// outMu orders what goes to the agent: the supervisor answers some calls
	// itself while Playwright's answers arrive.
	outMu sync.Mutex
	// gated are the tool calls Begin let through and End is owed for.
	gated   map[string]bool
	mu      sync.Mutex
	childIn io.Writer
	stdout  io.Writer
	used    bool
	pending map[string]chan json.RawMessage
	next    int
	// lists are the agent's tools/list requests still unanswered.
	lists map[string]bool
	// compact are the browser_snapshot calls whose answer is to be reduced to controls.
	compact map[string]bool
	// reads are the browser_read calls, which Playwright answers as an evaluate.
	reads map[string]bool
}

// maxPageText bounds what one browser_read returns to the agent, in characters.
const maxPageText = 40000

// readTool is the one tool Mewla adds to Playwright's: a snapshot lists what
// can be clicked, and an article, a thread or a table is read as text.
const readTool = `{"name":"browser_read","description":"Read the current page's text, as a person would read it, with the page's address. Use it for an article, a post, a table or a long page; browser_snapshot lists the controls, this reads the words. The text is cut at 40000 characters, and says so.","inputSchema":{"type":"object","properties":{},"additionalProperties":false},"annotations":{"title":"Read page text","readOnlyHint":true}}`

// readScript is what a browser_read runs in the page.
const readScript = `() => JSON.stringify({ url: location.href, text: document.body ? document.body.innerText : '' })`

// toolAdvice is added to these tools' descriptions. The page an action
// leaves is not sent back (--snapshot-mode none), and every tool call is a
// model turn, which takes far longer than the browser does: agents get a
// task done fastest by scripting it, and reading only what they need.
// Codex doesn't show an MCP server's instructions, so the advice goes where
// every agent reads it.
var toolAdvice = map[string]string{
	"browser_run_code_unsafe": "Each tool call costs a model turn, which takes far longer than the browser. Script a whole flow in one call: when the task names the fields and buttons (a form, a wizard with Next buttons, items to add and tick), take their names from the task, or from browser_find, and fill, click and wait in one script with getByLabel, getByRole or getByText and a short timeout on each step, such as { timeout: 3000 }, so a wrong guess fails fast. Only if it fails, look at the page (browser_find, or a snapshot of one part) and script the rest. Return just what you need to read. To wait for a change use page.waitForFunction(condition, null, { timeout: 5000 }) or expect(locator).toHaveText(...), not waitForTimeout or a loop of sleeps. A send, submit or payment goes in the script only if it is already approved; otherwise stop the script before it, ask, then finish.",
	"browser_snapshot":        "Actions don't return the page, so take a snapshot only when you need to see it. A busy app's whole page (chat, mail, docs) can run to hundreds of thousands of tokens: prefer browser_find, pass target to snapshot one part, or pass interactive: true for just the controls and headings with their refs.",
	"browser_tabs":            "To open or read several pages, do it in one browser_run_code_unsafe script (page.context().newPage(), goto, read what you need, return it all) rather than one call per tab. Use this tool for a single tab operation.",
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
				if msg.Method == "tools/call" && strings.HasPrefix(msg.Params.Name, "browser_") && len(msg.ID) > 0 && s.gate != nil {
					if err := s.gate.Begin(); err != nil {
						s.refuse(msg.ID, err.Error())
						continue
					}
					s.mu.Lock()
					s.gated[string(msg.ID)] = true
					s.mu.Unlock()
				}
				s.mu.Lock()
				if msg.Method == "tools/call" && strings.HasPrefix(msg.Params.Name, "browser_") {
					s.used = true
				}
				s.mu.Unlock()
				if msg.Method == "tools/call" && msg.Params.Name == "browser_read" && len(msg.ID) > 0 {
					line = s.asEvaluate(msg.ID, line)
				}
				s.mu.Lock()
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
			line = s.advised(s.compacted(s.readAnswer(line)))
			s.outMu.Lock()
			_, _ = s.stdout.Write(s.noted(line))
			s.outMu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

// asEvaluate turns a browser_read call into the evaluate Playwright runs.
func (s *supervisor) asEvaluate(id json.RawMessage, line []byte) []byte {
	raw, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": "tools/call",
		"params": map[string]any{"name": "browser_evaluate", "arguments": map[string]any{"function": readScript}}})
	if err != nil {
		return line
	}
	s.mu.Lock()
	s.reads[string(id)] = true
	s.mu.Unlock()
	return append(raw, '\n')
}

// resultText is the text of Playwright's evaluate answer: the JSON string the
// page script returned, between the "Result" heading and the next.
var resultText = regexp.MustCompile(`(?s)### Result\s*\n(.*?)(?:\n### |\z)`)

// readAnswer turns the evaluate answer of a browser_read into the page's text.
// An answer it can't read passes unchanged, so the agent sees what happened.
func (s *supervisor) readAnswer(line []byte) []byte {
	s.mu.Lock()
	waiting := len(s.reads) > 0
	s.mu.Unlock()
	if !waiting || !bytes.Contains(line, []byte(`"result"`)) {
		return line
	}
	var msg map[string]json.RawMessage
	if json.Unmarshal(line, &msg) != nil || len(msg["id"]) == 0 {
		return line
	}
	s.mu.Lock()
	wanted := s.reads[string(msg["id"])]
	delete(s.reads, string(msg["id"]))
	s.mu.Unlock()
	if !wanted {
		return line
	}
	var result struct {
		IsError bool `json:"isError"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if json.Unmarshal(msg["result"], &result) != nil || result.IsError || len(result.Content) == 0 {
		return line
	}
	found := resultText.FindStringSubmatch(result.Content[0].Text)
	if found == nil {
		return line
	}
	var inner string
	var page struct {
		URL  string `json:"url"`
		Text string `json:"text"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(found[1])), &inner) != nil || json.Unmarshal([]byte(inner), &page) != nil {
		return line
	}
	runes := []rune(page.Text)
	note := ""
	if len(runes) > maxPageText {
		runes, note = runes[:maxPageText], "\n\n[The text was cut at 40000 characters.]"
	}
	text := "Page: " + page.URL + "\n\n" + string(runes) + note
	answer, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": msg["id"], "result": map[string]any{"content": []map[string]string{{"type": "text", "text": text}}}})
	if err != nil {
		return line
	}
	return append(answer, '\n')
}

// refuse answers a tool call itself, as a failed call with the reason.
func (s *supervisor) refuse(id json.RawMessage, why string) {
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"isError": true, "content": []map[string]string{{"type": "text", "text": why}}}})
	s.outMu.Lock()
	_, _ = s.stdout.Write(append(raw, '\n'))
	s.outMu.Unlock()
}

// noted ends the gate for the tool call a line answers and adds what the
// person did in the window since the Worker last heard, as one more block of
// text; every other line passes unchanged.
func (s *supervisor) noted(line []byte) []byte {
	s.mu.Lock()
	waiting := len(s.gated) > 0
	s.mu.Unlock()
	if !waiting || (!bytes.Contains(line, []byte(`"result"`)) && !bytes.Contains(line, []byte(`"error"`))) {
		return line
	}
	var msg map[string]json.RawMessage
	if json.Unmarshal(line, &msg) != nil || len(msg["id"]) == 0 {
		return line
	}
	s.mu.Lock()
	owed := s.gated[string(msg["id"])]
	delete(s.gated, string(msg["id"]))
	s.mu.Unlock()
	if !owed {
		return line
	}
	notes := s.gate.End()
	if len(notes) == 0 || msg["result"] == nil {
		return line
	}
	var result map[string]json.RawMessage
	var content []json.RawMessage
	if json.Unmarshal(msg["result"], &result) != nil {
		return line
	}
	if result["content"] != nil && json.Unmarshal(result["content"], &content) != nil {
		return line
	}
	block, _ := json.Marshal(map[string]string{"type": "text", "text": strings.Join(notes, "\n")})
	content = append(content, block)
	var err error
	if result["content"], err = json.Marshal(content); err != nil {
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

// compacted reduces the answer to a browser_snapshot call made with
// interactive: true; every other line passes unchanged.
func (s *supervisor) compacted(line []byte) []byte {
	s.mu.Lock()
	waiting := len(s.compact) > 0
	s.mu.Unlock()
	if !waiting || !bytes.Contains(line, []byte(`"result"`)) {
		return line
	}
	var msg struct {
		ID json.RawMessage `json:"id"`
	}
	if json.Unmarshal(line, &msg) != nil || len(msg.ID) == 0 {
		return line
	}
	s.mu.Lock()
	wanted := s.compact[string(msg.ID)]
	delete(s.compact, string(msg.ID))
	s.mu.Unlock()
	if !wanted {
		return line
	}
	return compactAnswer(line)
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
	tools = append(tools, map[string]json.RawMessage{})
	_ = json.Unmarshal([]byte(readTool), &tools[len(tools)-1])
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
		if name == "browser_snapshot" {
			tool["inputSchema"] = withInteractive(tool["inputSchema"])
		}
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
