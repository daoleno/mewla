package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/vdisplay"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const displayToolDescription = `Run desktop apps on a display of your own, off the person's screen, and see and drive them. The person can watch and use it live in Screen, hold it or stop you at any time.

Start with action "open" and command, the program and its arguments (["kcalc"], ["kate", "notes.txt"]). Your display is created then and each answer includes a picture of it; coordinates are that picture's pixels. Then use click, double_click, move, scroll (dy positive scrolls down), type (text), key (such as "Enter", "ctrl+s", "alt+F4") and screenshot. close ends your display and every app on it.

There is no window manager: a window is where it opens, with no title bar, and clicking one does not raise it. Apps run as X11 clients. Prefer a command or a file edit when the task has one; this is for what only an app has.

The person's input does not pause you. If they used your display, the answer tells you how many clicks, scrolls and keys, when and whether it landed during your action, never what they typed. They may have tapped by mistake or entered a password: look at the picture again and judge before you go on. If the answer says they are holding the display, stop and report; do not poll. If they stopped you, do not ask again. Ask the person before anything sent, paid, deleted or changed in an account.`

type displayInput struct {
	Action  string   `json:"action" jsonschema:"open, screenshot, click, double_click, move, scroll, type, key or close"`
	Command []string `json:"command,omitempty" jsonschema:"for open: the program and its arguments, such as [\"kcalc\"]"`
	X       int      `json:"x,omitempty" jsonschema:"pixel in the latest picture, for click, double_click, move and scroll"`
	Y       int      `json:"y,omitempty" jsonschema:"pixel in the latest picture, for click, double_click, move and scroll"`
	Button  string   `json:"button,omitempty" jsonschema:"for click: left (default), middle or right"`
	Text    string   `json:"text,omitempty" jsonschema:"for type: the text to type into the focused field"`
	Key     string   `json:"key,omitempty" jsonschema:"for key: a key or chord such as Enter, Tab, ctrl+a, alt+F4"`
	DX      int      `json:"dx,omitempty" jsonschema:"for scroll: horizontal notches, positive to the right"`
	DY      int      `json:"dy,omitempty" jsonschema:"for scroll: vertical notches, positive down"`
}

// displayOn is whether agents on this computer have displays: the Virtual screen switch
// is on and Xvnc is installed.
func (a *controlApp) displayOn() bool {
	return a.displays != nil && a.browsers != nil && a.browsers.VirtualScreen() && vdisplay.Capability() == nil
}

func (a *controlApp) handleDisplayControl(req control.Request) control.Response {
	if !a.displayOn() {
		return control.ErrorResponse("display_unavailable", "Virtual displays are not turned on for this computer")
	}
	if req.DisplayAction == nil || req.WorkerID == "" {
		return control.ErrorResponse("invalid_request", "A Worker and an action are required")
	}
	if !a.liveWorker(req.WorkerID) {
		return control.ErrorResponse("display_denied", "Only a running Mewla Worker can use a display")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	result, err := a.displays.Act(ctx, req.WorkerID, *req.DisplayAction)
	if err != nil {
		return control.ErrorResponse("display_failed", err.Error())
	}
	return control.Response{OK: true, DisplayResult: &result}
}

// runDisplayMCP is what agents run: their private display as one MCP tool,
// relayed to the daemon over the private control socket.
func runDisplayMCP(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("mewla agent-use display-mcp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var stateDir string
	fs.StringVar(&stateDir, "state-dir", "", "state directory (default: ~/.mewla)")
	task := os.Getenv("MEWLA_WORKER_ID")
	fs.StringVar(&task, "task", task, "Mewla Worker ID (provided by managed launch)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if task == "" {
		return errors.New("a display needs the Mewla Worker it runs in (MEWLA_WORKER_ID)")
	}
	cfg := cliConfig{stateDir: resolveCLIStateDir(stateDir)}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	server := mcp.NewServer(&mcp.Implementation{Name: "mewla-display", Version: "1"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "display", Description: displayToolDescription}, func(_ context.Context, _ *mcp.CallToolRequest, in displayInput) (*mcp.CallToolResult, any, error) {
		socket, err := control.DefaultSocketPath(cfg.stateDir)
		if err != nil {
			return nil, nil, err
		}
		action := vdisplay.Action{Action: in.Action, Command: in.Command, X: in.X, Y: in.Y, Button: in.Button, Text: in.Text, Key: in.Key, DX: in.DX, DY: in.DY}
		resp, err := control.CallWithTimeout(socket, control.Request{Type: "display", WorkerID: task, DisplayAction: &action}, 60*time.Second)
		if err != nil {
			return nil, nil, controlError(cfg.stateDir, err)
		}
		if !resp.OK || resp.DisplayResult == nil {
			message := "display failed"
			if resp.Error != nil {
				message = resp.Error.Message
			}
			return nil, nil, errors.New(message)
		}
		result := &mcp.CallToolResult{}
		if len(resp.DisplayResult.Image) > 0 {
			result.Content = append(result.Content, &mcp.ImageContent{MIMEType: "image/jpeg", Data: resp.DisplayResult.Image})
		}
		if text := resp.DisplayResult.Text; text != "" {
			result.Content = append(result.Content, &mcp.TextContent{Text: text})
		}
		return result, nil, nil
	})
	return server.Run(ctx, &mcp.StdioTransport{})
}
