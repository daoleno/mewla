package ghosttyvt

import (
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// LocalDir turns a program's working directory report (OSC 7) into an
// absolute path on this machine, "" when it names none. A shell reports a
// file URL, file://host/path with the path percent-encoded (macOS's zsh and
// bash, fish, most shell integrations); its host must be this machine,
// localhost or empty, because a shell on another machine (over ssh) reports
// a folder that is not here. Any other scheme is ignored. A bare absolute
// path is kept as it is, and so is a value that was already one, so values
// stored before reports were decoded heal when read through LocalDir.
func LocalDir(report string) string {
	return localDir(report, hostname(), runtime.GOOS == "windows")
}

var hostname = sync.OnceValue(func() string {
	name, _ := os.Hostname()
	return name
})

func localDir(report, host string, windows bool) string {
	report = strings.TrimSpace(report)
	if report == "" {
		return ""
	}
	if isAbsDir(report, windows) {
		return cleanDir(report, windows)
	}
	u, err := url.Parse(report)
	if err != nil || !strings.EqualFold(u.Scheme, "file") || u.Opaque != "" || !sameHost(u.Hostname(), host) {
		return ""
	}
	dir := u.Path
	// file:///C:/Users/x on Windows: the drive follows the URL's slash.
	if windows && len(dir) >= 3 && dir[0] == '/' && dir[2] == ':' && isLetter(dir[1]) {
		dir = dir[1:]
	}
	if !isAbsDir(dir, windows) {
		return ""
	}
	return cleanDir(dir, windows)
}

// sameHost reports whether a file URL's host is this machine: empty,
// localhost, or its name. Names compare without case and by their first
// label too, since a Mac's shell may say Mac.lan or Mac.local for Mac.
func sameHost(urlHost, host string) bool {
	if urlHost == "" || strings.EqualFold(urlHost, "localhost") || urlHost == "127.0.0.1" || urlHost == "::1" {
		return true
	}
	if host == "" {
		return false
	}
	if strings.EqualFold(urlHost, host) {
		return true
	}
	label := func(name string) string { first, _, _ := strings.Cut(name, "."); return first }
	return strings.EqualFold(label(urlHost), label(host))
}

func isAbsDir(dir string, windows bool) bool {
	if windows {
		return len(dir) >= 3 && isLetter(dir[0]) && dir[1] == ':' && (dir[2] == '/' || dir[2] == '\\')
	}
	return strings.HasPrefix(dir, "/")
}

func cleanDir(dir string, windows bool) string {
	if windows {
		return filepath.Clean(strings.ReplaceAll(dir, "/", `\`))
	}
	return path.Clean(dir)
}

func isLetter(b byte) bool { return ('a' <= b && b <= 'z') || ('A' <= b && b <= 'Z') }

// StoredDir heals a directory stored before OSC 7 reports were decoded: a
// file URL becomes its local path ("" when it names none here); any other
// value is kept as it is.
func StoredDir(value string) string {
	if len(value) >= 5 && strings.EqualFold(value[:5], "file:") {
		return LocalDir(value)
	}
	return value
}
