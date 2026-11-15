//go:build linux || darwin

package compatsweep

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// specialText carries every character that breaks a shell, cmd.exe, JSON or
// a terminal paste: %, both quotes, &, $, a backtick command, ^, |, <, >,
// a backslash, a newline, CJK and an emoji.
const specialText = "Sweep marker %PATH% \"double\" 'single' & $HOME `echo hi` ^caret | <in> >out \\back\nsecond line 中文 🐈"

// transcriptHolds reports whether any provider transcript under root holds
// text exactly, as JSON-escaped in a transcript line (agents write <, > and &
// unescaped). When it does not, near is the transcript's text around the
// prompt's first words, for the record.
func transcriptHolds(root, text string) (held bool, near string) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(text)
	needle := strings.Trim(strings.TrimSpace(buf.String()), `"`)
	head := text[:strings.Index(text, " ")+7]
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || held || info.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		if strings.Contains(string(raw), needle) {
			held = true
		} else if index := strings.Index(string(raw), head); index >= 0 && near == "" {
			end := min(len(raw), index+len(needle)+40)
			near = string(raw[index:end])
		}
		return nil
	})
	return held, near
}

type workerRow struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
}

func (e *env) workers(t *testing.T) []workerRow {
	out, err := e.cli(30*time.Second, "worker", "list", "@state", "--json")
	if err != nil {
		return nil
	}
	var reply struct {
		Workers []workerRow `json:"workers"`
	}
	_ = json.Unmarshal([]byte(out[strings.Index(out, "{"):]), &reply)
	return reply.Workers
}

func (e *env) waitWorker(t *testing.T, name string, timeout time.Duration, match func(workerRow) bool) (workerRow, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, w := range e.workers(t) {
			if strings.HasPrefix(w.Name, name) && (match == nil || match(w)) {
				return w, true
			}
		}
		time.Sleep(3 * time.Second)
	}
	return workerRow{}, false
}

// TestWorkerDispatchOnEachExecutor: Brain spawns a Worker from a Chat request,
// the Worker replies, Brain gets its result and says so in Chat, and the
// Worker is closed; on claude and on codex. A prompt full of special
// characters reaches each agent exactly.
func TestWorkerDispatchOnEachExecutor(t *testing.T) {
	realAgent(t, "claude")
	realAgent(t, "codex")
	e := newRealEnv(t, realOptions{})
	project := filepath.Join(e.home, "projects", "sweep app")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	e.start()
	c := e.connect(e.pair())
	for _, executor := range []string{"claude", "codex"} {
		t.Run(executor, func(t *testing.T) {
			if out, err := e.cli(3*time.Minute, "brain", "use", "@state", executor); err != nil {
				record(t, "dispatch-"+executor, "fail", "brain use: "+oneLine(out))
				t.Fatal(out)
			}
			hostID, last := brainHost(c, executor, 3*time.Minute)
			if hostID == "" {
				record(t, "dispatch-"+executor, "fail", fmt.Sprintf("no %s Brain host: %v", executor, find(last, "host_problem")))
				t.Fatal("no host")
			}
			name := "SweepW-" + executor
			done := fmt.Sprintf("SWEEPBRAIN-%s-%s", strings.ToUpper(executor), uuid.NewString()[:6])
			ask := fmt.Sprintf("Compatibility check. Start exactly one Worker now with mewla worker spawn: name %q, executor %s, "+
				"working directory %q, profile quick, prompt \"Reply with the single word SWEEPW-OK and report done.\". "+
				"Do not do the task yourself. When its result arrives, close that Worker with mewla worker close, then reply to me with only %s.",
				name, executor, project, done)
			if _, err := chat(c, hostID, ask); err != nil {
				record(t, "dispatch-"+executor, "fail", "chat: "+err.Error())
				t.Fatal(err)
			}
			worker, spawned := e.waitWorker(t, name, 4*time.Minute, nil)
			reply, replied := e.waitReply(done, 8*time.Minute)
			_, stillOpen := e.waitWorker(t, name, 5*time.Second, nil)
			pass := spawned && replied && !stillOpen
			detail := fmt.Sprintf("spawned=%v (%s %s) brain reported back=%v closed=%v reply=%q", spawned, worker.ID, worker.Status, replied, !stillOpen, oneLine(reply))
			if !pass {
				detail += "\n" + e.logTail(12)
			}
			record(t, "dispatch-"+executor, map[bool]string{true: "pass", false: "fail"}[pass], detail)
			if !pass {
				t.Error(detail)
			}

			// Special characters, spawned directly so nothing paraphrases them.
			response, err := e.cli(2*time.Minute, "worker", "spawn", "@state", "-name", "SweepChars-"+executor, "-executor", executor,
				"-cwd", project, "-profile", "quick", "-prompt", specialText+"\nJust reply OK.")
			root := filepath.Join(e.home, ".claude", "projects")
			if executor == "codex" {
				root = filepath.Join(e.home, ".codex", "sessions")
			}
			deadline := time.Now().Add(3 * time.Minute)
			held, near := false, ""
			for !held && time.Now().Before(deadline) {
				held, near = transcriptHolds(root, specialText)
				if !held {
					time.Sleep(3 * time.Second)
				}
			}
			detail = "the agent's transcript holds the prompt byte for byte"
			if err != nil || !held {
				detail = fmt.Sprintf("spawn err=%v held=%v; transcript has %q: %s", err, held, near, oneLine(response))
			}
			record(t, "special-chars-prompt-"+executor, map[bool]string{true: "pass", false: "fail"}[err == nil && held], detail)
			if w, ok := e.waitWorker(t, "SweepChars-"+executor, 5*time.Second, nil); ok {
				_, _ = e.cli(time.Minute, "worker", "close", "@state", "-id", w.ID, "--force")
			}
		})
	}
}

