package work

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Tool kinds: what a call did, whichever agent made it and whatever its tool
// is called. The apps word and draw a row from these alone.
const (
	ToolKindCommand   = "command"    // ran a shell command
	ToolKindRead      = "read"       // read a file (Target), or a range of it
	ToolKindList      = "list"       // listed a folder
	ToolKindSearch    = "search"     // searched code or files: Target is the pattern, Scope where
	ToolKindEdit      = "edit"       // changed or wrote files: Target, Added, Removed
	ToolKindFetch     = "fetch"      // fetched a URL
	ToolKindWebSearch = "web_search" // searched the web
	ToolKindAgent     = "agent"      // handed work to a subagent
	ToolKindWait      = "wait"       // waited (sleep, a background command's output)
	ToolKindPlan      = "plan"       // updated its plan or todo list
	ToolKindImage     = "image"      // looked at an image
	ToolKindBrowser   = "browser"    // drove a browser
	ToolKindOther     = "other"      // any other tool: Name says which
)

// Tool statuses.
const (
	ToolStatusRunning     = "running"
	ToolStatusDone        = "done"
	ToolStatusFailed      = "failed"
	ToolStatusRefused     = "refused"
	ToolStatusInterrupted = "interrupted"
)

// ConversationTool is one tool call as the apps show it, the same for every
// agent: what kind of thing it did, on what, how it ended, what it found and
// how long it took. It is read from the event the agent's reader made; the
// raw fields stay on the event for the opened row.
type ConversationTool struct {
	Kind string `json:"kind"`
	// Name is the tool's own name made readable ("Get Mcp Tools"), for kinds
	// the apps cannot word alone (other, browser); empty otherwise.
	Name string `json:"name,omitempty"`
	// Server is the MCP server a tool came from.
	Server string `json:"server,omitempty"`
	// Target is what the call worked on: the command line from its first
	// meaningful part, a path, a pattern, a URL, a subagent's task.
	Target string `json:"target,omitempty"`
	// More counts the further calls one call made in parallel (a Codex
	// code-mode cell that ran three commands is Target plus More 2).
	More int `json:"more,omitempty"`
	// Scope is where a search looked (a folder or glob).
	Scope string `json:"scope,omitempty"`
	// Intent is the agent's own words for the call (Claude's Bash
	// description), when it gave some.
	Intent string `json:"intent,omitempty"`
	Status string `json:"status"`
	// Result is a few words on what came back: "exit 2", "12 results",
	// "no results", "lines 1–200", "3 of 7 done".
	Result string `json:"result,omitempty"`
	// Added and Removed are an edit's line counts when they are known.
	Added   *int `json:"added,omitempty"`
	Removed *int `json:"removed,omitempty"`
	// DurationMS is how long the call took, when the agent recorded both its
	// start and its result.
	DurationMS int64 `json:"duration_ms,omitempty"`
}

// settleTool records that a tool call's result came at timestamp: the call
// keeps when it started, and its row's time becomes the result's, as the
// readers have always placed a finished call.
func (event *CodexConversationEvent) settleTool(timestamp string) {
	event.endTool(timestamp)
	if timestamp != "" {
		event.Timestamp = timestamp
	}
}

// endTool records when a tool call's result came, without moving the row.
func (event *CodexConversationEvent) endTool(timestamp string) {
	if timestamp == "" {
		return
	}
	if event.StartedAt == "" {
		event.StartedAt = event.Timestamp
	}
	event.EndedAt = timestamp
}

// AnnotateConversationTool sets event.Tool for a tool call; other events are
// left as they are. Calling it again gives the same answer.
func AnnotateConversationTool(event *CodexConversationEvent) {
	if event.Subagent != nil {
		// A subagent's steps are the subagent's own calls: each is worded the same way.
		subagent := *event.Subagent
		subagent.Steps = append([]CodexConversationEvent(nil), subagent.Steps...)
		for index := range subagent.Steps {
			AnnotateConversationTool(&subagent.Steps[index])
		}
		event.Subagent = &subagent
	}
	tool, ok := conversationToolOf(*event)
	if !ok {
		event.Tool = nil
		return
	}
	event.Tool = &tool
}

