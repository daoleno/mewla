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
	fields, _, ok := scan(command)
	return fields, ok
}

// ReplaceFields returns command with the fields at the given indexes (as
// Fields numbers them) rewritten to the mapped literal values, leaving every
// other byte of the command exactly as written. It reports false when command
// is not one simple command or an index is out of range.
func ReplaceFields(command string, replace map[int]string) (string, bool) {
	fields, spans, ok := scan(command)
	if !ok {
		return "", false
	}
	for index := range replace {
		if index < 0 || index >= len(fields) {
			return "", false
		}
	}
	var out strings.Builder
	last := 0
	for index, span := range spans {
		value, ok := replace[index]
		if !ok {
			continue
		}
		out.WriteString(command[last:span[0]])
		out.WriteString(Word(value))
		last = span[1]
	}
	out.WriteString(command[last:])
	return out.String(), true
}

// scan splits command like Fields and also returns each field's [start, end)
// byte span in command.
func scan(command string) ([]string, [][2]int, bool) {
	var fields []string
	var spans [][2]int
	var token strings.Builder
	var quote byte
	started := false
	start := 0
	flush := func(end int) {
		if started {
			fields = append(fields, token.String())
			spans = append(spans, [2]int{start, end})
			token.Reset()
			started = false
		}
	}
	begin := func(index int) {
		if !started {
			start = index
			started = true
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
				return nil, nil, false
			}
			if quote == '"' && current == '\\' {
				index++
				if index >= len(command) {
					return nil, nil, false
				}
				current = command[index]
			}
			token.WriteByte(current)
			continue
		}
		switch current {
		case '\'', '"':
			begin(index)
			quote = current
		case '\\':
			begin(index)
			index++
			if index >= len(command) {
				return nil, nil, false
			}
			token.WriteByte(command[index])
		case '#', ';', '|', '&', '<', '>', '`', '(', ')', '\n', '\r':
			return nil, nil, false
		case ' ', '\t':
			flush(index)
		default:
			begin(index)
			token.WriteByte(current)
		}
	}
	if quote != 0 {
		return nil, nil, false
	}
	flush(len(command))
	return fields, spans, true
}
