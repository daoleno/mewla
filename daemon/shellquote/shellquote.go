// Package shellquote renders literal words for POSIX sh command lines.
package shellquote

import "strings"

// Quote always wraps value in single quotes.
func Quote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// wordSpecial holds every byte that sh would split, expand or treat as syntax
// inside an unquoted word. A leading ~ is deliberately absent: executor
// commands are re-rendered field by field and keep their tilde paths.
const wordSpecial = " \t\n\r\"'\\$`|&;()<>*?[]{}!#"

// Word returns value unchanged when sh reads it as one literal word, and
// Quote(value) otherwise. An empty value becomes a quoted empty word, so it
// stays an argument.
func Word(value string) string {
	if value == "" {
		return "''"
	}
	if !strings.ContainsAny(value, wordSpecial) {
		return value
	}
	return Quote(value)
}

// Fields splits a command line that is one simple command: a direct executable
// (optionally behind env assignments) and its arguments. It handles the shell
// quoting needed to recover argv but deliberately rejects shell comments,
// command composition and substitution outside single quotes.
func Fields(command string) ([]string, bool) {
	var fields []string
	var token strings.Builder
	var quote byte
	started := false
	flush := func() {
		if started {
			fields = append(fields, token.String())
			token.Reset()
			started = false
		}
	}
	for index := 0; index < len(command); index++ {
		current := command[index]
		if quote != 0 {
			if current == quote {
				quote = 0
				continue
			}
			if quote == '"' && (current == '`' ||
				current == '$' && index+1 < len(command) && command[index+1] == '(') {
				return nil, false
			}
			if quote == '"' && current == '\\' {
				index++
				if index >= len(command) {
					return nil, false
				}
				current = command[index]
			}
			token.WriteByte(current)
			started = true
			continue
		}
		switch current {
		case '\'', '"':
			quote = current
			started = true
		case '\\':
			index++
			if index >= len(command) {
				return nil, false
			}
			token.WriteByte(command[index])
			started = true
		case '#', ';', '|', '&', '<', '>', '`', '(', ')', '\n', '\r':
			return nil, false
		case ' ', '\t':
			flush()
		default:
			token.WriteByte(current)
			started = true
		}
	}
	if quote != 0 {
		return nil, false
	}
	flush()
	return fields, true
}