func conversationToolOf(event CodexConversationEvent) (ConversationTool, bool) {
	if event.Choice != nil {
		return ConversationTool{}, false
	}
	name := strings.TrimSpace(event.ToolName)
	var tool ConversationTool
	switch event.Kind {
	case "command", "command_execution":
		tool = commandTool(firstNonEmpty(event.Command, commandFromInput(event.Input)))
		if intent := strings.TrimSpace(event.Body); intent != "" && event.Source == claudeConversationSource &&
			intent != strings.TrimSpace(event.Command) && !strings.Contains(intent, "\n") {
			tool.Intent = truncateRunes(intent, 160)
		}
	case "patch":
		tool = ConversationTool{Kind: ToolKindEdit, Target: pathsTarget(event.Files)}
	case "web_search":
		tool = ConversationTool{Kind: ToolKindWebSearch, Target: firstNonEmpty(inputString(event.Input, "query", "q"), toolLine(event.Body), toolLine(event.Title))}
	case "tool", "tool_call":
		if name == "" || name == "tool" {
			// A result whose call the reader never saw: nothing to say what it did.
			return ConversationTool{}, false
		}
		tool = namedTool(name, event)
	default:
		return ConversationTool{}, false
	}
	tool.Status = toolStatus(event)
	toolResult(&tool, event)
	if ms, ok := toolDuration(event); ok {
		tool.DurationMS = ms
	}
	tool.Target = maskSecrets(truncateRunes(tool.Target, 240))
	tool.Scope = maskSecrets(truncateRunes(tool.Scope, 120))
	return tool, true
}

// namedTool reads a call by its tool's name, the same name under any agent's
// spelling: Read, read_file, view; Grep, grep_search, rg; StrReplace,
// search_replace, edit_file; and so on.
func namedTool(rawName string, event CodexConversationEvent) ConversationTool {
	input := event.Input
	name, server := splitToolNamespace(rawName)
	// Grok can name a call by its title: "Execute `ls -la`".
	if command, ok := titledCommand(name); ok {
		return commandTool(command)
	}
	key := toolKey(name)
	switch key {
	case "bash", "shell", "execcommand", "shellcommand", "runterminalcommand", "runshellcommand",
		"terminal", "localshell", "execute", "runcommand", "commandexecution", "unifiedexec":
		command := firstNonEmpty(event.Command, commandFromInput(input))
		if command == "" && key == "execcommand" {
			if calls := codexCodeModeCommands(input); len(calls) > 0 {
				tool := commandTool(calls[0])
				tool.More = len(calls) - 1
				return tool
			}
		}
		return commandTool(command)
	case "read", "readfile", "view", "viewfile", "openfile", "readmanyfiles", "cat":
		return ConversationTool{Kind: ToolKindRead, Target: firstNonEmpty(inputPath(input), pathsTarget(event.Files))}
	case "ls", "listdir", "listdirectory", "listfiles", "list", "tree":
		return ConversationTool{Kind: ToolKindList, Target: firstNonEmpty(inputPath(input), inputString(input, "dir", "directory", "DirectoryPath", "target_directory", "dir_path", "relative_workspace_path"))}
	case "grep", "rg", "search", "grepsearch", "codebasesearch", "searchfiles", "searchcode", "filesearch", "find", "glob", "findfiles", "semanticsearch":
		return ConversationTool{
			Kind:   ToolKindSearch,
			Target: firstNonEmpty(inputString(input, "pattern", "query", "regex", "search_term", "Query", "Pattern", "glob_pattern", "name"), toolLine(event.Title)),
			Scope:  firstNonEmpty(inputString(input, "path", "directory", "dir", "SearchPath", "target_directory", "include", "glob", "include_pattern"), ""),
		}
	case "edit", "multiedit", "write", "applypatch", "strreplace", "strreplacebasededittool", "searchreplace", "editfile",
		"writefile", "createfile", "replace", "deletefile", "notebookedit", "patch", "replaceinfile", "writetofile", "replacefilecontent":
		tool := ConversationTool{Kind: ToolKindEdit, Target: firstNonEmpty(inputPath(input), pathsTarget(event.Files), patchPathsTarget(input))}
		return tool
	case "webfetch", "fetch", "readurl", "fetchurl", "urlfetch", "readurlcontent":
		return ConversationTool{Kind: ToolKindFetch, Target: inputString(input, "url", "URL", "Url", "uri")}
	case "websearch", "googlesearch", "searchweb", "webrun", "web":
		return ConversationTool{Kind: ToolKindWebSearch, Target: firstNonEmpty(inputString(input, "query", "q", "search_query", "search_term"), queryFromWebRun(input))}
	case "task", "agent", "spawnagent", "spawnsubagent", "subagent", "delegate":
		return ConversationTool{Kind: ToolKindAgent, Target: firstNonEmpty(inputString(input, "description", "title", "subagent_type"), toolFirstLine(inputString(input, "prompt", "message")))}
	case "sleep", "wait", "await", "awaitshell", "writestdin", "waitagent", "bashoutput", "taskoutput", "checkcommandstatus", "getcommandstatus":
		return ConversationTool{Kind: ToolKindWait}
	case "todowrite", "updateplan", "todoread", "todo", "plan", "updatetodos":
		return ConversationTool{Kind: ToolKindPlan}
	case "viewimage", "image", "readimage":
		return ConversationTool{Kind: ToolKindImage, Target: firstNonEmpty(inputPath(input), pathsTarget(event.Files))}
	}
	if strings.HasPrefix(key, "browser") || strings.Contains(key, "playwright") || strings.Contains(strings.ToLower(server), "chrome") || strings.Contains(strings.ToLower(server), "browser") {
		return ConversationTool{Kind: ToolKindBrowser, Name: humanToolName(strings.TrimPrefix(name, "browser_")), Server: server,
			Target: firstNonEmpty(inputString(input, "url", "text", "element", "selector", "key"), "")}
	}
	if strings.HasPrefix(name, "multi:") {
		parts := strings.Split(strings.TrimPrefix(name, "multi:"), ",")
		if calls := codexCodeModeCommands(input); len(calls) > 0 {
			tool := commandTool(calls[0])
			tool.More = len(calls) - 1
			if len(parts) > len(calls) {
				tool.More = len(parts) - 1
			}
			return tool
		}
		if len(parts) > 0 {
			tool := namedTool(parts[0], CodexConversationEvent{Input: input})
			tool.More = len(parts) - 1
			return tool
		}
	}
	return ConversationTool{Kind: ToolKindOther, Name: humanToolName(name), Server: server,
		Target: toolLine(firstNonEmpty(inputString(input, "description", "title", "path", "file_path", "url", "query", "name"), ""))}
}

