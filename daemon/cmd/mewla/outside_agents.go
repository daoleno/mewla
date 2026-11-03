package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/outsideagents"
)

// outsideAgentsBackend is the door's view of Brain: Work to read, and one way
// to write, a Brain turn.
type outsideAgentsBackend struct {
	store   *brain.Store
	service *brain.Service
}

func summarizeWork(item brain.Work) outsideagents.WorkSummary {
	objective := item.Objective
	if runes := []rune(objective); len(runes) > 600 {
		objective = string(runes[:600]) + "…"
	}
	return outsideagents.WorkSummary{
		ID: item.ID, Title: item.Title, Status: string(item.Status), Objective: objective,
		NextAction: item.NextAction, Question: item.Question, UpdatedAt: item.UpdatedAt,
	}
}

func (b outsideAgentsBackend) ListWork() ([]outsideagents.WorkSummary, error) {
	if b.store == nil {
		return nil, errors.New("Brain Work is not configured")
	}
	items, err := b.store.ListWork()
	if err != nil {
		return nil, err
	}
	out := make([]outsideagents.WorkSummary, 0, len(items))
	for _, item := range items {
		out = append(out, summarizeWork(item))
	}
	return out, nil
}

func (b outsideAgentsBackend) Work(id string) (outsideagents.WorkSummary, []outsideagents.WorkEventSummary, error) {
	if b.store == nil {
		return outsideagents.WorkSummary{}, nil, errors.New("Brain Work is not configured")
	}
	item, err := b.store.Work(id)
	if err != nil {
		return outsideagents.WorkSummary{}, nil, fmt.Errorf("no Work %q", id)
	}
	events, err := b.store.ListWorkEvents(id)
	if err != nil {
		return outsideagents.WorkSummary{}, nil, err
	}
	out := make([]outsideagents.WorkEventSummary, 0, len(events))
	for _, event := range events {
		out = append(out, outsideagents.WorkEventSummary{Kind: event.Kind, Summary: event.Summary, At: event.CreatedAt})
	}
	return summarizeWork(item), out, nil
}

func (b outsideAgentsBackend) SubmitToBrain(receipt, body string) (string, error) {
	if b.service == nil {
		return string(brain.ExternalInputNotSubmitted), errors.New("Brain is unavailable")
	}
	disposition, err := b.service.SubmitExternalUserInput(receipt, body)
	return string(disposition), err
}

const agentsUsage = `Usage: mewla agents mcp <connect|list|approve|deny|revoke|requests|reply> [flags]

Let an agent that Mewla did not start, such as Claude Code or Codex in a
terminal, hand work to Brain through MCP. Brain decides; the agent never
reaches a Worker. Each agent is approved once, by name, and can be revoked.

  connect [-name NAME] [-for print|claude|codex]
                       Ask to connect this agent. Approve it in the app
                       (Settings → Outside agents) or with the number shown.
  list                 Approved agents and requests waiting for approval
  approve <number>     Approve a waiting request from this terminal
  deny <number>        Refuse one
  revoke <agent id>    Remove an approved agent; its token stops working
  requests             Hand-offs agents made to Brain
  reply <request id> "<text>" [-work <work id>]
                       What Brain runs to answer an agent

The door is http://127.0.0.1:9876/mcp on this computer. An agent on another
computer uses this computer's remote access address (https only).
`

