package server

import "strings"

// codexSlashCommandSource names where the served list comes from. The list is
// shipped with Mewla and refreshed by Mewla releases; it is not read from the
// installed Codex binary.
const codexSlashCommandSource = "bundled"

type CodexSlashCommand struct {
	Value             string                  `json:"value"`
	Name              string                  `json:"name"`
	Title             string                  `json:"title"`
	Description       string                  `json:"description"`
	Source            string                  `json:"source,omitempty"`
	Category          string                  `json:"category"`
	Execution         string                  `json:"execution"`
	Input             CodexSlashCommandInput  `json:"input"`
	Output            CodexSlashCommandOutput `json:"output"`
	Interactive       bool                    `json:"interactive"`
	ChatSupported     bool                    `json:"chat_supported"`
	TerminalSupported bool                    `json:"terminal_supported"`
}

type CodexSlashCommandInput struct {
	Kind        string `json:"kind"`
	Placeholder string `json:"placeholder,omitempty"`
	Picker      string `json:"picker,omitempty"`
	Required    *bool  `json:"required,omitempty"`
}

type CodexSlashCommandOutput struct {
	Kind string `json:"kind"`
}

type CodexSlashCommandSnapshot struct {
	Source   string              `json:"source"`
	Commands []CodexSlashCommand `json:"commands"`
}

type codexSlashCommandSpec struct {
	name        string
	description string
}

type codexSlashCommandCapability struct {
	category          string
	execution         string
	input             CodexSlashCommandInput
	output            CodexSlashCommandOutput
	interactive       bool
	chatSupported     bool
	terminalSupported bool
}

var codexSlashCommandSpecs = []codexSlashCommandSpec{
	{name: "model", description: "choose what model and reasoning effort to use"},
	{name: "fast", description: "1.5x speed, increased usage"},
	{name: "ide", description: "include current selection, open files, and other context from your IDE"},
	{name: "permissions", description: "choose what Codex is allowed to do"},
	{name: "keymap", description: "remap TUI shortcuts"},
	{name: "setup-default-sandbox", description: "set up elevated agent sandbox"},
	{name: "sandbox-add-read-dir", description: "let sandbox read a directory: /sandbox-add-read-dir <absolute_path>"},
	{name: "vim", description: "toggle Vim mode for the composer"},
	{name: "experimental", description: "toggle experimental features"},
	{name: "approve", description: "approve one retry of a recent auto-review denial"},
	{name: "memories", description: "configure memory use and generation"},
	{name: "skills", description: "use skills to improve how Codex performs specific tasks"},
	{name: "hooks", description: "view and manage lifecycle hooks"},
	{name: "review", description: "review my current changes and find issues"},
	{name: "rename", description: "rename the current thread"},
	{name: "new", description: "start a new chat during a conversation"},
	{name: "resume", description: "resume a saved chat"},
	{name: "fork", description: "fork the current chat"},
	{name: "init", description: "create an AGENTS.md file with instructions for Codex"},
	{name: "goal", description: "set or view the goal for a long-running task"},
	{name: "side", description: "start a side conversation in an ephemeral fork"},
	{name: "copy", description: "copy last response as markdown"},
	{name: "raw", description: "toggle raw scrollback mode for copy-friendly terminal selection"},
	{name: "diff", description: "show git diff (including untracked files)"},
	{name: "mention", description: "mention a file"},
	{name: "debug-config", description: "show config layers and requirement sources for debugging"},
	{name: "title", description: "configure which items appear in the terminal title"},
	{name: "statusline", description: "configure which items appear in the status line"},
	{name: "pets", description: "choose or hide the terminal pet"},
	{name: "apps", description: "manage apps"},
	{name: "plugins", description: "browse plugins"},
	{name: "logout", description: "log out of Codex"},
	{name: "quit", description: "exit Codex"},
	{name: "exit", description: "exit Codex"},
	{name: "feedback", description: "send logs to maintainers"},
	{name: "rollout", description: "print the rollout file path"},
	{name: "ps", description: "list background terminals"},
	{name: "stop", description: "stop all background terminals"},
	{name: "clear", description: "clear the terminal and start a new chat"},
	{name: "personality", description: "choose a communication style for Codex"},
	{name: "realtime", description: "toggle realtime voice mode (experimental)"},
	{name: "settings", description: "configure realtime microphone/speaker"},
	{name: "test-approval", description: "test approval request"},
	{name: "agent", description: "switch the active agent thread"},
	{name: "subagents", description: "switch the active agent thread"},
	{name: "btw", description: "start a side conversation in an ephemeral fork"},
	{name: "debug-m-drop", description: "DO NOT USE"},
	{name: "debug-m-update", description: "DO NOT USE"},
}

