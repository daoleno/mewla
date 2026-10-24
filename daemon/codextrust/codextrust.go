// Package codextrust spells Codex's per-launch folder trust.
//
// Codex asks whether a folder is trusted before it loads that folder's own
// config and hooks. The answer normally lives in the user's
// ~/.codex/config.toml; a launch can carry it instead as a config override, so
// Mewla never has to write the user's file. The override must be an inline
// table: the dotted form (-c 'projects."<path>".trust_level=...') is accepted
// but not honoured by Codex 0.163 for a path.
//
// When Codex runs as a TUI attached to an app-server of its own (--remote),
// the app-server is the process that loads folder config, and the TUI never
// shows a trust screen. The override therefore has to reach the app-server's
// command line as well, which Forwarded is for.
package codextrust

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/daoleno/mewla/daemon/shellquote"
)

const key = "projects"

// Value is the TOML inline table that marks each directory trusted. Keys are
// JSON strings, which are valid TOML basic strings.
func Value(dirs ...string) string {
	sorted := append([]string(nil), dirs...)
	sort.Strings(sorted)
	var out strings.Builder
	out.WriteString(key + "={")
	for index, dir := range sorted {
		if index > 0 {
			out.WriteString(",")
		}
		name, _ := json.Marshal(dir)
		out.Write(name)
		out.WriteString(`={trust_level="trusted"}`)
	}
	out.WriteString("}")
	return out.String()
}

// Flag is the launch option carrying Value, ready to append to a command line.
func Flag(dirs ...string) string { return flag(Value(dirs...)) }

func flag(value string) string { return "--config " + shellquote.Word(value) }

// Forwarded returns, as launch options to append to an app-server command, the
// trust overrides that a Codex TUI command line already carries. It returns
// "" when there are none, and for a command it cannot split.
func Forwarded(tuiArgs string) string {
	fields, ok := shellquote.Fields(tuiArgs)
	if !ok {
		return ""
	}
	var out strings.Builder
	for index := 0; index+1 < len(fields); index++ {
		if (fields[index] == "--config" || fields[index] == "-c") && strings.HasPrefix(fields[index+1], key+"={") {
			out.WriteString(" " + flag(fields[index+1]))
		}
	}
	return out.String()
}
