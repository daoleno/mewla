package connections

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

//go:embed tailscale_console.js
var tailscaleConsoleScript string

// TailscaleClient is the OAuth client the browser created.
type TailscaleClient struct{ ID, Secret string }

// TailscaleBrowser creates Tailscale's OAuth client in the person's own
// browser, where they are already signed in to the admin console.
type TailscaleBrowser interface {
	// Ready says whether the person's browser can be used now.
	Ready() bool
	// Create makes one client. It returns a *TailscaleBrowserError when the
	// browser could not finish and the person has to do the rest by hand.
	Create(ctx context.Context) (TailscaleClient, error)
	// Probe goes as far as the button that creates the client and reports what
	// the console showed, without pressing it.
	Probe(ctx context.Context) (state string, seen []string, err error)
}

// TailscaleBrowserError says why the browser could not create the client, in
// a line for the person.
type TailscaleBrowserError struct{ Message string }

func (e *TailscaleBrowserError) Error() string { return e.Message }

// chromeTools runs the person's Chrome through the same browser tools agents
// use (mewla agent-use chrome-mcp): Chrome with its logins, in a tab group of
// its own. The script is fixed code; nothing a page says is run or obeyed.
type chromeTools struct {
	ready func() bool
	start func() *exec.Cmd
	// run replaces the MCP session in tests: it receives the script and
	// returns the tool's text.
	run func(ctx context.Context, script string) (string, error)
}

// NewTailscaleBrowser drives Chrome through the command that starts the
// browser tools as an MCP server on stdio.
func NewTailscaleBrowser(ready func() bool, start func() *exec.Cmd) TailscaleBrowser {
	return &chromeTools{ready: ready, start: start}
}

func (c *chromeTools) Ready() bool { return c.ready != nil && c.ready() }

// consoleScope is one row of the console's custom scopes: the section it is
// under, its label there, and whether Mewla ticks Read or Write. Write includes
// Read. Tag: Tailscale asks for a tag with this scope.
type consoleScope struct {
	Section string `json:"section"`
	Label   string `json:"label"`
	Level   string `json:"level"`
	Tag     bool   `json:"tag,omitempty"`
}

func consoleScopes() []consoleScope {
	return []consoleScope{
		{Section: "Devices", Label: "Core", Level: "write", Tag: true},
		{Section: "Devices", Label: "Posture Attributes", Level: "write"},
		{Section: "Keys", Label: "Auth Keys", Level: "write", Tag: true},
		{Section: "General", Label: "Policy File", Level: "write"},
		{Section: "General", Label: "DNS", Level: "write"},
		{Section: "General", Label: "Users", Level: "read"},
		{Section: "General", Label: "Services", Level: "read"},
		{Section: "Devices", Label: "Routes", Level: "read"},
		{Section: "Devices", Label: "Device Invites", Level: "read"},
		{Section: "Keys", Label: "API Access Tokens", Level: "read"},
		{Section: "Keys", Label: "Webhooks", Level: "read"},
		{Section: "Logging", Label: "Log Streaming", Level: "read"},
		{Section: "Logging", Label: "Audit Logs", Level: "read"},
	}
}

// consoleScopeSummary says what the client is given, for the person who
// creates it by hand.
func consoleScopeSummary() string {
	var write, read []string
	for _, s := range consoleScopes() {
		if s.Level == "write" {
			write = append(write, s.Section+" "+s.Label)
		} else {
			read = append(read, s.Section+" "+s.Label)
		}
	}
	return "custom scopes: Write on " + strings.Join(write, ", ") + "; Read on " + strings.Join(read, ", ")
}

type consoleResult struct {
	State  string   `json:"state"`
	Step   string   `json:"step"`
	ID     string   `json:"id"`
	Secret string   `json:"secret"`
	Seen   []string `json:"seen"`
	Page   string   `json:"page"`
}

var consoleOutput = regexp.MustCompile(`MEWLA:([A-Za-z0-9+/=]+)`)