// splitToolNamespace reads "mcp__server__tool", "functions.tool" and
// "server.tool" names into the tool and its MCP server.
func splitToolNamespace(name string) (tool, server string) {
	name = strings.TrimSpace(name)
	if strings.HasPrefix(name, "mcp__") {
		parts := strings.SplitN(strings.TrimPrefix(name, "mcp__"), "__", 2)
		if len(parts) == 2 {
			return parts[1], parts[0]
		}
	}
	if rest, ok := strings.CutPrefix(name, "functions."); ok {
		return rest, ""
	}
	return name, ""
}

func toolKey(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

var titledCommandPattern = regexp.MustCompile("(?s)^(?:Execute|Run|Running|Ran)\\s+`(.+)`\\s*$")

func titledCommand(name string) (string, bool) {
	match := titledCommandPattern.FindStringSubmatch(strings.TrimSpace(name))
	if match == nil {
		return "", false
	}
	return match[1], true
}

func humanToolName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	var words []string
	var current []rune
	flush := func() {
		if len(current) > 0 {
			words = append(words, string(current))
			current = current[:0]
		}
	}
	runes := []rune(name)
	for i, r := range runes {
		switch {
		case r == '_' || r == '-' || r == ' ' || r == '.' || r == ':':
			flush()
		case unicode.IsUpper(r) && i > 0 && (unicode.IsLower(runes[i-1]) || (i+1 < len(runes) && unicode.IsLower(runes[i+1]) && unicode.IsUpper(runes[i-1]))):
			flush()
			current = append(current, r)
		default:
			current = append(current, r)
		}
	}
	flush()
	for i, word := range words {
		if i == 0 {
			r := []rune(word)
			r[0] = unicode.ToUpper(r[0])
			words[i] = string(r)
		} else if strings.ToUpper(word) != word {
			words[i] = strings.ToLower(word)
		}
	}
	return truncateRunes(strings.Join(words, " "), 60)
}

// --- Commands ---------------------------------------------------------------

// commandTool reads a shell command: what it runs, past setup like `cd x &&`
// and `export A=b;`. A command that only reads a file, lists a folder or
// searches (`sed -n 1,80p a.go`, `rg -n foo daemon/`) is that act, as the
// agents' own apps show it; one that does more is a command.
func commandTool(command string) ConversationTool {
	line := commandFirstLine(command)
	segments := shellSegments(line)
	main := -1
	var mains []int
	for index, segment := range segments {
		if segment.afterPipe && isPipeFilter(segment.words) {
			continue
		}
		if isSetupSegment(segment.words) {
			continue
		}
		if main < 0 {
			main = index
		}
		mains = append(mains, index)
	}
	if main < 0 {
		return ConversationTool{Kind: ToolKindCommand, Target: strings.TrimSpace(line)}
	}
	target := strings.TrimSpace(line[segments[main].start:])
	if len(mains) == 1 {
		if tool, ok := commandAct(segments[main].words); ok {
			return tool
		}
	}
	return ConversationTool{Kind: ToolKindCommand, Target: target}
}

// commandFirstLine is the command's first line, with line continuations
// joined: a heredoc's body or a script's later lines are not its name.
func commandFirstLine(command string) string {
	command = strings.ReplaceAll(strings.TrimSpace(command), "\\\n", " ")
	if index := strings.IndexByte(command, '\n'); index >= 0 {
		command = command[:index]
	}
	return strings.TrimSpace(command)
}

type shellSegment struct {
	start     int
	words     []string
	afterPipe bool
}