func runAgentsCommand(args []string, stderr io.Writer) error {
	if len(args) == 0 || isHelpArg(args[0]) || args[0] != "mcp" {
		fmt.Fprint(stderr, agentsUsage)
		if len(args) == 0 || isHelpArg(args[0]) {
			return flag.ErrHelp
		}
		return fmt.Errorf("unknown agents command %q; see mewla agents --help", args[0])
	}
	args = args[1:]
	if len(args) == 0 || isHelpArg(args[0]) {
		fmt.Fprint(stderr, agentsUsage)
		return flag.ErrHelp
	}
	action, rest := args[0], args[1:]
	// The subject (number, id) comes first so flags can follow it.
	subject := ""
	if (action == "approve" || action == "deny" || action == "revoke" || action == "reply") && len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		subject, rest = rest[0], rest[1:]
	}
	fs := flag.NewFlagSet("mewla agents mcp "+action, flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		stateDir, name, target, mcpURL, workID string
		asJSON, noWait                         bool
		wait                                   time.Duration
	)
	fs.StringVar(&stateDir, "state-dir", "", "state directory (default: ~/.mewla)")
	switch action {
	case "connect":
		fs.StringVar(&name, "name", "", "the agent's name as the person will see it (default: Claude Code or Codex from -for)")
		fs.StringVar(&target, "for", "print", "print, claude or codex: how to hand the token to the agent")
		fs.StringVar(&mcpURL, "url", "", "MCP address (default http://127.0.0.1:9876/mcp)")
		fs.BoolVar(&asJSON, "json", false, "print the URL and token as JSON")
		fs.BoolVar(&noWait, "no-wait", false, "return after asking, without waiting for approval")
		fs.DurationVar(&wait, "wait", outsideagents.ApprovalTTL, "how long to wait for approval")
	case "list", "requests":
		fs.BoolVar(&asJSON, "json", false, "print JSON")
	case "reply":
		fs.StringVar(&workID, "work", "", "the Work that carries this request out")
	case "approve", "deny", "revoke":
	default:
		return fmt.Errorf("unknown agents command %q; see mewla agents --help", action)
	}
	// Flags may follow the text of a reply, so parse in rounds and keep the
	// words between them.
	var words []string
	for {
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if fs.NArg() == 0 {
			break
		}
		words = append(words, fs.Arg(0))
		rest = fs.Args()[1:]
	}
	cfg := cliConfig{stateDir: resolveCLIStateDir(stateDir)}
	call := func(req outsideagents.ControlRequest) (outsideagents.ControlResult, error) {
		resp, err := callControl(cfg, control.Request{Type: "agents", AgentsRequest: &req})
		if err != nil {
			return outsideagents.ControlResult{}, err
		}
		if !resp.OK || resp.Agents == nil {
			message := "request failed"
			if resp.Error != nil {
				message = resp.Error.Message
			}
			return outsideagents.ControlResult{}, errors.New(message)
		}
		return *resp.Agents, nil
	}
	out := os.Stdout
	switch action {
	case "connect":
		return agentsConnect(call, out, stderr, name, target, mcpURL, asJSON, noWait, wait)
	case "list":
		pending, err := call(outsideagents.ControlRequest{Action: "pending"})
		if err != nil {
			return err
		}
		agents, err := call(outsideagents.ControlRequest{Action: "agents"})
		if err != nil {
			return err
		}
		if asJSON {
			return json.NewEncoder(out).Encode(map[string]any{"pending": pending.Pendings, "agents": agents.Agents})
		}
		for _, p := range pending.Pendings {
			fmt.Fprintf(out, "waiting\t%s\t%s\tnumber %s\texpires %s\n", p.ID, p.Name, p.Number, p.ExpiresAt.Local().Format("15:04"))
		}
		for _, a := range agents.Agents {
			last := "never used"
			if a.LastUsedAt != nil {
				last = "used " + a.LastUsedAt.Local().Format("Jan 2 15:04")
			}
			fmt.Fprintf(out, "approved\t%s\t%s\t%s\n", a.ID, a.Name, last)
		}
		if len(pending.Pendings) == 0 && len(agents.Agents) == 0 {
			fmt.Fprintln(out, "No outside agents yet. On the agent's computer: mewla agents mcp connect")
		}
	case "approve", "deny":
		req := outsideagents.ControlRequest{Action: action}
		if strings.HasPrefix(subject, "ag_") {
			req.ID = subject
		} else {
			req.Number = subject
		}
		result, err := call(req)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "%s %s\n", map[string]string{"approve": "Approved", "deny": "Denied"}[action], result.Pending.Name)
	case "revoke":
		if subject == "" {
			return errors.New("give the agent id from: mewla agents mcp list")
		}
		if _, err := call(outsideagents.ControlRequest{Action: "revoke", ID: subject}); err != nil {
			return err
		}
		fmt.Fprintln(out, "Revoked. Its token no longer works.")
	case "requests":
		result, err := call(outsideagents.ControlRequest{Action: "requests"})
		if err != nil {
			return err
		}
		if asJSON {
			return json.NewEncoder(out).Encode(result.Requests)
		}
		for _, r := range result.Requests {
			fmt.Fprintf(out, "%s\t%s\t%s\t%s\n", r.ID, r.AgentName, r.Disposition, firstWords(r.Text, 80))
		}
	case "reply":
		text := strings.TrimSpace(strings.Join(words, " "))
		if subject == "" {
			return errors.New("give the request id Brain was sent")
		}
		if _, err := call(outsideagents.ControlRequest{Action: "reply", ID: subject, Text: text, WorkID: workID}); err != nil {
			return err
		}
		fmt.Fprintln(out, "Recorded. The agent sees it next time it checks.")
	}
	return nil
}

func firstWords(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	if runes := []rune(text); len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return text
}

