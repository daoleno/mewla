package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
)

// Version is the product version for banners and release ldflags.
// Default must match app/app.base.json expo.version.
// Override: -ldflags "-X main.Version=<semver>"
var Version = "0.2.15"

// versionInfo is the machine-readable shape of `mewla version --json`.
type versionInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	Go      string `json:"go"`
}

// vcsRevision returns the short commit stamped by the Go toolchain, with a
// "-dirty" suffix when the worktree had uncommitted changes at build time.
// It returns an empty string when the build carried no VCS metadata.
func vcsRevision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	revision := ""
	modified := false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = strings.TrimSpace(setting.Value)
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if revision == "" {
		return ""
	}
	const shortRevision = 7
	if len(revision) > shortRevision {
		revision = revision[:shortRevision]
	}
	if modified {
		revision += "-dirty"
	}
	return revision
}

// formatVersionLine renders the one-line human form. The commit part is
// omitted when the build has no VCS metadata.
func formatVersionLine(version, commit, goos, goarch string) string {
	if commit == "" {
		return fmt.Sprintf("mewla %s (%s/%s)", version, goos, goarch)
	}
	return fmt.Sprintf("mewla %s (%s, %s/%s)", version, commit, goos, goarch)
}

func currentVersionInfo() versionInfo {
	return versionInfo{
		Version: Version,
		Commit:  vcsRevision(),
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
		Go:      runtime.Version(),
	}
}

func runVersionCommand(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("mewla version", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := false
	fs.BoolVar(&asJSON, "json", false, "print version information as JSON")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: mewla version [--json]")
		fmt.Fprintln(stderr, "")
		fmt.Fprintln(stderr, "Print the Mewla version, commit and platform.")
		fmt.Fprintln(stderr, "")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	info := currentVersionInfo()
	if asJSON {
		encoded, err := json.Marshal(info)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stdout, "%s\n", encoded)
		return nil
	}
	fmt.Fprintln(os.Stdout, formatVersionLine(info.Version, info.Commit, info.OS, info.Arch))
	return nil
}
