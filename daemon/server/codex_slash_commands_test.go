package server

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestDefaultCodexSlashCommandsContainsRealCommandSet(t *testing.T) {
	commands := defaultCodexSlashCommands("test")
	if len(commands) < 40 {
		t.Fatalf("command count = %d, want at least 40", len(commands))
	}

	values := map[string]bool{}
	for _, command := range commands {
		values[command.Value] = true
		if command.Source != "test" {
			t.Fatalf("command %s source = %q, want test", command.Value, command.Source)
		}
		if command.Category == "" || command.Execution == "" || command.Input.Kind == "" || command.Output.Kind == "" {
			t.Fatalf("command %s missing capability metadata: %#v", command.Value, command)
		}
	}
	for _, value := range []string{"/model", "/permissions", "/diff", "/agent", "/setup-default-sandbox", "/settings"} {
		if !values[value] {
			t.Fatalf("missing command %s", value)
		}
	}
	for _, value := range []string{"/test", "/help", "/sandbox", "/voice", "/theme", "/compact", "/plan", "/status", "/mcp"} {
		if values[value] {
			t.Fatalf("unexpected non-Codex command %s", value)
		}
	}
}

func TestSlashCommandCapabilitiesAreConservative(t *testing.T) {
	commands := defaultCodexSlashCommands("test")
	values := map[string]CodexSlashCommand{}
	for _, command := range commands {
		values[command.Value] = command
	}

	for _, value := range []string{"/copy", "/diff", "/goal"} {
		command := values[value]
		if command.Execution != "terminal-required" || !command.ChatSupported || !command.TerminalSupported {
			t.Fatalf("%s capability = %#v, want chat-supported Codex terminal command", value, command)
		}
	}
	for _, value := range []string{"/status", "/mcp"} {
		if command, ok := values[value]; ok {
			t.Fatalf("%s should be hidden, got %#v", value, command)
		}
	}

	for _, value := range []string{"/model", "/permissions", "/mention", "/skills"} {
		command := values[value]
		if command.Execution != "terminal-required" || command.ChatSupported || !command.TerminalSupported || !command.Interactive {
			t.Fatalf("%s capability = %#v, want interactive terminal-required", value, command)
		}
	}

	if command := values["/review"]; command.Input.Kind != "freeform" || command.Input.Required == nil || *command.Input.Required {
		t.Fatalf("/review input = %#v, want optional freeform", command.Input)
	}
	if command := values["/goal"]; command.Input.Kind != "freeform" || command.Input.Required == nil || *command.Input.Required || command.Interactive {
		t.Fatalf("/goal = %#v, want optional freeform chat-supported terminal command", command)
	}
	if command := values["/rename"]; command.Input.Kind != "freeform" || command.Input.Required == nil || !*command.Input.Required {
		t.Fatalf("/rename input = %#v, want required freeform", command.Input)
	}

	command := values["/debug-m-drop"]
	if command.Execution != "unsupported" || command.ChatSupported || command.TerminalSupported {
		t.Fatalf("/debug-m-drop capability = %#v, want unsupported", command)
	}
}

func TestSlashCommandTitlePreservesKnownInitialisms(t *testing.T) {
	for input, want := range map[string]string{
		"ide":                   "IDE",
		"mcp":                   "MCP",
		"sandbox-add-read-dir":  "Sandbox Add Read Dir",
		"setup-default-sandbox": "Setup Default Sandbox",
	} {
		if got := slashCommandTitle(input); got != want {
			t.Fatalf("slashCommandTitle(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestBundledCodexSlashCommandsServeStaticList(t *testing.T) {
	snapshot := bundledCodexSlashCommands()
	if snapshot.Source != "bundled" {
		t.Fatalf("source = %q, want bundled", snapshot.Source)
	}
	if len(snapshot.Commands) != len(codexSlashCommandSpecs) {
		t.Fatalf("served %d commands, want %d", len(snapshot.Commands), len(codexSlashCommandSpecs))
	}
	values := map[string]CodexSlashCommand{}
	for _, command := range snapshot.Commands {
		values[command.Value] = command
		if command.Source != "bundled" {
			t.Fatalf("command %s source = %q, want bundled", command.Value, command.Source)
		}
		if command.Description == "" {
			t.Fatalf("command %s has no description", command.Value)
		}
	}
	for _, value := range []string{"/model", "/fast", "/review", "/goal", "/rename", "/new", "/clear", "/skills", "/init", "/stop", "/agent", "/debug-m-drop"} {
		if _, ok := values[value]; !ok {
			t.Fatalf("served list is missing %s", value)
		}
	}
}

func TestEveryCodexSlashCommandSpecHasCapability(t *testing.T) {
	for _, spec := range codexSlashCommandSpecs {
		if _, ok := codexSlashCommandCapabilities[spec.name]; !ok {
			t.Fatalf("/%s has no capability entry", spec.name)
		}
	}
	for name := range codexSlashCommandCapabilities {
		found := false
		for _, spec := range codexSlashCommandSpecs {
			if spec.name == name {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("capability entry %q has no spec", name)
		}
	}
}

func TestCodexBinaryScannerIsGone(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	removed := map[string]bool{
		"discoverCodexSlashCommands":         true,
		"discoverCodexSlashCommandsUncached": true,
		"commandsFromDiscovery":              true,
		"parseCodexSlashCommandDiscovery":    true,
		"parseCodexSlashDescriptions":        true,
		"parseCodexFastSlashDescription":     true,
		"parseCodexSlashCommandNames":        true,
		"resolveCodexNativeBinary":           true,
		"newBinaryAddressMapper":             true,
		"maxCodexBinaryScanBytes":            true,
	}
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			ast.Inspect(file, func(node ast.Node) bool {
				if ident, ok := node.(*ast.Ident); ok && removed[ident.Name] && ident.Obj != nil {
					t.Errorf("scanner symbol %s is back at %s", ident.Name, fset.Position(ident.Pos()))
				}
				return true
			})
		}
	}
}
