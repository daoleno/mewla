package work

import (
	"encoding/json"
	"strings"

	"github.com/daoleno/mewla/daemon/shellquote"
)

// ShownCommand is a Session's launch command as a person sees it: the command
// Mewla launched, without the hooks Mewla merged into a Claude launch's inline
// --settings JSON (each runs `mewla agent-event`, some 2 KB that say nothing
// about the Session). The launch itself keeps the full command; every other
// command, and settings that bring hooks of their own, are shown as written.
func ShownCommand(command string) string {
	if IsAntigravitySessionCommand(command) {
		return shownAntigravitySession(command)
	}
	if !strings.Contains(command, "agent-event") || !strings.Contains(command, "--settings") {
		return command
	}
	fields, ok := shellquote.Fields(command)
	if !ok {
		return command
	}
	for index := 1; index < len(fields); index++ {
		field := fields[index]
		if field == "--" {
			break
		}
		prefix, raw, valueIndex := "", "", -1
		switch {
		case field == "--settings" && index+1 < len(fields):
			raw, valueIndex = fields[index+1], index+1
		case strings.HasPrefix(field, "--settings="):
			prefix, raw, valueIndex = "--settings=", strings.TrimPrefix(field, "--settings="), index
		default:
			continue
		}
		stripped, changed := withoutMewlaHooks(raw)
		if !changed {
			return command
		}
		shown, ok := shellquote.ReplaceFields(command, map[int]string{valueIndex: prefix + stripped})
		if !ok {
			return command
		}
		return shown
	}
	return command
}

// withoutMewlaHooks drops the hooks key of a settings JSON object when every
// hook in it is Mewla's agent-event hook.
func withoutMewlaHooks(raw string) (string, bool) {
	var settings map[string]json.RawMessage
	if !strings.HasPrefix(strings.TrimSpace(raw), "{") || json.Unmarshal([]byte(raw), &settings) != nil {
		return "", false
	}
	hooksRaw, ok := settings["hooks"]
	if !ok {
		return "", false
	}
	var hooks map[string][]struct {
		Hooks []struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"hooks"`
	}
	if json.Unmarshal(hooksRaw, &hooks) != nil || len(hooks) == 0 {
		return "", false
	}
	for _, matchers := range hooks {
		for _, matcher := range matchers {
			for _, hook := range matcher.Hooks {
				mewla := strings.Contains(hook.Command, " agent-event ")
				for _, arg := range hook.Args {
					if arg == "agent-event" {
						mewla = true
					}
				}
				if !mewla {
					return "", false
				}
			}
		}
	}
	delete(settings, "hooks")
	out, err := json.Marshal(settings)
	if err != nil {
		return "", false
	}
	return string(out), true
}

// shownAntigravitySession is an Antigravity Session as the agy launch it is:
// Mewla's program, its identity, state folder and the Mewla tools it hands
// agy are how Mewla runs agy, not what the person chose.
func shownAntigravitySession(command string) string {
	fields, ok := shellquote.Fields(command)
	if !ok {
		return "agy"
	}
	shown := []string{"agy"}
	for index := 0; index < len(fields); index++ {
		switch field := fields[index]; field {
		case "--model", "--effort", "--mode", "--conversation", "--add-dir":
			if index+1 < len(fields) {
				shown = append(shown, field, shellquote.Word(fields[index+1]))
				index++
			}
		case "--dangerously-skip-permissions":
			shown = append(shown, field)
		}
	}
	return strings.Join(shown, " ")
}
