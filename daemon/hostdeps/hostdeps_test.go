package hostdeps

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

// writeFakeProgram puts a program LookPath can find in dir: name.exe on
// Windows, an executable script elsewhere. It returns its path.
func writeFakeProgram(t *testing.T, dir, name string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// useHome points the home folder at a new empty one and searches no system
// installer folder, so only what a test creates is found.
func useHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("PATH", t.TempDir())
	t.Cleanup(SetSystemPathDirs())
	return home
}

const ubuntu = "NAME=\"Ubuntu\"\nPRETTY_NAME=\"Ubuntu 24.04.1 LTS\"\nID=ubuntu\nID_LIKE=debian\n"

func TestDetectFromNamesWSL(t *testing.T) {
	p := DetectFrom("linux", ubuntu, "Linux version 5.15-microsoft-standard-WSL2", true)
	if !p.WSL || p.Name() != "Ubuntu 24.04.1 LTS (WSL)" || !p.Headless {
		t.Fatalf("platform = %+v name=%q", p, p.Name())
	}
	if mac := DetectFrom("darwin", "", "", false); mac.Name() != "macOS" || mac.WSL {
		t.Fatalf("mac = %+v", mac)
	}
}

func TestAgentsUseOfficialInstallersOnMacAndLinux(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		agents := Agents(Platform{GOOS: goos})
		if len(agents) != 2 || agents[0].ID != "claude" || agents[1].ID != "codex" {
			t.Fatalf("%s agents = %+v", goos, agents)
		}
		if got := agents[0].Install.Command(); got != "curl -fsSL https://claude.ai/install.sh | bash" {
			t.Fatalf("%s claude install = %q", goos, got)
		}
		if got := agents[1].Install.Command(); got != "curl -fsSL https://chatgpt.com/codex/install.sh | sh" {
			t.Fatalf("%s codex install = %q", goos, got)
		}
	}
	if Agents(Platform{GOOS: "windows"}) != nil {
		t.Fatal("windows has no supported agent plan")
	}
}

func TestAddToPathAppendsOnce(t *testing.T) {
	sep := string(os.PathListSeparator)
	t.Setenv("PATH", "/usr/bin"+sep+"/bin")
	if !AddToPath("/home/u/.local/bin") || AddToPath("/home/u/.local/bin") || AddToPath("/bin") {
		t.Fatal("AddToPath added an existing entry or skipped a new one")
	}
	if got := os.Getenv("PATH"); got != "/usr/bin"+sep+"/bin"+sep+"/home/u/.local/bin" {
		t.Fatalf("PATH = %q, want the new dir last", got)
	}
}

func TestAgentsFollowAgentOrder(t *testing.T) {
	order := AgentOrder()
	if order[0] != "claude" || order[1] != "codex" {
		t.Fatalf("order = %v, want Claude Code first", order)
	}
	order[0] = "changed"
	if AgentOrder()[0] != "claude" {
		t.Fatal("AgentOrder returned its backing list")
	}
	last := -1
	for _, agent := range Agents(Platform{GOOS: "linux"}) {
		rank := AgentRank(agent.ID)
		if rank <= last || rank == len(AgentOrder()) {
			t.Fatalf("install menu %q out of AgentOrder", agent.ID)
		}
		last = rank
	}
}

func TestSystemPathDirsAreSearchedAfterPATHAndCanBeReplaced(t *testing.T) {
	useHome(t)
	dir := t.TempDir()
	program := writeFakeProgram(t, dir, "mewla-fake-agent")
	if _, err := LookPath("mewla-fake-agent"); err == nil {
		t.Fatal("found a tool in no search directory")
	}
	restore := SetSystemPathDirs(dir)
	if path, err := LookPath("mewla-fake-agent"); err != nil || path != program {
		t.Fatalf("a system installer directory is searched: %q %v", path, err)
	}
	restore()
	if _, err := LookPath("mewla-fake-agent"); err == nil {
		t.Fatal("the replaced directories were not put back")
	}
}

func TestLookPathFindsAnAgentInTheNativeInstallersFolder(t *testing.T) {
	home := useHome(t)
	program := writeFakeProgram(t, filepath.Join(home, ".local", "bin"), "mewla-fake-agent")
	if path, err := LookPath("mewla-fake-agent"); err != nil || path != program {
		t.Fatalf("LookPath = %q, %v; want %q (claude.exe on Windows)", path, err, program)
	}
}

func TestAddInstallerDirsToPathAddsAFolderCreatedLater(t *testing.T) {
	home := useHome(t)
	bin := filepath.Join(home, ".local", "bin")
	AddInstallerDirsToPath()
	if slices.Contains(filepath.SplitList(os.Getenv("PATH")), bin) {
		t.Fatal("added a folder that does not exist")
	}
	writeFakeProgram(t, bin, "mewla-fake-agent")
	AddInstallerDirsToPath()
	AddInstallerDirsToPath()
	if got := filepath.SplitList(os.Getenv("PATH")); len(got) != 2 || got[1] != bin {
		t.Fatalf("PATH = %q, want the installer folder added once, last", got)
	}
}