// validMCPURL accepts this computer's own address or an https one: a token
// never goes over plain HTTP across a network.
func validMCPURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("%q is not a URL", raw)
	}
	host := parsed.Hostname()
	ip := net.ParseIP(host)
	switch {
	case parsed.Scheme == "https":
		return nil
	case parsed.Scheme == "http" && (host == "localhost" || (ip != nil && ip.IsLoopback())):
		return nil
	}
	return errors.New("the MCP address must be https, or http on this computer (127.0.0.1)")
}

func agentsConnect(call func(outsideagents.ControlRequest) (outsideagents.ControlResult, error), out, stderr io.Writer, name, target, mcpURL string, asJSON, noWait bool, wait time.Duration) error {
	if mcpURL == "" {
		mcpURL = "http://127.0.0.1:9876/mcp"
	}
	if err := validMCPURL(mcpURL); err != nil {
		return err
	}
	switch target {
	case "print", "claude", "codex":
	default:
		return fmt.Errorf("-for must be print, claude or codex, not %q", target)
	}
	if strings.TrimSpace(name) == "" {
		name = map[string]string{"claude": "Claude Code", "codex": "Codex"}[target]
	}
	if strings.TrimSpace(name) == "" {
		return errors.New("name the agent: -name \"Claude Code\"")
	}
	token, err := outsideagents.NewToken()
	if err != nil {
		return err
	}
	result, err := call(outsideagents.ControlRequest{Action: "connect", Name: name, TokenHash: outsideagents.HashToken(token)})
	if err != nil {
		return err
	}
	pending := result.Pending
	fmt.Fprintf(stderr, "\n“%s” asks to hand work to Brain on this computer.\n", pending.Name)
	fmt.Fprintf(stderr, "  Number: %s\n", pending.Number)
	fmt.Fprintf(stderr, "  Approve it in the Mewla app (Settings → Outside agents), or here: mewla agents mcp approve %s\n", pending.Number)
	fmt.Fprintf(stderr, "  It expires at %s.\n", pending.ExpiresAt.Local().Format("15:04"))
	if noWait {
		return nil
	}
	fmt.Fprintln(stderr, "Waiting for approval… (Ctrl-C to cancel)")
	deadline := time.Now().Add(wait)
	for {
		time.Sleep(time.Second)
		status, err := call(outsideagents.ControlRequest{Action: "status", ID: pending.ID})
		if err != nil {
			return err
		}
		switch status.Pending.Status {
		case outsideagents.StatusApproved:
			return agentsConnected(out, stderr, name, target, mcpURL, token, asJSON)
		case outsideagents.StatusDenied:
			return errors.New("the request was denied")
		case outsideagents.StatusExpired:
			return errors.New("the request expired; run mewla agents mcp connect again")
		}
		if time.Now().After(deadline) {
			return errors.New("timed out waiting for approval")
		}
	}
}

func agentsConnected(out, stderr io.Writer, name, target, mcpURL, token string, asJSON bool) error {
	if asJSON {
		return json.NewEncoder(out).Encode(map[string]any{
			"name": name, "url": mcpURL, "token": token,
			"mcp_config": map[string]any{"mcpServers": map[string]any{"mewla": map[string]any{
				"type": "http", "url": mcpURL, "headers": map[string]string{"Authorization": "Bearer " + token},
			}}},
		})
	}
	fmt.Fprintf(stderr, "Approved. “%s” can now hand work to Brain.\n\n", name)
	switch target {
	case "claude":
		if path, err := exec.LookPath("claude"); err == nil {
			command := exec.Command(path, "mcp", "add", "--transport", "http", "--scope", "user", "mewla", mcpURL, "--header", "Authorization: Bearer "+token)
			command.Stdout, command.Stderr = stderr, stderr
			if err := command.Run(); err != nil {
				return fmt.Errorf("claude mcp add failed: %w (the token below still works)\n  %s", err, token)
			}
			fmt.Fprintln(stderr, "Added to Claude Code as “mewla”. Restart its session to use it.")
			return nil
		}
		fmt.Fprintln(stderr, "claude is not on PATH here; add it where it runs:")
	}
	if target == "codex" {
		fmt.Fprintf(out, "export MEWLA_AGENT_TOKEN=%s\ncodex mcp add mewla --url %s --bearer-token-env-var MEWLA_AGENT_TOKEN\n", token, mcpURL)
		return nil
	}
	fmt.Fprintf(out, "claude mcp add --transport http --scope user mewla %s --header \"Authorization: Bearer %s\"\n", mcpURL, token)
	fmt.Fprintf(stderr, "\nOr for Codex:\n  export MEWLA_AGENT_TOKEN=%s\n  codex mcp add mewla --url %s --bearer-token-env-var MEWLA_AGENT_TOKEN\n", token, mcpURL)
	fmt.Fprintln(stderr, "\nKeep the token private: it lets the agent hand work to Brain until you revoke it.")
	return nil
}
