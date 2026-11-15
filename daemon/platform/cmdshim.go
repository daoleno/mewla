package platform

import (
	"regexp"
	"strings"
)

// CmdShim is what an npm or pnpm .cmd shim runs: Program itself when Node is
// empty, else Node (node.exe beside the shim, or "node" found on PATH when
// NodeOnPath is set) with Script as its first argument.
type CmdShim struct {
	Program    string
	Script     string
	NodeBeside string
	NodeOnPath bool
}

var (
	// The shim's last word runs the target with the caller's arguments.
	shimExeLine = regexp.MustCompile(`^"(%dp0%|%~dp0)(\\[^"%]+)"\s+%\*$`)
	// npm's cmd-shim for a JavaScript bin: "%_prog%" "%dp0%\…\x.js" %*,
	// after `endLocal & goto … & ` on the same line.
	shimNpmNodeLine = regexp.MustCompile(`^endLocal & goto #_undefined_# 2>NUL \|\| title %COMSPEC% & "%_prog%"\s+"%dp0%(\\[^"%]+)"\s+%\*$`)
	// pnpm's: "%~dp0\node.exe" "%~dp0\…\x.js" %*, and node "%~dp0\…\x.js" %*.
	shimPnpmNodeLine = regexp.MustCompile(`^("%~dp0\\node\.exe"|node)\s+"%~dp0(\\[^"%]+)"\s+%\*$`)
)

// ReadCmdShim recognizes the .cmd shims npm and pnpm write for a package's
// bin (dir is the shim's folder). A .cmd runs only through cmd.exe, which
// expands %VAR% and has no escape for a quote inside an argument, so a JSON
// or TOML argument can't pass through one safely; the program the shim names
// can be started directly instead, with the arguments as given. Anything but
// the exact shapes those tools write, or a target that does not exist, is not
// a shim: ok is false and the .cmd stays a .cmd.
func ReadCmdShim(dir, content string, exists func(string) bool) (CmdShim, bool) {
	dir = strings.TrimRight(dir, `\/`)
	var shim CmdShim
	found := false
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "@"))
		if !strings.HasSuffix(line, "%*") {
			continue
		}
		var next CmdShim
		switch {
		case shimExeLine.MatchString(line):
			m := shimExeLine.FindStringSubmatch(line)
			next.Program = dir + m[2]
			if !strings.EqualFold(windowsExt(next.Program), ".exe") {
				return CmdShim{}, false
			}
		case shimNpmNodeLine.MatchString(line):
			next.Script = dir + shimNpmNodeLine.FindStringSubmatch(line)[1]
			next.NodeBeside, next.NodeOnPath = dir+`\node.exe`, true
		case shimPnpmNodeLine.MatchString(line):
			m := shimPnpmNodeLine.FindStringSubmatch(line)
			next.Script = dir + m[2]
			if m[1] == "node" {
				next.NodeOnPath = true
			} else {
				next.NodeBeside = dir + `\node.exe`
			}
		default:
			return CmdShim{}, false
		}
		if found {
			// pnpm's two lines run the same script with either Node.
			if next.Program != "" || shim.Program != "" || next.Script != shim.Script {
				return CmdShim{}, false
			}
			shim.NodeOnPath = shim.NodeOnPath || next.NodeOnPath
			if shim.NodeBeside == "" {
				shim.NodeBeside = next.NodeBeside
			}
			continue
		}
		shim, found = next, true
	}
	if !found {
		return CmdShim{}, false
	}
	target := shim.Program
	if target == "" {
		target = shim.Script
	}
	if !exists(target) {
		return CmdShim{}, false
	}
	if shim.NodeBeside != "" && !exists(shim.NodeBeside) {
		shim.NodeBeside = ""
	}
	if shim.Program == "" && shim.NodeBeside == "" && !shim.NodeOnPath {
		return CmdShim{}, false
	}
	return shim, true
}

func windowsExt(path string) string {
	base := path[strings.LastIndexAny(path, `\/`)+1:]
	if i := strings.LastIndex(base, "."); i >= 0 {
		return base[i:]
	}
	return ""
}