func (c *chromeTools) script(dryRun bool) string {
	config, _ := json.Marshal(map[string]any{"url": TailscaleCredentialsURL, "name": tailscaleClientName, "dryRun": dryRun, "scopes": consoleScopes()})
	return strings.Replace(tailscaleConsoleScript, "__CONFIG__", string(config), 1)
}

func (c *chromeTools) Create(ctx context.Context) (TailscaleClient, error) {
	res, err := c.createClient(ctx, false)
	if err != nil {
		return TailscaleClient{}, err
	}
	return res, nil
}

// Probe goes as far as the button that creates the client and reports what the
// console showed, without pressing it. It is how a change in Tailscale's page
// is told apart from a signed-out browser.
func (c *chromeTools) Probe(ctx context.Context) (state string, seen []string, err error) {
	res, err := c.exec(ctx, true)
	if res.Page != "" {
		seen = append([]string{"page " + res.Page}, res.Seen...)
	} else {
		seen = res.Seen
	}
	return res.State, seen, err
}

func (c *chromeTools) exec(ctx context.Context, dryRun bool) (consoleResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	text, err := c.call(ctx, c.script(dryRun))
	if err != nil {
		return consoleResult{}, &TailscaleBrowserError{"Chrome could not open Tailscale."}
	}
	m := consoleOutput.FindStringSubmatch(text)
	if m == nil {
		return consoleResult{}, &TailscaleBrowserError{"Chrome could not finish in Tailscale."}
	}
	raw, err := base64.StdEncoding.DecodeString(m[1])
	var res consoleResult
	if err != nil || json.Unmarshal(raw, &res) != nil {
		return consoleResult{}, &TailscaleBrowserError{"Chrome could not finish in Tailscale."}
	}
	return res, nil
}

const tailscaleUnreadMessage = "Tailscale made a credential, but Mewla could not read it. Revoke the one named " + tailscaleClientName + " in Trust credentials, then paste a new one."

func (c *chromeTools) createClient(ctx context.Context, dryRun bool) (TailscaleClient, error) {
	res, err := c.exec(ctx, dryRun)
	if err != nil {
		return TailscaleClient{}, err
	}
	switch res.State {
	case "created":
		if !tsClientSecret.MatchString(res.Secret) || !strings.HasPrefix(res.Secret, "tskey-client-"+res.ID+"-") {
			return TailscaleClient{}, &TailscaleBrowserError{tailscaleUnreadMessage}
		}
		return TailscaleClient{ID: res.ID, Secret: res.Secret}, nil
	case "created_unread":
		return TailscaleClient{}, &TailscaleBrowserError{tailscaleUnreadMessage}
	case "signed_out":
		return TailscaleClient{}, &TailscaleBrowserError{"Chrome isn't signed in to Tailscale."}
	case "needs_you":
		return TailscaleClient{}, &TailscaleBrowserError{"Tailscale asked for a check only you can do."}
	case "needs_tag":
		return TailscaleClient{}, &TailscaleBrowserError{"Your tailnet has no tags yet, and device access needs one."}
	}
	return TailscaleClient{}, &TailscaleBrowserError{"Chrome didn't recognise Tailscale's credential page."}
}

// call runs the script in the browser tools and returns the tool's text.
func (c *chromeTools) call(ctx context.Context, script string) (string, error) {
	if c.run != nil {
		return c.run(ctx, script)
	}
	if c.start == nil {
		return "", errors.New("no browser tools")
	}
	cmd := c.start()
	client := mcp.NewClient(&mcp.Implementation{Name: "mewla-connect", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		return "", err
	}
	defer session.Close()
	// The agent's tabs are closed when the session ends.
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "browser_run_code_unsafe", Arguments: map[string]any{"code": script}})
	if err != nil {
		return "", err
	}
	var text strings.Builder
	for _, content := range result.Content {
		if t, ok := content.(*mcp.TextContent); ok {
			text.WriteString(t.Text)
			text.WriteByte('\n')
		}
	}
	if result.IsError {
		return "", errors.New("the browser tools reported an error")
	}
	return text.String(), nil
}
