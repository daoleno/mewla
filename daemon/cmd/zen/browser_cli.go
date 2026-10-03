package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/daoleno/zen/daemon/browser"
	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/control"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Local control is owner-UID access, not a sandbox against arbitrary shell.
func (a *controlApp) handleBrowserControl(req control.Request) control.Response {
	if a.browsers == nil || req.BrowserRequest == nil || req.WorkerID == "" || len(req.Client) > 80 {
		return control.ErrorResponse("browser_unavailable", "A managed task and browser attachment are required")
	}
	if _, err := uuid.Parse(req.Client); err != nil {
		return control.ErrorResponse("browser_attachment", "Invalid attachment")
	}
	q := *req.BrowserRequest
	if q.Action != "control" && q.Action != "command" && q.Action != "release" {
		return control.ErrorResponse("browser_denied", "Agents may only attach, act and detach")
	}
	actor := browser.Owner{Kind: "agent", ID: req.WorkerID + "/" + req.Client}
	if q.Action != "release" {
		var active bool
		if a.watcher != nil {
			w := a.watcher.GetWorker(req.WorkerID)
			if w != nil && w.BrowserID != q.ID {
				return control.ErrorResponse("browser_attachment", "This task is not attached to the selected Browser")
			}
			active = w != nil && a.watcher.HasSession(req.WorkerID)
			if active {
				// Quiet/noninteractive provider processes can be classified idle
				// while executing an MCP call. Prove the current owned process,
				// rather than treating terminal paint as task liveness.
				_, err := a.watcher.ResolveOwnedGeneration(req.WorkerID)
				active = err == nil
				if w.Delegated && (w.State == classifier.StateDone || w.State == classifier.StateFailed) {
					active = false
				}
			}
		}
		if !active {
			a.browsers.ReleaseOwner(actor)
			return control.ErrorResponse("browser_task_ended", "Task is no longer active; browser preserved")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	result, err := a.browsers.Handle(ctx, actor, q)
	if err != nil {
		return control.ErrorResponse("browser_denied", err.Error())
	}
	return control.Response{OK: true, Browser: &result}
}

type browserToolInput struct {
	Action string `json:"action" jsonschema:"control, release, snapshot, tabs, select_tab, navigate, new_tab, click, fill, press, dialog_status, dialog_accept or dialog_dismiss"`
	URL    string `json:"url,omitempty"`
	Target string `json:"target,omitempty"`
	Ref    string `json:"ref,omitempty"`
	Text   string `json:"text,omitempty"`
}

func runBrowserCommand(args []string, stderr io.Writer) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		fmt.Fprintln(stderr, "Usage: zen browser mcp --id PROFILE_ID --task WORKER_ID [--state-dir DIR]\nAttach this MCP server to the selected task. Open Browser in Zen and enable Agent access first. Browser/profile outlives this process.")
		return flag.ErrHelp
	}
	if args[0] != "mcp" {
		return errors.New("expected browser mcp")
	}
	fs := flag.NewFlagSet("browser mcp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var cfg cliConfig
	var id, task string
	fs.StringVar(&cfg.stateDir, "state-dir", "", "daemon state directory")
	fs.StringVar(&id, "id", "", "explicit browser resource")
	fs.StringVar(&task, "task", os.Getenv("ZEN_WORKER_ID"), "active Zen Worker ID (provided by managed launch)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if _, err := uuid.Parse(id); err != nil || task == "" || fs.NArg() != 0 {
		return errors.New("explicit --id and --task required")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	client := uuid.NewString()
	var mu sync.Mutex
	var lease *browser.Lease
	call := func(q browser.Request) (*browser.Response, error) {
		q.ID = id
		r, err := callControl(cfg, control.Request{Type: "browser", WorkerID: task, Client: client, BrowserRequest: &q})
		if err != nil {
			return nil, err
		}
		if !r.OK {
			if r.Error != nil {
				return nil, errors.New(r.Error.Message)
			}
			return nil, errors.New("Browser request rejected")
		}
		return r.Browser, nil
	}
	defer func() { _, _ = call(browser.Request{Action: "release"}) }()
	server := mcp.NewServer(&mcp.Implementation{Name: "zen-browser", Version: "1"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "browser", Description: "Use the explicitly selected persistent Zen Browser. Request control before reading or acting. Human control denies ALL managed reads and actions. Never ask for credentials. release detaches this task without closing the browser."}, func(ctx context.Context, req *mcp.CallToolRequest, in browserToolInput) (*mcp.CallToolResult, map[string]any, error) {
		mu.Lock()
		defer mu.Unlock()
		q := browser.Request{Action: "command", Lease: lease}
		switch in.Action {
		case "control", "release":
			q.Action = in.Action
		case "snapshot", "tabs", "select_tab", "navigate", "new_tab", "click", "fill", "press", "dialog_status", "dialog_accept", "dialog_dismiss":
			q.Command = &browser.Command{Kind: in.Action, URL: in.URL, Target: in.Target, Ref: in.Ref, Text: in.Text}
		default:
			return nil, nil, errors.New("unsupported browser action")
		}
		result, err := call(q)
		if err != nil {
			lease = nil
			return nil, nil, err
		}
		if q.Action == "release" {
			lease = nil
		} else if result != nil && result.Lease != nil {
			lease = result.Lease
		}
		raw, err := json.Marshal(result)
		if err != nil {
			return nil, nil, err
		}
		var output map[string]any
		if err = json.Unmarshal(raw, &output); err != nil {
			return nil, nil, err
		}
		return nil, output, nil
	})
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				mu.Lock()
				if lease != nil {
					if _, err := call(browser.Request{Action: "command", Lease: lease, Command: &browser.Command{Kind: "heartbeat"}}); err != nil {
						lease = nil
					}
				}
				mu.Unlock()
			}
		}
	}()
	return server.Run(ctx, &mcp.StdioTransport{})
}
