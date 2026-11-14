package codex

import (
	"strings"
	"testing"
)

func TestInlineTUIStartsCodexInlineUnlessThePersonChoseTheScreen(t *testing.T) {
	for _, tc := range []struct {
		name, command, config, want string
	}{
		{"unset", "codex --dangerously-bypass-approvals-and-sandbox", "", "codex --dangerously-bypass-approvals-and-sandbox --no-alt-screen"},
		{"unset, other tui keys", "codex", "[tui]\nscreen_reader_detection_done = true\n", "codex --no-alt-screen"},
		{"bare program path", "/usr/local/bin/codex -m gpt-6", "", "/usr/local/bin/codex -m gpt-6 --no-alt-screen"},
		{"resume", "codex resume 01a1c34f", "", "codex resume 01a1c34f --no-alt-screen"},
		{"fork", "codex fork 01a1c34f", "", "codex fork 01a1c34f --no-alt-screen"},
		{"config chose the alternate screen", "codex", "[tui]\nalternate_screen = \"always\"\n", "codex"},
		{"config chose inline itself", "codex", "[tui]\nalternate_screen = \"never\"\n", "codex"},
		{"config the person broke", "codex", "[tui\nalternate_screen = ", "codex"},
		{"flag already there", "codex --no-alt-screen", "", "codex --no-alt-screen"},
		{"override on the command", "codex -c tui.alternate_screen=always", "", "codex -c tui.alternate_screen=always"},
		{"override as --config=", "codex --config=tui.alternate_screen=always", "", "codex --config=tui.alternate_screen=always"},
		{"another subcommand", "codex exec 'fix it'", "", "codex exec 'fix it'"},
		{"app server", "codex app-server --listen unix:///x.sock", "", "codex app-server --listen unix:///x.sock"},
		{"environment prefix", "env FOO=1 codex", "", "env FOO=1 codex"},
		{"shell syntax", "codex; rm -rf y", "", "codex; rm -rf y"},
		{"another program", "claude", "", "claude"},
		{"empty", "", "", ""},
	} {
		if got := InlineTUI(tc.command, []byte(tc.config)); got != tc.want {
			t.Errorf("%s: InlineTUI(%q) = %q, want %q", tc.name, tc.command, got, tc.want)
		}
	}
}

func TestPrepareLaunchStartsTheTUIInlineButNotItsAppServer(t *testing.T) {
	a := Adapter{ControlDir: "/state/codex-ctl", Inline: true}
	launch, err := a.PrepareLaunch("codex --dangerously-bypass-approvals-and-sandbox")
	if err != nil {
		t.Fatal(err)
	}
	tui := launch.Command[strings.Index(launch.Command, "exec codex --remote"):]
	server := launch.Command[:strings.Index(launch.Command, "exec codex --remote")]
	if !strings.HasSuffix(tui, " --no-alt-screen") {
		t.Errorf("the TUI is not inline: %s", tui)
	}
	if strings.Contains(server, "--no-alt-screen") {
		t.Errorf("the app-server got a TUI flag: %s", server)
	}
	resumed, _ := a.PrepareLaunch("codex resume abc")
	if resumed.Command != "codex resume abc --no-alt-screen" || resumed.Control != "" {
		t.Errorf("resume = %+v", resumed)
	}
	plain, _ := Adapter{ControlDir: "/state/codex-ctl"}.PrepareLaunch("codex resume abc")
	if plain.Command != "codex resume abc" {
		t.Errorf("without Inline the command changed: %q", plain.Command)
	}
}
