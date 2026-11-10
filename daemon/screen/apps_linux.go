//go:build linux && !screenfake

package screen

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// linuxAppCommand is how to start the installed app a name means: the app
// whose desktop entry has that name (KWrite, Text Editor) or file name
// (org.kde.kwrite), or else a program of that name on the PATH. Field codes
// in the entry's Exec, which would take files or URLs, are dropped: open_app
// starts an app, never a document.
func linuxAppCommand(name string) ([]string, error) {
	if err := checkAppName(name); err != nil {
		return nil, err
	}
	for _, dir := range applicationDirs() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".desktop") {
				continue
			}
			de, err := readDesktopEntry(filepath.Join(dir, entry.Name()))
			if err != nil || de.hidden || de.kind != "Application" || len(de.exec) == 0 {
				continue
			}
			base := strings.TrimSuffix(entry.Name(), ".desktop")
			if de.names(name) || strings.EqualFold(base, name) || strings.EqualFold(filepath.Base(de.exec[0]), name) {
				return de.exec, nil
			}
		}
	}
	for _, candidate := range []string{name, strings.ToLower(name)} {
		if path, err := exec.LookPath(candidate); err == nil {
			return []string{path}, nil
		}
	}
	return nil, fmt.Errorf("no app named %q is installed on this computer", name)
}

// applicationDirs are where desktop entries live, the person's own first.
func applicationDirs() []string {
	home, _ := os.UserHomeDir()
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" && home != "" {
		dataHome = filepath.Join(home, ".local", "share")
	}
	dataDirs := os.Getenv("XDG_DATA_DIRS")
	if dataDirs == "" {
		dataDirs = "/usr/local/share:/usr/share"
	}
	var dirs []string
	if dataHome != "" {
		dirs = append(dirs, filepath.Join(dataHome, "applications"), filepath.Join(dataHome, "flatpak", "exports", "share", "applications"))
	}
	for _, d := range strings.Split(dataDirs, ":") {
		if d != "" {
			dirs = append(dirs, filepath.Join(d, "applications"))
		}
	}
	return append(dirs, "/var/lib/flatpak/exports/share/applications")
}

type desktopEntry struct {
	kind   string
	name   map[string]string // by key: Name, Name[de], GenericName
	exec   []string
	hidden bool
}

func (d desktopEntry) names(want string) bool {
	for _, n := range d.name {
		if strings.EqualFold(strings.TrimSpace(n), want) {
			return true
		}
	}
	return false
}

func readDesktopEntry(path string) (desktopEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return desktopEntry{}, err
	}
	defer f.Close()
	de := desktopEntry{name: map[string]string{}}
	inMain := false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") {
			inMain = line == "[Desktop Entry]"
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !inMain || !ok {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		switch {
		case key == "Type":
			de.kind = value
		case key == "Name" || strings.HasPrefix(key, "Name["):
			de.name[key] = value
		case key == "Exec":
			de.exec, err = parseDesktopExec(value)
			if err != nil {
				return desktopEntry{}, err
			}
		case key == "Hidden" && value == "true":
			de.hidden = true
		}
	}
	return de, scanner.Err()
}

// parseDesktopExec splits an Exec value as the desktop entry spec quotes it,
// leaving out field codes (%f, %U and the rest; %% is a percent sign).
func parseDesktopExec(value string) ([]string, error) {
	var argv []string
	var cur strings.Builder
	have, quoted := false, false
	runes := []rune(value)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case quoted && r == '\\' && i+1 < len(runes):
			i++
			cur.WriteRune(runes[i])
		case r == '"':
			quoted = !quoted
			have = true
		case !quoted && (r == ' ' || r == '\t'):
			if have {
				argv = append(argv, cur.String())
				cur.Reset()
				have = false
			}
		case r == '%' && i+1 < len(runes):
			i++
			if runes[i] == '%' {
				cur.WriteRune('%')
				have = true
			}
		default:
			cur.WriteRune(r)
			have = true
		}
	}
	if quoted {
		return nil, errors.New("unterminated quote in Exec")
	}
	if have {
		argv = append(argv, cur.String())
	}
	return argv, nil
}