// TestWorkerPromptAnsweredFromChat: a claude Worker that asks permission (not
// bypassed) shows the decision, and answering it the way the Chat card does
// lets it go on.
func TestWorkerPromptAnsweredFromChat(t *testing.T) {
	realAgent(t, "claude")
	e := newRealEnv(t, realOptions{})
	project := filepath.Join(e.home, "projects", "ask")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	e.start()
	c := e.connect(e.pair())
	response, err := e.cli(2*time.Minute, "worker", "spawn", "@state", "-name", "SweepAsk", "-executor", "claude", "-cwd", project,
		"-command", "claude --model claude-sonnet-5-5 --permission-mode default", "-profile", "quick",
		"-prompt", "Run this exact shell command with your Bash tool: touch approved-by-chat.txt")
	if err != nil {
		record(t, "worker-prompt-answered-from-chat", "fail", "spawn: "+oneLine(response))
		t.Fatal(response)
	}
	var approval map[string]any
	var workerID string
	deadline := time.Now().Add(4 * time.Minute)
	for approval == nil && time.Now().Before(deadline) {
		out, _ := e.cli(30*time.Second, "worker", "approvals", "@state")
		if index := strings.Index(out, "{"); index >= 0 {
			var reply map[string]any
			if json.Unmarshal([]byte(out[index:]), &reply) == nil {
				if list, ok := find(reply, "approvals").([]any); ok && len(list) > 0 {
					approval, _ = list[0].(map[string]any)
					workerID = str(approval["worker_id"])
				}
			}
		}
		if approval == nil {
			time.Sleep(3 * time.Second)
		}
	}
	if approval == nil {
		record(t, "worker-prompt-answered-from-chat", "fail", "no decision was ever listed:\n"+e.logTail(10))
		t.Fatal("no approval")
	}
	if workerID == "" {
		if w, ok := e.waitWorker(t, "SweepAsk", time.Second, nil); ok {
			workerID = w.ID
		}
	}
	allow := ""
	if options, ok := approval["options"].([]any); ok {
		for _, option := range options {
			o, _ := option.(map[string]any)
			if id := str(o["id"]); allow == "" && strings.Contains(strings.ToLower(id+str(o["label"])), "allow") {
				allow = id
			}
		}
	}
	requestID := "approve-" + uuid.NewString()[:6]
	c.send(map[string]any{"type": "approval_decision", "request_id": requestID, "worker_id": workerID, "id": str(approval["id"]), "option_id": allow})
	answer, _ := c.waitFor(30*time.Second, func(m map[string]any) bool { return str(m["request_id"]) == requestID })
	created := false
	deadline = time.Now().Add(3 * time.Minute)
	for !created && time.Now().Before(deadline) {
		_, statErr := os.Stat(filepath.Join(project, "approved-by-chat.txt"))
		created = statErr == nil
		if !created {
			time.Sleep(2 * time.Second)
		}
	}
	detail := fmt.Sprintf("decision %q (%s) answered %q -> reply %s; command ran=%v", str(approval["summary"]), str(approval["id"]), allow, str(answer["type"])+" "+str(answer["message"]), created)
	record(t, "worker-prompt-answered-from-chat", map[bool]string{true: "pass", false: "fail"}[created], detail)
	if !created {
		t.Fatal(detail)
	}
}
