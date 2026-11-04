package agentuse

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// An agent that wants to act on a busy page needs its controls, not every
// paragraph: Playwright's whole snapshot of a long chat is a third of a
// megabyte. browser_snapshot gets one more argument, `interactive`, handled
// here: the supervisor strips it from the call and, when it is true, reduces
// the answer to the page's controls and headings, each with its ref.

var interactiveRoles = map[string]bool{
	"button": true, "link": true, "textbox": true, "checkbox": true, "radio": true, "combobox": true,
	"searchbox": true, "slider": true, "spinbutton": true, "switch": true, "tab": true, "menuitem": true,
	"menuitemcheckbox": true, "menuitemradio": true, "option": true, "treeitem": true, "listbox": true,
}

var valueRoles = map[string]bool{"textbox": true, "combobox": true, "searchbox": true, "spinbutton": true, "slider": true}

// One line of Playwright's YAML tree: - role "name" [attr] [ref=…]: rest
var snapshotLine = regexp.MustCompile(`^\s*- ([a-z]+)(?: ("(?:[^"\\]|\\.)*"))?((?: \[[^\]]*\])*)(?::(.*))?$`)

var keptAttribute = regexp.MustCompile(`\[(ref=[^\]]*|level=[^\]]*|checked|disabled|expanded|selected|pressed|required|readonly)\]`)

const interactiveSchemaText = "Return only the page's controls (buttons, links, fields, options) and headings, each with its ref, instead of the whole tree. A busy page's whole tree can be hundreds of thousands of tokens."

// compactSnapshot reduces a browser_snapshot answer to controls and headings.
// Text that isn't a snapshot is returned unchanged.
func compactSnapshot(text string) string {
	start := strings.Index(text, "```yaml\n")
	if start < 0 {
		return text
	}
	body := text[start+len("```yaml\n"):]
	end := strings.LastIndex(body, "\n```")
	if end < 0 {
		return text
	}
	rest := body[end:]
	var out []string
	total := 0
	for _, line := range strings.Split(body[:end], "\n") {
		m := snapshotLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		role, name, attrs, tail := m[1], m[2], m[3], strings.TrimSpace(m[4])
		if !strings.Contains(attrs, "[ref=") {
			continue
		}
		total++
		if !interactiveRoles[role] && role != "heading" {
			continue
		}
		var b strings.Builder
		b.WriteString("- " + role)
		if name != "" {
			b.WriteString(" " + name)
		}
		for _, a := range keptAttribute.FindAllString(attrs, -1) {
			b.WriteString(" " + a)
		}
		if valueRoles[role] && tail != "" {
			b.WriteString(": " + tail)
		}
		out = append(out, b.String())
	}
	note := fmt.Sprintf("\n(Controls and headings only: %d of %d elements. Call browser_snapshot without interactive for everything, or with target for one part.)", len(out), total)
	return text[:start] + "```yaml\n" + strings.Join(out, "\n") + rest + note
}

// takeInteractive removes the interactive argument from a tools/call line for
// browser_snapshot and says whether it was true. Other lines are returned as is.
func takeInteractive(line []byte) ([]byte, bool) {
	if !bytes.Contains(line, []byte(`"interactive"`)) {
		return line, false
	}
	var msg map[string]json.RawMessage
	var params map[string]json.RawMessage
	var args map[string]json.RawMessage
	if json.Unmarshal(line, &msg) != nil || json.Unmarshal(msg["params"], &params) != nil || json.Unmarshal(params["arguments"], &args) != nil {
		return line, false
	}
	value, ok := args["interactive"]
	if !ok {
		return line, false
	}
	delete(args, "interactive")
	var err error
	if params["arguments"], err = json.Marshal(args); err != nil {
		return line, false
	}
	if msg["params"], err = json.Marshal(params); err != nil {
		return line, false
	}
	out, err := json.Marshal(msg)
	if err != nil {
		return line, false
	}
	return append(out, '\n'), string(bytes.TrimSpace(value)) == "true"
}

// compactAnswer compacts the text parts of a tools/call answer line.
func compactAnswer(line []byte) []byte {
	var msg map[string]json.RawMessage
	var result map[string]json.RawMessage
	var content []map[string]json.RawMessage
	if json.Unmarshal(line, &msg) != nil || json.Unmarshal(msg["result"], &result) != nil || json.Unmarshal(result["content"], &content) != nil {
		return line
	}
	for _, part := range content {
		var kind, text string
		_ = json.Unmarshal(part["type"], &kind)
		if kind != "text" || json.Unmarshal(part["text"], &text) != nil {
			continue
		}
		part["text"], _ = json.Marshal(compactSnapshot(text))
	}
	var err error
	if result["content"], err = json.Marshal(content); err != nil {
		return line
	}
	if msg["result"], err = json.Marshal(result); err != nil {
		return line
	}
	out, err := json.Marshal(msg)
	if err != nil {
		return line
	}
	return append(out, '\n')
}

// withInteractive adds the interactive argument to a tool's input schema.
func withInteractive(schema json.RawMessage) json.RawMessage {
	var m map[string]any
	if json.Unmarshal(schema, &m) != nil {
		return schema
	}
	props, _ := m["properties"].(map[string]any)
	if props == nil {
		props = map[string]any{}
		m["properties"] = props
	}
	props["interactive"] = map[string]any{"type": "boolean", "description": interactiveSchemaText}
	out, err := json.Marshal(m)
	if err != nil {
		return schema
	}
	return out
}
