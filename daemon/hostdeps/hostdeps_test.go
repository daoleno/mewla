package hostdeps

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func lookIn(names ...string) func(string) (string, error) {
	return func(name string) (string, error) {
		for _, have := range names {
			if have == name {
				if strings.Contains(name, "/") {
					return name, nil
				}
				return "/usr/bin/" + name, nil
			}
		}
		return "", exec.ErrNotFound
	}
}

const (
	ubuntu   = "NAME=\"Ubuntu\"\nPRETTY_NAME=\"Ubuntu 24.04.1 LTS\"\nID=ubuntu\nID_LIKE=debian\n"
	debian   = "PRETTY_NAME=\"Debian GNU/Linux 12 (bookworm)\"\nID=debian\n"
	fedora   = "NAME=\"Fedora Linux\"\nID=fedora\n"
	rocky    = "NAME=\"Rocky Linux\"\nID=\"rocky\"\nID_LIKE=\"rhel centos fedora\"\n"
	arch     = "NAME=\"Arch Linux\"\nID=arch\n"
	manjaro  = "NAME=\"Manjaro Linux\"\nID=manjaro\nID_LIKE=arch\n"
	opensuse = "NAME=\"openSUSE Tumbleweed\"\nID=\"opensuse-tumbleweed\"\nID_LIKE=\"opensuse suse\"\n"
	alpine   = "NAME=\"Alpine Linux\"\nID=alpine\n"
	nixos    = "NAME=NixOS\nID=nixos\n"
)

func TestTmuxPlanShowsOnlyThisPlatformsCommand(t *testing.T) {
	cases := []struct {
		name      string
		goos      string
		osRelease string
		proc      string
		root      bool
		have      []string
		want      []string
		doc       string
		blocked   bool
	}{
		{name: "macOS with brew", goos: "darwin", have: []string{"brew", "sudo"}, want: []string{"brew install tmux"}},
		{name: "macOS with brew off PATH", goos: "darwin", have: []string{"/opt/homebrew/bin/brew"}, want: []string{"/opt/homebrew/bin/brew install tmux"}},
		{name: "macOS without brew", goos: "darwin", have: []string{"sudo"}, want: []string{HomebrewInstall, "/opt/homebrew/bin/brew install tmux"}},
		{name: "ubuntu", goos: "linux", osRelease: ubuntu, have: []string{"sudo", "apt-get"}, want: []string{"sudo apt update", "sudo apt install -y tmux"}},
		{name: "debian", goos: "linux", osRelease: debian, have: []string{"sudo"}, want: []string{"sudo apt update", "sudo apt install -y tmux"}},
		{name: "wsl ubuntu", goos: "linux", osRelease: ubuntu, proc: "Linux version 5.15.153.1-microsoft-standard-WSL2", have: []string{"sudo"}, want: []string{"sudo apt update", "sudo apt install -y tmux"}},
		{name: "fedora", goos: "linux", osRelease: fedora, have: []string{"sudo", "dnf"}, want: []string{"sudo dnf install -y tmux"}},
		{name: "rhel-like with dnf", goos: "linux", osRelease: rocky, have: []string{"sudo", "dnf"}, want: []string{"sudo dnf install -y tmux"}},
		{name: "rhel-like without dnf", goos: "linux", osRelease: rocky, have: []string{"sudo"}, want: []string{"sudo yum install -y tmux"}},
		{name: "arch", goos: "linux", osRelease: arch, have: []string{"sudo"}, want: []string{"sudo pacman -S --needed --noconfirm tmux"}},
		{name: "manjaro", goos: "linux", osRelease: manjaro, have: []string{"sudo"}, want: []string{"sudo pacman -S --needed --noconfirm tmux"}},
		{name: "opensuse", goos: "linux", osRelease: opensuse, have: []string{"sudo"}, want: []string{"sudo zypper install -y tmux"}},
		{name: "alpine", goos: "linux", osRelease: alpine, have: []string{"sudo"}, want: []string{"sudo apk add tmux"}},
		{name: "unknown distro with a known manager", goos: "linux", osRelease: "ID=mystery\n", have: []string{"sudo", "zypper"}, want: []string{"sudo zypper install -y tmux"}},
		{name: "unknown distro", goos: "linux", osRelease: nixos, have: []string{"sudo"}, doc: TmuxInstallDoc},
		{name: "root needs no sudo", goos: "linux", osRelease: debian, root: true, want: []string{"apt update", "apt install -y tmux"}},
		{name: "no sudo binary", goos: "linux", osRelease: debian, want: []string{"sudo apt update", "sudo apt install -y tmux"}, blocked: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			uid := 1000
			if tc.root {
				uid = 0
			}
			p := DetectFrom(tc.goos, tc.osRelease, tc.proc, uid, false, lookIn(tc.have...))
			plan := TmuxPlan(p)
			if strings.Join(plan.Steps, "\n") != strings.Join(tc.want, "\n") {
				t.Fatalf("steps = %q, want %q", plan.Steps, tc.want)
			}
			if plan.Doc != tc.doc {
				t.Fatalf("doc = %q, want %q", plan.Doc, tc.doc)
			}
			if (plan.Blocked != "") != tc.blocked {
				t.Fatalf("blocked = %q, want blocked=%v", plan.Blocked, tc.blocked)
			}
			for _, step := range plan.Steps {
				if tc.goos == "darwin" && strings.Contains(step, "apt") {
					t.Fatalf("macOS plan mentions apt: %q", step)
				}
			}
		})
	}
}

func TestDetectFromNamesWSL(t *testing.T) {
	p := DetectFrom("linux", ubuntu, "Linux version 5.15-microsoft-standard-WSL2", 1000, true, lookIn("sudo"))
	if !p.WSL || p.Name() != "Ubuntu 24.04.1 LTS (WSL)" {
		t.Fatalf("platform = %+v name=%q", p, p.Name())
	}
	if mac := DetectFrom("darwin", "", "", 501, false, lookIn()); mac.Name() != "macOS" || mac.Manager != "" {
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
		if agents[0].Login != "claude auth login" || agents[1].Login != "codex login" {
			t.Fatalf("%s logins = %q %q", goos, agents[0].Login, agents[1].Login)
		}
	}
	if got := Agents(Platform{GOOS: "linux", Headless: true})[1].Login; got != "codex login --device-auth" {
		t.Fatalf("headless codex login = %q", got)
	}
	if Agents(Platform{GOOS: "windows"}) != nil {
		t.Fatal("windows has no supported agent plan")
	}
}

func TestAddToPathAppendsOnce(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")
	if !AddToPath("/home/u/.local/bin") || AddToPath("/home/u/.local/bin") || AddToPath("/bin") {
		t.Fatal("AddToPath added an existing entry or skipped a new one")
	}
	if got := os.Getenv("PATH"); got != "/usr/bin:/bin:/home/u/.local/bin" {
		t.Fatalf("PATH = %q, want the new dir last", got)
	}
}