// shellSegments splits one command line at its top-level ;, &&, || and |,
// respecting quotes and $( ) or ` ` substitutions, into each part's words
// (quotes removed).
func shellSegments(line string) []shellSegment {
	var segments []shellSegment
	current := shellSegment{}
	var word strings.Builder
	inWord := false
	depth := 0
	quote := byte(0)
	flushWord := func() {
		if inWord {
			current.words = append(current.words, word.String())
			word.Reset()
			inWord = false
		}
	}
	flushSegment := func(next int, afterPipe bool) {
		flushWord()
		if len(current.words) > 0 {
			segments = append(segments, current)
		}
		current = shellSegment{start: next, afterPipe: afterPipe}
	}
	for i := 0; i < len(line); i++ {
		c := line[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			} else if c == '\\' && quote == '"' && i+1 < len(line) {
				i++
				word.WriteByte(line[i])
			} else {
				word.WriteByte(c)
			}
			continue
		}
		if depth > 0 {
			word.WriteByte(c)
			switch {
			case c == '(':
				depth++
			case c == ')':
				depth--
			}
			continue
		}
		switch {
		case c == '\'' || c == '"':
			quote = c
			inWord = true
		case c == '\\' && i+1 < len(line):
			i++
			word.WriteByte(line[i])
			inWord = true
		case c == '$' && i+1 < len(line) && line[i+1] == '(':
			word.WriteString("$(")
			i++
			depth = 1
			inWord = true
		case c == ';':
			flushSegment(i+1, false)
		case c == '&' && i+1 < len(line) && line[i+1] == '&':
			flushSegment(i+2, false)
			i++
		case c == '|' && i+1 < len(line) && line[i+1] == '|':
			flushSegment(i+2, false)
			i++
		case c == '|':
			flushSegment(i+1, true)
		case c == '&':
			flushSegment(i+1, false)
		case c == ' ' || c == '\t':
			flushWord()
			if len(current.words) == 0 && !inWord {
				current.start = i + 1
			}
		default:
			word.WriteByte(c)
			inWord = true
		}
	}
	flushSegment(len(line), false)
	return segments
}

var shellAssignment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// commandWords drops what only sets a command up: VAR=x, sudo, env, timeout
// 30, nice -n 19, time, command, exec.
func commandWords(words []string) []string {
	for len(words) > 0 {
		head := words[0]
		switch {
		case shellAssignment.MatchString(head):
			words = words[1:]
		case head == "sudo" || head == "env" || head == "time" || head == "command" || head == "exec" || head == "nohup" || head == "setsid":
			words = words[1:]
		case head == "timeout" && len(words) > 1:
			words = words[2:]
		case head == "nice" && len(words) > 2 && words[1] == "-n":
			words = words[3:]
		default:
			return words
		}
	}
	return words
}

func isSetupSegment(words []string) bool {
	if len(words) == 0 {
		return true
	}
	switch words[0] {
	case "cd", "pushd", "popd", "export", "set", "source", ".", "unset", "umask", "ulimit", "true", ":", "shopt":
		return true
	}
	// A bare assignment (`W=$(cat $TMPDIR/w)`) sets a variable up.
	allAssignments := true
	for _, word := range words {
		if !shellAssignment.MatchString(word) {
			allAssignments = false
			break
		}
	}
	return allAssignments
}

func isPipeFilter(words []string) bool {
	words = commandWords(words)
	if len(words) == 0 {
		return true
	}
	switch words[0] {
	case "head", "tail", "wc", "sort", "uniq", "cut", "tr", "cat", "nl", "less", "more", "column", "grep", "rg", "sed", "awk", "jq", "tee", "xargs", "python3", "python", "fold", "fmt":
		return true
	}
	return false
}

