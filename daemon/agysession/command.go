// Package agysession names the command line of an Antigravity Session that
// Mewla starts: `mewla agy-session --agy-session <id> …`, the program that
// holds agy over the Agent Client Protocol (work/antigravity_session.go). It
// is its own package so the watcher, which cannot import work, reads the same
// identity.
package agysession

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/daoleno/mewla/daemon/shellquote"
)

const (
	// Subcommand is the mewla subcommand that runs a Session.
	Subcommand = "agy-session"
	// Flag names the Session's identity.
	Flag = "--agy-session"
)

var idPattern = regexp.MustCompile(`^agy-[a-f0-9-]{36}$`)

// IsID reports a Session id Mewla made.
func IsID(id string) bool { return idPattern.MatchString(id) }

// ID is the Session id on a `mewla agy-session` command line, "" for any
// other command.
func ID(command string) string {
	fields, ok := shellquote.Fields(command)
	if !ok {
		return ""
	}
	seen := false
	for index, field := range fields {
		if field == Subcommand && index > 0 && strings.HasPrefix(strings.ToLower(filepath.Base(fields[index-1])), "mewla") {
			seen = true
			continue
		}
		if !seen {
			continue
		}
		value := ""
		switch {
		case field == Flag && index+1 < len(fields):
			value = fields[index+1]
		case strings.HasPrefix(field, Flag+"="):
			value = strings.TrimPrefix(field, Flag+"=")
		default:
			continue
		}
		if IsID(value) {
			return value
		}
		return ""
	}
	return ""
}

// IsCommand reports a `mewla agy-session` command line.
func IsCommand(command string) bool { return ID(command) != "" }

// Option is the value of one --name option on the command line.
func Option(command, name string) string {
	fields, ok := shellquote.Fields(command)
	if !ok {
		return ""
	}
	for index, field := range fields {
		if field == name && index+1 < len(fields) {
			return fields[index+1]
		}
		if strings.HasPrefix(field, name+"=") {
			return strings.TrimPrefix(field, name+"=")
		}
	}
	return ""
}

// Screen reads the Session program's own terminal: blocked when it shows a
// permission agy waits on ("Allow …?" with numbered answers and the "? "
// prompt), ready when its composer ("› ") is the last line. summary is the
// question, or the last line of the conversation.
func Screen(lines []string) (ready, blocked bool, summary string) {
	var tail []string
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			tail = append(tail, strings.TrimRight(line, " "))
		}
	}
	if len(tail) == 0 {
		return false, false, ""
	}
	last := strings.TrimSpace(tail[len(tail)-1])
	if last == "?" || strings.HasPrefix(last, "? ") {
		for index := len(tail) - 1; index >= 0; index-- {
			line := strings.TrimSpace(tail[index])
			if strings.HasPrefix(line, "Allow ") && strings.HasSuffix(line, "?") {
				return true, true, line
			}
		}
	}
	ready = last == "›" || strings.HasPrefix(last, "› ")
	if !strings.Contains(strings.Join(tail, "\n"), "Antigravity") {
		ready = false
	}
	for index := len(tail) - 1; index >= 0; index-- {
		line := strings.TrimSpace(tail[index])
		if line == "›" || strings.HasPrefix(line, "› ") && index == len(tail)-1 {
			continue
		}
		return ready, false, line
	}
	return ready, false, ""
}