var codexSlashCommandCapabilities = map[string]codexSlashCommandCapability{
	"model":                 pickerCommand("settings", "model", "management-screen"),
	"fast":                  terminalCommand("settings", inlineArgs("optional speed mode"), "terminal", true),
	"ide":                   terminalCommand("settings", inputNone(), "terminal", true),
	"permissions":           pickerCommand("settings", "permissions", "management-screen"),
	"keymap":                terminalCommand("settings", inputNone(), "terminal", true),
	"setup-default-sandbox": terminalCommand("settings", inputNone(), "terminal", true),
	"sandbox-add-read-dir":  terminalCommand("settings", inlineArgs("<absolute_path>"), "terminal", true),
	"vim":                   terminalCommand("settings", inputNone(), "terminal", true),
	"experimental":          terminalCommand("settings", inputNone(), "terminal", true),
	"approve":               terminalCommand("session", inputNone(), "terminal", false),
	"memories":              terminalCommand("management", inputNone(), "management-screen", true),
	"skills":                terminalCommand("management", inputNone(), "management-screen", true),
	"hooks":                 terminalCommand("management", inputNone(), "management-screen", true),
	"review":                chatTerminalCommand("tools", optionalFreeformInput("optional review instructions"), "terminal", false),
	"rename":                chatTerminalCommand("session", requiredFreeformInput("new thread title"), "terminal", false),
	"new":                   terminalCommand("session", inputNone(), "terminal", true),
	"resume":                terminalCommand("navigation", pickerInput("conversation"), "management-screen", true),
	"fork":                  terminalCommand("navigation", pickerInput("conversation"), "management-screen", true),
	"init":                  terminalCommand("tools", inputNone(), "terminal", false),
	"goal":                  chatTerminalCommand("session", optionalFreeformInput("optional goal text"), "terminal", false),
	"side":                  terminalCommand("navigation", optionalFreeformInput("side conversation prompt"), "terminal", true),
	"copy":                  chatTerminalCommand("tools", inputNone(), "terminal", false),
	"raw":                   terminalCommand("tools", inputNone(), "terminal", true),
	"diff":                  chatTerminalCommand("tools", inputNone(), "terminal", false),
	"mention":               terminalCommand("tools", pickerInput("file"), "terminal", true),
	"debug-config":          terminalCommand("debug", inputNone(), "terminal", false),
	"title":                 terminalCommand("settings", inputNone(), "management-screen", true),
	"statusline":            terminalCommand("settings", inputNone(), "management-screen", true),
	"pets":                  terminalCommand("settings", pickerInput("pet"), "management-screen", true),
	"apps":                  terminalCommand("management", inputNone(), "management-screen", true),
	"plugins":               terminalCommand("management", inputNone(), "management-screen", true),
	"logout":                terminalCommand("danger", inputNone(), "terminal", true),
	"quit":                  terminalCommand("danger", inputNone(), "terminal", true),
	"exit":                  terminalCommand("danger", inputNone(), "terminal", true),
	"feedback":              terminalCommand("management", inputNone(), "terminal", true),
	"rollout":               chatTerminalCommand("tools", inputNone(), "terminal", false),
	"ps":                    chatTerminalCommand("tools", inputNone(), "terminal", false),
	"stop":                  terminalCommand("tools", inputNone(), "terminal", false),
	"clear":                 terminalCommand("session", inputNone(), "terminal", true),
	"personality":           terminalCommand("settings", pickerInput("personality"), "management-screen", true),
	"realtime":              terminalCommand("settings", inputNone(), "terminal", true),
	"settings":              terminalCommand("settings", inputNone(), "management-screen", true),
	"test-approval":         terminalCommand("debug", inputNone(), "terminal", true),
	"agent":                 terminalCommand("navigation", pickerInput("agent"), "management-screen", true),
	"subagents":             terminalCommand("navigation", pickerInput("agent"), "management-screen", true),
	"btw":                   terminalCommand("navigation", optionalFreeformInput("side conversation prompt"), "terminal", true),
	"debug-m-drop":          unsupportedCommand("debug"),
	"debug-m-update":        unsupportedCommand("debug"),
}