// commandAct reads a one-part command that only reads, lists or searches.
func commandAct(words []string) (ConversationTool, bool) {
	words = commandWords(words)
	if len(words) == 0 {
		return ConversationTool{}, false
	}
	head := words[0]
	if index := strings.LastIndexByte(head, '/'); index >= 0 {
		head = head[index+1:]
	}
	args, redirectOut := plainArgs(words[1:])
	switch head {
	case "cat", "head", "tail", "nl", "less", "more", "bat":
		if redirectOut != "" {
			return ConversationTool{Kind: ToolKindEdit, Target: redirectOut}, true
		}
		paths := pathArgs(args, head == "head" || head == "tail")
		if len(paths) == 0 {
			return ConversationTool{}, false
		}
		return ConversationTool{Kind: ToolKindRead, Target: strings.Join(paths, ", ")}, true
	case "sed":
		if containsWord(words, "-i") || redirectOut != "" {
			return ConversationTool{}, false
		}
		if !containsWord(words, "-n") {
			return ConversationTool{}, false
		}
		// sed -n 1,80p file: the first plain argument is the script.
		plain := pathArgs(args, false)
		if len(plain) < 2 {
			return ConversationTool{}, false
		}
		return ConversationTool{Kind: ToolKindRead, Target: strings.Join(plain[1:], ", "), Result: sedRange(plain[0])}, true
	case "ls", "tree":
		if redirectOut != "" {
			return ConversationTool{}, false
		}
		paths := pathArgs(args, false)
		return ConversationTool{Kind: ToolKindList, Target: firstNonEmpty(strings.Join(paths, ", "), ".")}, true
	case "rg", "grep", "ag", "ack", "egrep", "fgrep":
		if redirectOut != "" {
			return ConversationTool{}, false
		}
		pattern, rest := searchPattern(words[1:])
		if pattern == "" {
			return ConversationTool{}, false
		}
		return ConversationTool{Kind: ToolKindSearch, Target: pattern, Scope: strings.Join(pathArgs(rest, false), ", ")}, true
	case "find", "fd":
		if redirectOut != "" || containsWord(words, "-delete") || containsWord(words, "-exec") {
			return ConversationTool{}, false
		}
		name := ""
		for i := 1; i+1 < len(words); i++ {
			if words[i] == "-name" || words[i] == "-iname" || words[i] == "-path" {
				name = words[i+1]
				break
			}
		}
		scope := ""
		if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
			scope = args[0]
		}
		if head == "fd" && name == "" && len(args) > 0 {
			name, scope = args[0], ""
			if len(args) > 1 {
				scope = args[1]
			}
		}
		return ConversationTool{Kind: ToolKindSearch, Target: firstNonEmpty(name, scope), Scope: map[bool]string{true: scope, false: ""}[name != ""]}, true
	}
	return ConversationTool{}, false
}

// plainArgs drops redirections (2>/dev/null, > out.txt) and says where
// stdout went.
func plainArgs(words []string) (args []string, redirectOut string) {
	for i := 0; i < len(words); i++ {
		word := words[i]
		switch {
		case word == ">" || word == ">>" || word == "1>":
			if i+1 < len(words) {
				redirectOut = words[i+1]
				i++
			}
		case strings.HasPrefix(word, ">") && len(word) > 1 && word[1] != '&':
			redirectOut = strings.TrimLeft(word, ">")
		case word == "2>" || word == "<" || word == "2>>" || word == "&>":
			i++
		case strings.HasPrefix(word, "2>") || strings.HasPrefix(word, "<") || strings.HasPrefix(word, "&>") || word == "2>&1" || word == ">&2":
		case strings.HasPrefix(word, "<<"):
			// A heredoc: the rest of the line is its delimiter.
			return args, redirectOut
		default:
			args = append(args, word)
		}
	}
	return args, redirectOut
}

func pathArgs(args []string, countFlags bool) []string {
	var paths []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			// -n 40, -c 800: the number belongs to the flag.
			if countFlags && (arg == "-n" || arg == "-c") {
				i++
			}
			continue
		}
		paths = append(paths, arg)
	}
	return paths
}

func searchPattern(words []string) (string, []string) {
	var rest []string
	pattern := ""
	for i := 0; i < len(words); i++ {
		word := words[i]
		if strings.HasPrefix(word, ">") || strings.HasPrefix(word, "2>") || word == "2>&1" {
			if word == ">" || word == "2>" {
				i++
			}
			continue
		}
		if strings.HasPrefix(word, "-") {
			switch word {
			case "-e", "--regexp":
				if i+1 < len(words) && pattern == "" {
					pattern = words[i+1]
				}
				i++
			case "-g", "--glob", "-t", "--type", "-A", "-B", "-C", "-m", "--max-count", "--include", "--exclude", "-f", "--max-depth", "-T":
				i++
			}
			continue
		}
		if pattern == "" {
			pattern = word
			continue
		}
		rest = append(rest, word)
	}
	return pattern, rest
}

func containsWord(words []string, want string) bool {
	for _, word := range words {
		if word == want {
			return true
		}
	}
	return false
}

var sedRangePattern = regexp.MustCompile(`^(\d+)(?:,(\d+|\$))?p$`)

func sedRange(script string) string {
	match := sedRangePattern.FindStringSubmatch(strings.TrimSpace(script))
	if match == nil {
		return ""
	}
	if match[2] == "" {
		return "line " + match[1]
	}
	if match[2] == "$" {
		return "from line " + match[1]
	}
	return "lines " + match[1] + "–" + match[2]
}

