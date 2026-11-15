package platform

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// pnpm 10's shim for a JavaScript bin (npm 11's own, as installed by
// `npm i -g` on Windows 11, are in testdata/npm-11).
const pnpmNodeShim = "@SETLOCAL\r\n@IF NOT DEFINED NODE_PATH (\r\n  @SET \"NODE_PATH=C:\\pnpm\\global\\5\\node_modules\"\r\n) ELSE (\r\n  @SET \"NODE_PATH=%NODE_PATH%;C:\\pnpm\\global\\5\\node_modules\"\r\n)\r\n" +
	"@IF EXIST \"%~dp0\\node.exe\" (\r\n  \"%~dp0\\node.exe\"  \"%~dp0\\..\\global\\5\\node_modules\\@openai\\codex\\bin\\codex.js\" %*\r\n) ELSE (\r\n  @SET PATHEXT=%PATHEXT:;.JS;=;%\r\n  node  \"%~dp0\\..\\global\\5\\node_modules\\@openai\\codex\\bin\\codex.js\" %*\r\n)\r\n"

func readShim(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "npm-11", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func existing(paths ...string) func(string) bool {
	return func(p string) bool { return slices.Contains(paths, p) }
}

func TestReadCmdShimFindsWhatNpmAndPnpmShimsRun(t *testing.T) {
	npmExeShim, npmNodeShim := readShim(t, "claude.cmd"), readShim(t, "codex.cmd")
	npm := `C:\Users\u\AppData\Roaming\npm`
	claude := npm + `\node_modules\@anthropic-ai\claude-code\bin\claude.exe`
	if shim, ok := ReadCmdShim(npm+`\`, npmExeShim, existing(claude)); !ok || shim != (CmdShim{Program: claude}) {
		t.Fatalf("npm exe shim = %+v, %v", shim, ok)
	}
	codex := npm + `\node_modules\@openai\codex\bin\codex.js`
	if shim, ok := ReadCmdShim(npm, npmNodeShim, existing(codex)); !ok || shim != (CmdShim{Script: codex, NodeOnPath: true}) {
		t.Fatalf("npm node shim = %+v, %v", shim, ok)
	}
	if shim, ok := ReadCmdShim(npm, npmNodeShim, existing(codex, npm+`\node.exe`)); !ok || shim.NodeBeside != npm+`\node.exe` {
		t.Fatalf("npm node shim beside node.exe = %+v, %v", shim, ok)
	}
	pnpm := `C:\pnpm`
	script := pnpm + `\..\global\5\node_modules\@openai\codex\bin\codex.js`
	if shim, ok := ReadCmdShim(pnpm, pnpmNodeShim, existing(script)); !ok || shim != (CmdShim{Script: script, NodeOnPath: true}) {
		t.Fatalf("pnpm shim = %+v, %v", shim, ok)
	}
}

// Anything but those exact shapes stays a .cmd, run through cmd.exe with its
// argument checks: a shim that does more than run one program, or names one
// that is not there, is not read as one.
func TestReadCmdShimRefusesAnythingElse(t *testing.T) {
	npmExeShim := readShim(t, "claude.cmd")
	npm := `C:\npm`
	claude := npm + `\node_modules\@anthropic-ai\claude-code\bin\claude.exe`
	for name, content := range map[string]string{
		"a plain batch file":     "@echo shim %*\r\n",
		"a second command":       npmExeShim + "del /q C:\\x %*\r\n",
		"chained on the line":    strings.Replace(npmExeShim, `"   %*`, `"   %* & calc`, 1),
		"an env var in the path": strings.Replace(npmExeShim, `\bin\claude.exe`, `\%BIN%\claude.exe`, 1),
		"not an exe":             strings.Replace(npmExeShim, `claude.exe`, `claude.bat`, 1),
		"an absolute target":     "@\"C:\\Windows\\System32\\calc.exe\" %*\r\n",
		"no arguments passed on": strings.Replace(npmExeShim, `   %*`, ``, 1),
	} {
		if shim, ok := ReadCmdShim(npm, content, existing(claude, `C:\Windows\System32\calc.exe`)); ok {
			t.Errorf("%s: read as a shim for %+v", name, shim)
		}
	}
	if shim, ok := ReadCmdShim(npm, npmExeShim, existing()); ok {
		t.Errorf("a shim whose program is gone: %+v", shim)
	}
}
