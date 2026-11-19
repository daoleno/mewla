package claude

import "strings"

// TurnCommand reads the command a Claude Bash tool shell runs. Claude (2.1.x)
// runs every Bash call, foreground or run_in_background, as a direct child in
// a session of its own, with one script:
//
//	<shell> -c source <config>/shell-snapshots/snapshot-<shell>-<id>.sh 2>/dev/null || true && … && eval '<command>' < /dev/null && pwd -P >| <cwd file>
//
// Its hooks and MCP servers are children too, so only that form is a command
// of the conversation. The command is the word after the first "&& eval"
// following the snapshot (the preamble has none).
func (Adapter) TurnCommand(args string) (string, bool) {
	return bashToolCommand(args)
}

func bashToolCommand(args string) (string, bool) {
	snapshot := strings.Index(args, "/shell-snapshots/snapshot-")
	if snapshot < 0 || !strings.Contains(args[:snapshot], "source ") {
		return "", false
	}
	rest := args[snapshot:]
	eval := strings.Index(rest, "&& eval ")
	if eval < 0 {
		return "", false
	}
	command, ok := shellWord(rest[eval+len("&& eval "):])
	command = strings.TrimSpace(command)
	if !ok || command == "" {
		return "", false
	}
	return command, true
}

// shellWord reads the one POSIX shell word at the start of s: single quotes
// are literal, double quotes keep \ before " \ $ ` and a newline, and an
// unquoted backslash escapes the next byte. It ends at unquoted whitespace.
func shellWord(s string) (string, bool) {
	var out strings.Builder
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch quote {
		case '\'':
			if c == '\'' {
				quote = 0
			} else {
				out.WriteByte(c)
			}
			continue
		case '"':
			switch {
			case c == '"':
				quote = 0
			case c == '\\' && i+1 < len(s) && strings.IndexByte("\"\\$`\n", s[i+1]) >= 0:
				i++
				out.WriteByte(s[i])
			default:
				out.WriteByte(c)
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case '\\':
			if i+1 >= len(s) {
				return "", false
			}
			i++
			out.WriteByte(s[i])
		case ' ', '\t', '\n':
			return out.String(), true
		default:
			out.WriteByte(c)
		}
	}
	return out.String(), quote == 0
}