// codexCodeModeCommands reads the commands a Codex code-mode cell ran:
// `tools.exec_command({cmd: "…"})` calls and the `[label, command]` pairs it
// hands Promise.allSettled. The cell's JavaScript is never run.
func codexCodeModeCommands(input string) []string {
	var commands []string
	for _, match := range codeModeCmdPattern.FindAllStringIndex(input, -1) {
		if value, ok := jsStringAt(input, match[1]); ok && strings.TrimSpace(value) != "" {
			commands = append(commands, value)
		}
	}
	if len(commands) > 0 {
		return commands
	}
	for _, match := range codeModePairPattern.FindAllStringIndex(input, -1) {
		// ["label", "command"]: skip the label to the second string.
		_, end, ok := jsStringSpan(input, match[1]-1)
		if !ok {
			continue
		}
		rest := strings.TrimLeft(input[end:], " \t\n")
		if !strings.HasPrefix(rest, ",") {
			continue
		}
		at := end + strings.Index(input[end:], ",") + 1
		for at < len(input) && (input[at] == ' ' || input[at] == '\n' || input[at] == '\t') {
			at++
		}
		if value, ok := jsStringAt(input, at); ok && strings.TrimSpace(value) != "" {
			commands = append(commands, value)
		}
	}
	return commands
}

var (
	codeModeCmdPattern  = regexp.MustCompile(`\bcmd\s*:\s*`)
	codeModePairPattern = regexp.MustCompile(`\[\s*["'` + "`" + `]`)
)

func jsStringAt(source string, at int) (string, bool) {
	if at >= len(source) {
		return "", false
	}
	start, end, ok := jsStringSpan(source, at)
	if !ok {
		return "", false
	}
	raw := source[start+1 : end-1]
	if source[start] == '`' {
		return raw, true
	}
	var decoded string
	if err := json.Unmarshal([]byte(`"`+strings.ReplaceAll(raw, `\'`, `'`)+`"`), &decoded); err == nil {
		return decoded, true
	}
	return raw, true
}

// jsStringSpan is the quoted literal starting at source[at], quotes included.
func jsStringSpan(source string, at int) (int, int, bool) {
	if at < 0 || at >= len(source) {
		return 0, 0, false
	}
	quote := source[at]
	if quote != '"' && quote != '\'' && quote != '`' {
		return 0, 0, false
	}
	for i := at + 1; i < len(source); i++ {
		switch source[i] {
		case '\\':
			i++
		case quote:
			return at, i + 1, true
		}
	}
	return 0, 0, false
}

// --- Inputs -----------------------------------------------------------------

func inputObject(input string) map[string]any {
	input = strings.TrimSpace(input)
	if !strings.HasPrefix(input, "{") {
		return nil
	}
	var object map[string]any
	if json.Unmarshal([]byte(input), &object) != nil {
		return nil
	}
	return object
}

func inputString(input string, keys ...string) string {
	object := inputObject(input)
	if object == nil {
		return truncatedInputString(input, keys...)
	}
	for _, key := range keys {
		switch value := object[key].(type) {
		case string:
			if strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		case []any:
			var parts []string
			for _, item := range value {
				if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
					parts = append(parts, strings.TrimSpace(text))
				}
			}
			if len(parts) > 0 {
				return strings.Join(parts, ", ")
			}
		}
	}
	return ""
}

// truncatedInputString reads a string field from an input the reader cut
// short (its JSON no longer parses): a long Write keeps its path up front.
func truncatedInputString(input string, keys ...string) string {
	input = strings.TrimSpace(input)
	if !strings.HasPrefix(input, "{") {
		return ""
	}
	for _, key := range keys {
		if match := truncatedKeyPattern(key).FindStringSubmatch(input); match != nil {
			var value string
			if json.Unmarshal([]byte(match[1]), &value) == nil && strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		}
	}
	return ""
}

var truncatedKeyPatterns sync.Map

func truncatedKeyPattern(key string) *regexp.Regexp {
	if pattern, ok := truncatedKeyPatterns.Load(key); ok {
		return pattern.(*regexp.Regexp)
	}
	pattern := regexp.MustCompile(`"` + regexp.QuoteMeta(key) + `"\s*:\s*("(?:\\.|[^"\\])*")`)
	truncatedKeyPatterns.Store(key, pattern)
	return pattern
}

func inputPath(input string) string {
	return inputString(input, "file_path", "path", "filePath", "target_file", "absolute_path", "file", "AbsolutePath", "TargetFile", "notebook_path", "paths", "file_paths", "uri")
}

func commandFromInput(input string) string {
	object := inputObject(input)
	if object == nil {
		return ""
	}
	for _, key := range []string{"command", "cmd", "CommandLine", "commandLine", "script", "command_line"} {
		switch value := object[key].(type) {
		case string:
			if strings.TrimSpace(value) != "" {
				return value
			}
		case []any:
			var parts []string
			for _, item := range value {
				if text, ok := item.(string); ok {
					parts = append(parts, text)
				}
			}
			// ["bash", "-lc", "…"]: the script is the command.
			if len(parts) >= 3 && (parts[1] == "-lc" || parts[1] == "-c") {
				return parts[len(parts)-1]
			}
			if len(parts) > 0 {
				return strings.Join(parts, " ")
			}
		}
	}
	return ""
}