// bundledCodexSlashCommands returns the static list the daemon serves for the
// `codex_slash_commands` request.
func bundledCodexSlashCommands() CodexSlashCommandSnapshot {
	return CodexSlashCommandSnapshot{
		Source:   codexSlashCommandSource,
		Commands: defaultCodexSlashCommands(codexSlashCommandSource),
	}
}

func defaultCodexSlashCommands(source string) []CodexSlashCommand {
	commands := make([]CodexSlashCommand, 0, len(codexSlashCommandSpecs))
	for _, spec := range codexSlashCommandSpecs {
		commands = append(commands, codexSlashCommandFromSpec(spec, source))
	}
	return commands
}

func codexSlashCommandFromSpec(spec codexSlashCommandSpec, source string) CodexSlashCommand {
	capability := codexSlashCommandCapabilities[spec.name].withDefaults()
	return CodexSlashCommand{
		Value:             "/" + spec.name,
		Name:              spec.name,
		Title:             slashCommandTitle(spec.name),
		Description:       spec.description,
		Source:            source,
		Category:          capability.category,
		Execution:         capability.execution,
		Input:             capability.input,
		Output:            capability.output,
		Interactive:       capability.interactive,
		ChatSupported:     capability.chatSupported,
		TerminalSupported: capability.terminalSupported,
	}
}

func (capability codexSlashCommandCapability) withDefaults() codexSlashCommandCapability {
	if capability.category == "" {
		capability.category = "unknown"
	}
	if capability.execution == "" {
		capability.execution = "terminal-required"
	}
	if capability.input.Kind == "" {
		capability.input.Kind = "none"
	}
	if capability.output.Kind == "" {
		capability.output.Kind = "terminal"
	}
	if capability.execution != "unsupported" {
		capability.terminalSupported = true
	}
	return capability
}

func terminalCommand(category string, input CodexSlashCommandInput, outputKind string, interactive bool) codexSlashCommandCapability {
	return codexSlashCommandCapability{
		category:          category,
		execution:         "terminal-required",
		input:             input,
		output:            CodexSlashCommandOutput{Kind: outputKind},
		interactive:       interactive,
		chatSupported:     false,
		terminalSupported: true,
	}
}

func chatTerminalCommand(category string, input CodexSlashCommandInput, outputKind string, interactive bool) codexSlashCommandCapability {
	capability := terminalCommand(category, input, outputKind, interactive)
	capability.chatSupported = true
	return capability
}

func pickerCommand(category, picker, outputKind string) codexSlashCommandCapability {
	return terminalCommand(category, pickerInput(picker), outputKind, true)
}

func unsupportedCommand(category string) codexSlashCommandCapability {
	return codexSlashCommandCapability{
		category:          category,
		execution:         "unsupported",
		input:             inputNone(),
		output:            CodexSlashCommandOutput{Kind: "terminal"},
		interactive:       true,
		chatSupported:     false,
		terminalSupported: false,
	}
}

func inputNone() CodexSlashCommandInput {
	return CodexSlashCommandInput{Kind: "none"}
}

func inlineArgs(placeholder string) CodexSlashCommandInput {
	return CodexSlashCommandInput{Kind: "inline-args", Placeholder: placeholder}
}

func optionalFreeformInput(placeholder string) CodexSlashCommandInput {
	required := false
	return CodexSlashCommandInput{Kind: "freeform", Placeholder: placeholder, Required: &required}
}

func requiredFreeformInput(placeholder string) CodexSlashCommandInput {
	required := true
	return CodexSlashCommandInput{Kind: "freeform", Placeholder: placeholder, Required: &required}
}

func pickerInput(picker string) CodexSlashCommandInput {
	return CodexSlashCommandInput{Kind: "picker", Picker: picker}
}

func slashCommandTitle(name string) string {
	parts := strings.Split(name, "-")
	for i, part := range parts {
		if part == "" {
			continue
		}
		switch strings.ToLower(part) {
		case "ide":
			parts[i] = "IDE"
		case "mcp":
			parts[i] = "MCP"
		default:
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return strings.Join(parts, " ")
}
