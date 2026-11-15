package platform

import (
	"strings"
	"testing"
)

// The shims as npm 11 and pnpm 10 write them, CRLF and all (npm's for
// @anthropic-ai/claude-code 2.1.325 copied from a Windows 11 install).
const (
	npmExeShim = "@ECHO off\r\nGOTO start\r\n:find_dp0\r\nSET dp0=%~dp0\r\nEXIT /b\r\n:start\r\nSETLOCAL\r\nCALL :find_dp0\r\n" +
		`"%dp0%\node_modules\@anthropic-ai\claude-code\bin\claude.exe"   %*` + "\r\n"
	npmNodeShim = "@ECHO off\r\nGOTO start\r\n:find_dp0\r\nSET dp0=%~dp0\r\nEXIT /b\r\n:start\r\nSETLOCAL\r\nCALL :find_dp0\r\n\r\n" +
		"IF EXIST \"%dp0%\\node.exe\" (\r\n  SET \"_prog=%dp0%\\node.exe\"\r\n) ELSE (\r\n  SET \"_prog=node\"\r\n  SET PATHEXT=%PATHEXT:;.JS;=;%\r\n)\r\n\r\n" +
		`endLocal & goto #_undefined_# 2>NUL || title %COMSPEC% & "%_prog%"  "%dp0%\node_modules\@openai\codex\bin\codex.js" %*` + "\r\n"
	pnpmNodeShim = "@SETLOCAL\r\n@IF NOT DEFINED NODE_PATH (\r\n  @SET \"NODE_PATH=C:\\pnpm\\global\\5\\node_modules\"\r\n) ELSE (\r\n  @SET \"NODE_PATH=%NODE_PATH%;C:\\pnpm\\global\\5\\node_modules\"\r\n)\r\n" +
		"@IF EXIST \"%~dp0\\node.exe\" (\r\n  \"%~dp0\\node.exe\"  \"%~dp0\\..\\global\\5\\node_modules\\@openai\\codex\\bin\\codex.js\" %*\r\n) ELSE (\r\n  @SET PATHEXT=%PATHEXT:;.JS;=;%\r\n  node  \"%~dp0\\..\\global\\5\\node_modules\\@openai\\codex\\bin\\codex.js\" %*\r\n)\r\n"
)

func existing(paths ...string) func(string) bool {
	return func(p string) bool {
		for _, want := range paths {
			if p == want {
				return true
			}
		}
		return false
	}
}

func TestReadCmdShimFindsWhatNpmAndPnpmShimsRun(t *testing.T) {
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