var patchFilePattern = regexp.MustCompile(`(?m)^\*\*\* (?:Update|Add|Delete) File: (.+)$`)

func patchPathsTarget(input string) string {
	text := input
	if object := inputObject(input); object != nil {
		text = firstNonEmpty(inputString(input, "patch", "input", "diff"), input)
	}
	var paths []string
	for _, match := range patchFilePattern.FindAllStringSubmatch(text, -1) {
		paths = append(paths, strings.TrimSpace(match[1]))
	}
	return pathsTarget(paths)
}

func queryFromWebRun(input string) string {
	object := inputObject(input)
	if object == nil {
		return ""
	}
	for _, key := range []string{"search_query", "open", "find"} {
		if list, ok := object[key].([]any); ok && len(list) > 0 {
			if first, ok := list[0].(map[string]any); ok {
				for _, field := range []string{"q", "ref_id", "pattern"} {
					if text, ok := first[field].(string); ok && text != "" {
						return text
					}
				}
			}
		}
	}
	return ""
}

func pathsTarget(paths []string) string {
	var kept []string
	for _, path := range paths {
		if path = strings.TrimSpace(path); path != "" {
			kept = append(kept, path)
		}
	}
	return strings.Join(kept, ", ")
}

func toolLine(value string) string {
	return toolFirstLine(strings.TrimSpace(value))
}

func toolFirstLine(value string) string {
	value = strings.TrimSpace(value)
	if index := strings.IndexByte(value, '\n'); index >= 0 {
		value = value[:index]
	}
	return strings.TrimSpace(value)
}

// --- Status, result, duration -------------------------------------------

func toolStatus(event CodexConversationEvent) string {
	if event.Rejected {
		return ToolStatusRefused
	}
	switch strings.ToLower(strings.TrimSpace(event.Status)) {
	case "running", "in_progress", "pending", "started", "streaming":
		return ToolStatusRunning
	case "failed", "error", "errored":
		return ToolStatusFailed
	case "cancelled", "canceled", "interrupted", "aborted":
		return ToolStatusInterrupted
	}
	if event.ExitCode != nil && *event.ExitCode != 0 {
		return ToolStatusFailed
	}
	return ToolStatusDone
}

func toolOutput(event CodexConversationEvent) string {
	if event.Output != "" {
		return event.Output
	}
	// Codex keeps a command's output in Body; Claude keeps its description there.
	if event.Kind == "command" && event.Source != claudeConversationSource && event.Source != cursorConversationSource && event.Body != event.Command {
		return event.Body
	}
	return ""
}

var (
	foundCountPattern = regexp.MustCompile(`(?i)^\s*Found (\d+) (?:files?|matches|lines?|results?)`)
	noMatchPattern    = regexp.MustCompile(`(?i)^\s*(?:No (?:files|matches|results)(?: found)?|0 matches)\b`)
)

func toolResult(tool *ConversationTool, event CodexConversationEvent) {
	output := strings.TrimSpace(toolOutput(event))
	switch tool.Kind {
	case ToolKindCommand:
		if tool.Status == ToolStatusFailed && event.ExitCode != nil && *event.ExitCode != 0 {
			tool.Result = fmt.Sprintf("exit %d", *event.ExitCode)
		}
	case ToolKindRead:
		if tool.Result != "" {
			return
		}
		offset, limit := inputNumber(event.Input, "offset", "StartLine", "start_line"), inputNumber(event.Input, "limit", "EndLine", "end_line")
		switch {
		case offset > 0 && limit > 0 && strings.Contains(event.Input, "EndLine"):
			tool.Result = fmt.Sprintf("lines %d–%d", offset, limit)
		case offset > 0 && limit > 0:
			tool.Result = fmt.Sprintf("lines %d–%d", offset, offset+limit-1)
		case limit > 0:
			tool.Result = fmt.Sprintf("lines 1–%d", limit)
		case offset > 0:
			tool.Result = fmt.Sprintf("from line %d", offset)
		}
	case ToolKindSearch:
		if tool.Status != ToolStatusDone || output == "" && event.Status == "" {
			return
		}
		switch {
		case noMatchPattern.MatchString(output):
			tool.Result = "no results"
		case foundCountPattern.MatchString(output):
			count, _ := strconv.Atoi(foundCountPattern.FindStringSubmatch(output)[1])
			tool.Result = countWord(count, "result")
		case output != "":
			tool.Result = countWord(countLines(output), "result")
		}
	case ToolKindEdit:
		added, removed, ok := editCounts(event)
		if ok {
			tool.Added, tool.Removed = &added, &removed
		}
	case ToolKindPlan:
		if done, total := planCounts(event); total > 0 {
			tool.Result = fmt.Sprintf("%d of %d done", done, total)
		}
	}
	if tool.Status == ToolStatusFailed && tool.Result == "" && event.ExitCode != nil && *event.ExitCode > 1 {
		tool.Result = fmt.Sprintf("exit %d", *event.ExitCode)
	}
}

