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