func countWord(count int, word string) string {
	if count == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", count, word)
}

func countLines(text string) int {
	count := 0
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

func inputNumber(input string, keys ...string) int {
	object := inputObject(input)
	if object == nil {
		return 0
	}
	for _, key := range keys {
		if value, ok := object[key].(float64); ok && value > 0 {
			return int(value)
		}
	}
	return 0
}

// editCounts are an edit's added and removed lines: the provider's own file
// changes when it gave them, else the call's old and new text.
func editCounts(event CodexConversationEvent) (int, int, bool) {
	if len(event.FileChanges) > 0 {
		added, removed, known := 0, 0, false
		for _, change := range event.FileChanges {
			if change.Additions != nil {
				added += *change.Additions
				known = true
			}
			if change.Deletions != nil {
				removed += *change.Deletions
				known = true
			}
		}
		return added, removed, known
	}
	object := inputObject(event.Input)
	if object == nil {
		return 0, 0, false
	}
	lines := func(value any) int {
		text, ok := value.(string)
		if !ok || text == "" {
			return 0
		}
		return strings.Count(strings.TrimSuffix(text, "\n"), "\n") + 1
	}
	if edits, ok := object["edits"].([]any); ok {
		added, removed := 0, 0
		for _, item := range edits {
			if edit, ok := item.(map[string]any); ok {
				added += lines(firstPresent(edit, "new_string", "newText", "new_str"))
				removed += lines(firstPresent(edit, "old_string", "oldText", "old_str"))
			}
		}
		return added, removed, true
	}
	for _, pair := range [][2]string{{"new_string", "old_string"}, {"newString", "oldString"}, {"newText", "oldText"}, {"new_str", "old_str"}, {"replace", "search"}, {"ReplacementContent", "TargetContent"}} {
		if _, ok := object[pair[0]]; ok {
			return lines(object[pair[0]]), lines(object[pair[1]]), true
		}
	}
	for _, key := range []string{"content", "contents", "file_text", "CodeContent", "text"} {
		if _, ok := object[key]; ok {
			return lines(object[key]), 0, true
		}
	}
	return 0, 0, false
}

func firstPresent(object map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := object[key]; ok {
			return value
		}
	}
	return nil
}

func planCounts(event CodexConversationEvent) (int, int) {
	if len(event.Plan) > 0 {
		done := 0
		for _, step := range event.Plan {
			if step.Status == "completed" {
				done++
			}
		}
		return done, len(event.Plan)
	}
	object := inputObject(event.Input)
	if object == nil {
		return 0, 0
	}
	todos, _ := object["todos"].([]any)
	if todos == nil {
		todos, _ = object["plan"].([]any)
	}
	done := 0
	for _, item := range todos {
		if todo, ok := item.(map[string]any); ok {
			if status, _ := todo["status"].(string); status == "completed" || status == "done" {
				done++
			}
		}
	}
	return done, len(todos)
}

func toolDuration(event CodexConversationEvent) (int64, bool) {
	if event.StartedAt == "" || event.EndedAt == "" {
		return 0, false
	}
	start, err := time.Parse(time.RFC3339Nano, event.StartedAt)
	if err != nil {
		return 0, false
	}
	end, err := time.Parse(time.RFC3339Nano, event.EndedAt)
	if err != nil || end.Before(start) {
		return 0, false
	}
	// A result written days after its call is a resumed conversation, not a
	// call that took days.
	if end.Sub(start) > 24*time.Hour {
		return 0, false
	}
	return end.Sub(start).Milliseconds(), true
}

// --- Secrets ---------------------------------------------------------------

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|sk-[A-Za-z0-9_\-]{16,}|xox[abprs]-[A-Za-z0-9\-]{10,}|AKIA[A-Z0-9]{16}|AIza[A-Za-z0-9_\-]{30,})`),
	regexp.MustCompile(`(?i)\b(Bearer\s+)[A-Za-z0-9._\-~+/=]{12,}`),
	regexp.MustCompile(`(?i)\b((?:api[_-]?key|token|secret|password|passwd|pwd|access[_-]?key)\s*[=:]\s*)(?:"[^"$]{6,}"|'[^'$]{6,}'|[^\s"'$][^\s"']{5,})`),
}

// maskSecrets hides literal credentials in a row's words and keeps the
// rest: `GH_TOKEN=$(gh auth token -u x)` names no secret and stays as it is.
func maskSecrets(value string) string {
	if value == "" {
		return value
	}
	value = secretPatterns[0].ReplaceAllString(value, "•••")
	value = secretPatterns[1].ReplaceAllString(value, "${1}•••")
	value = secretPatterns[2].ReplaceAllString(value, "${1}•••")
	return value
}
