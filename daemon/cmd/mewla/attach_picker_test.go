package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/sessionhost"
)

var attachNow = time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

func hostInfo(id, name, executor string, age time.Duration, state string, labels map[string]string) sessionhost.Info {
	info := sessionhost.Info{
		ID: id, Alive: true, Cwd: "/work/" + name, Command: "zsh",
		CreatedAt: attachNow.Add(-age),
		Labels:    map[string]string{"name": name},
	}
	if executor != "" {
		info.Labels["executor"] = executor
	}
	for k, v := range labels {
		info.Labels[k] = v
	}
	if state != "" {
		info.Status = []sessionhost.ProgramStatus{{State: state, UpdatedAt: attachNow}}
	}
	return info
}

func testAttachEntries() []attachEntry {
	infos := []sessionhost.Info{
		hostInfo("%2", "Brain", "claude", 10*time.Hour, "working", map[string]string{"hidden": "1"}),
		hostInfo("mh-aaa", "KairoWebPreview", "claude", 20*time.Minute, "working", nil),
		hostInfo("mh-bbb", "Kairo021", "claude", 3*time.Hour, "idle", nil),
		hostInfo("mh-ccc", "", "", 5*time.Minute, "", nil),
		hostInfo("mh-ddd", "Docs", "codex", 50*time.Hour, "blocked", nil),
		hostInfo("mh-eee", "Hidden helper", "claude", time.Minute, "working", map[string]string{"hidden": "1"}),
	}
	ended := hostInfo("mh-old", "Docs", "codex", 60*time.Hour, "idle", nil)
	ended.Alive = false
	infos = append(infos, ended)
	return hostAttachEntries(infos, nil)
}

func TestHostAttachEntriesDescribeSessions(t *testing.T) {
	byID := map[string]attachEntry{}
	for _, entry := range testAttachEntries() {
		byID[entry.ID] = entry
	}
	if e := byID["%2"]; !e.Brain || e.Name != "Brain" || e.State != "working" || !e.listed() {
		t.Fatalf("Brain entry: %+v", e)
	}
	if e := byID["mh-ccc"]; e.Name != "Shell" || e.Agent != "zsh" || e.State != "running" {
		t.Fatalf("unnamed Session: %+v", e)
	}
	if e := byID["mh-ddd"]; e.State != "needs input" || e.Agent != "codex" {
		t.Fatalf("blocked Session: %+v", e)
	}
	if e := byID["mh-bbb"]; e.State != "idle" {
		t.Fatalf("idle Session: %+v", e)
	}
	if byID["mh-eee"].listed() {
		t.Fatalf("a hidden helper that is not Brain must not be listed")
	}
	if e := byID["mh-old"]; e.listed() || e.State != "exited" {
		t.Fatalf("ended Session: %+v", e)
	}
}

func TestDaemonPromptMakesNeedsInput(t *testing.T) {
	entries := hostAttachEntries([]sessionhost.Info{hostInfo("mh-aaa", "W", "claude", time.Minute, "working", nil)}, map[string]bool{"mh-aaa": true})
	if entries[0].State != "needs input" {
		t.Fatalf("state %q", entries[0].State)
	}
}

func TestListedAttachEntriesNewestFirst(t *testing.T) {
	var ids []string
	for _, entry := range listedAttachEntries(testAttachEntries()) {
		ids = append(ids, entry.ID)
	}
	want := []string{"mh-ccc", "mh-aaa", "mh-bbb", "%2", "mh-ddd"}
	if strings.Join(ids, " ") != strings.Join(want, " ") {
		t.Fatalf("order %v, want %v", ids, want)
	}
}

func TestResolveAttach(t *testing.T) {
	all := testAttachEntries()
	cases := []struct {
		query string
		want  string
	}{
		{"mh-aaa", "mh-aaa"},
		{"%2", "%2"},
		{"mh-old", "mh-old"}, // an exact id reaches an ended Session too
		{"brain", "%2"},
		{"BRAIN", "%2"},
		{"kairowebpreview", "mh-aaa"},
		{"KairoW", "mh-aaa"},   // a unique prefix
		{"kairo021", "mh-bbb"}, // exact wins over nothing longer
		{"shell", "mh-ccc"},    // the name an unnamed Session shows
		{"doc", "mh-ddd"},      // the ended Docs is not a candidate
		{"  Docs  ", "mh-ddd"}, // surrounding space is ignored
	}
	for _, c := range cases {
		got, err := resolveAttach(all, c.query)
		if err != nil || got.ID != c.want {
			t.Errorf("resolve %q = %q, %v; want %q", c.query, got.ID, err, c.want)
		}
	}
}

func TestResolveAttachAmbiguous(t *testing.T) {
	all := testAttachEntries()
	_, err := resolveAttach(all, "kairo")
	var ambiguous *attachAmbiguousError
	if !errors.As(err, &ambiguous) || len(ambiguous.Matches) != 2 {
		t.Fatalf("kairo: %v", err)
	}
	for _, want := range []string{"mh-aaa", "mh-bbb", "KairoWebPreview", "Kairo021"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("ambiguity message lacks %q:\n%s", want, err)
		}
	}
	// A longer name never hides an exact one: Kairo021 is exact for itself.
	if got, err := resolveAttach(all, "Kairo021"); err != nil || got.ID != "mh-bbb" {
		t.Fatalf("exact name: %q, %v", got.ID, err)
	}
	// Two Sessions with the same name are ambiguous even when exact.
	twin := append(all, attachEntry{ID: "mh-twin", Name: "docs", Live: true})
	if _, err := resolveAttach(twin, "docs"); !errors.As(err, &ambiguous) || len(ambiguous.Matches) != 2 {
		t.Fatalf("twin names: %v", err)
	}
}

func TestResolveAttachNotFound(t *testing.T) {
	all := testAttachEntries()
	for _, query := range []string{"nothing", "", "Hidden", "mh-zzz"} {
		_, err := resolveAttach(all, query)
		var missing *attachNotFoundError
		if !errors.As(err, &missing) {
			t.Errorf("%q: %v", query, err)
		}
	}
	// "brain" with no Brain running is not found, and does not match a Worker.
	if _, err := resolveAttach(all[1:], "brain"); err == nil {
		t.Fatalf("brain without a Brain resolved")
	}
}

func TestTmuxAttachEntries(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux does not run on Windows")
	}
	workers := []control.Worker{
		{ID: "%222", Name: "MewlaReleaseGates (%222)", ExecutorID: "claude", Cwd: "/r", Terminal: "tmux", TmuxSocket: "/s/one"},
		{ID: "%263", Name: "Gone (%263)", Terminal: "tmux", TmuxSocket: "/s/one"}, // pane no longer exists
		{ID: "%300", Name: "Asking (%300)", Terminal: "tmux", TmuxSocket: "/s/two", PromptID: "p1"},
		{ID: "mh-aaa", Name: "OnHost (mh-aaa)", Terminal: "host"},
		{ID: "%2", Name: "Moved (%2)", Terminal: "tmux"}, // the host has this id: it wins
	}
	asked := map[string]int{}
	entries := tmuxAttachEntries(workers, map[string]bool{"%2": true}, func(socket string) map[string]tmuxPane {
		asked[socket]++
		switch socket {
		case "/s/one":
			return map[string]tmuxPane{"%222": {Session: "$4"}}
		case "/s/two":
			return map[string]tmuxPane{"%300": {Session: "$9"}}
		}
		return nil
	})
	if len(entries) != 2 {
		t.Fatalf("entries %+v", entries)
	}
	first, second := entries[0], entries[1]
	if first.ID != "%222" || first.Name != "MewlaReleaseGates" || first.Tmux == nil || *first.Tmux != (attachTmux{Socket: "/s/one", Session: "$4", Pane: "%222"}) {
		t.Fatalf("first %+v tmux=%+v", first, first.Tmux)
	}
	if second.State != "needs input" || second.Tmux.Session != "$9" {
		t.Fatalf("second %+v", second)
	}
	if asked["/s/one"] != 1 {
		t.Fatalf("a server was asked %d times for its panes", asked["/s/one"])
	}
}

func TestParseTmuxPanes(t *testing.T) {
	panes := parseTmuxPanes("%0 $0\n%12 $3\n\nbad\n")
	if len(panes) != 2 || panes["%12"].Session != "$3" {
		t.Fatalf("%v", panes)
	}
}

func TestTmuxAttachArgs(t *testing.T) {
	target := attachTmux{Socket: "/s/one", Session: "$4", Pane: "%222"}
	got := strings.Join(tmuxAttachArgs(target, false), " ")
	want := "-u -S /s/one -N attach-session -t $4 ; select-window -t %222 ; select-pane -t %222"
	if got != want {
		t.Fatalf("attach:\n got %s\nwant %s", got, want)
	}
	got = strings.Join(tmuxAttachArgs(attachTmux{Session: "$4", Pane: "%222"}, true), " ")
	want = "-u switch-client -t $4 ; select-window -t %222 ; select-pane -t %222"
	if got != want {
		t.Fatalf("switch:\n got %s\nwant %s", got, want)
	}
}

func TestInsideSameTmux(t *testing.T) {
	target := attachTmux{Socket: "/s/one"}
	if !insideSameTmux(target, "/s/one,123,0") {
		t.Fatalf("same server")
	}
	if insideSameTmux(target, "/s/two,123,0") || insideSameTmux(target, "") {
		t.Fatalf("different or no server")
	}
	t.Setenv("TMUX_TMPDIR", "")
	if !insideSameTmux(attachTmux{}, defaultTmuxSocket("", os.Getuid())+",5,0") {
		t.Fatalf("default server")
	}
	if got := defaultTmuxSocket("/run/t", 7); got != filepath.Join("/run/t", "tmux-7", "default") {
		t.Fatalf("default socket %s", got)
	}
}

func TestTmuxDetachNoteNamesTmuxKey(t *testing.T) {
	note := tmuxDetachNote("C-a\n")
	if !strings.Contains(note, "C-a then d") || !strings.Contains(note, "Ctrl-\\") {
		t.Fatalf("note %q", note)
	}
	if !strings.Contains(tmuxDetachNote(""), "C-b then d") {
		t.Fatalf("default prefix")
	}
}

func TestFilterAttachEntries(t *testing.T) {
	entries := listedAttachEntries(testAttachEntries())
	names := func(list []attachEntry) string {
		var out []string
		for _, e := range list {
			out = append(out, e.Name)
		}
		return strings.Join(out, ",")
	}
	if got := names(filterAttachEntries(entries, "")); got != "Shell,KairoWebPreview,Kairo021,Brain,Docs" {
		t.Fatalf("all: %s", got)
	}
	if got := names(filterAttachEntries(entries, "KAIRO idle")); got != "Kairo021" {
		t.Fatalf("name and state: %s", got)
	}
	if got := names(filterAttachEntries(entries, "codex")); got != "Docs" {
		t.Fatalf("agent: %s", got)
	}
	if got := names(filterAttachEntries(entries, "needs input")); got != "Docs" {
		t.Fatalf("needs input: %s", got)
	}
	if got := names(filterAttachEntries(entries, "/work/Brain")); got != "Brain" {
		t.Fatalf("cwd: %s", got)
	}
	if got := filterAttachEntries(entries, "nothing like it"); len(got) != 0 {
		t.Fatalf("no match: %v", got)
	}
}

func TestParsePickerKeys(t *testing.T) {
	kinds := func(data string) []pickerKeyKind {
		var out []pickerKeyKind
		for _, key := range parsePickerKeys([]byte(data)) {
			out = append(out, key.kind)
		}
		return out
	}
	cases := []struct {
		in   string
		want []pickerKeyKind
	}{
		{"\x1b[A\x1b[B", []pickerKeyKind{keyUp, keyDown}},
		{"\x1bOA\x1bOB", []pickerKeyKind{keyUp, keyDown}},
		{"\x10\x0e", []pickerKeyKind{keyUp, keyDown}},
		{"\x1b[5~\x1b[6~", []pickerKeyKind{keyPageUp, keyPageDown}},
		{"\r", []pickerKeyKind{keyEnter}},
		{"\x1b", []pickerKeyKind{keyCancel}},
		{"\x03", []pickerKeyKind{keyCancel}},
		{"\x1c", []pickerKeyKind{keyCancel}},
		{"\x7f", []pickerKeyKind{keyBackspace}},
		{"ab", []pickerKeyKind{keyChar, keyChar}},
		{"\x1b[1;5C\x1b[Hx", []pickerKeyKind{keyChar}}, // other sequences are skipped whole
		{"é", []pickerKeyKind{keyChar}},
	}
	for _, c := range cases {
		got := kinds(c.in)
		if len(got) != len(c.want) {
			t.Errorf("%q: %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%q: %v, want %v", c.in, got, c.want)
				break
			}
		}
	}
}

func TestPickerSelectsWithArrowsAndFilter(t *testing.T) {
	entries := listedAttachEntries(testAttachEntries())
	size := func() (int, int) { return 100, 20 }
	run := func(input string) (*attachEntry, string) {
		var out bytes.Buffer
		chosen, err := runPicker(strings.NewReader(input), &out, entries, size, termStyle{}, func() time.Time { return attachNow })
		if err != nil {
			t.Fatal(err)
		}
		return chosen, out.String()
	}
	if chosen, _ := run("\r"); chosen == nil || chosen.ID != "mh-ccc" {
		t.Fatalf("Enter takes the newest: %+v", chosen)
	}
	if chosen, _ := run("\x1b[B\x1b[B\r"); chosen == nil || chosen.ID != "mh-bbb" {
		t.Fatalf("two down: %+v", chosen)
	}
	if chosen, _ := run("\x1b[A\r"); chosen == nil || chosen.ID != "mh-ccc" {
		t.Fatalf("up at the top stays: %+v", chosen)
	}
	if chosen, _ := run("\x1b[B\x1b[B\x1b[B\x1b[B\x1b[B\x1b[B\r"); chosen == nil || chosen.ID != "mh-ddd" {
		t.Fatalf("down past the end stays on the last: %+v", chosen)
	}
	if chosen, _ := run("brain\r"); chosen == nil || chosen.ID != "%2" {
		t.Fatalf("typed filter: %+v", chosen)
	}
	if chosen, _ := run("braix\x7f\x7fin\r"); chosen == nil || chosen.ID != "%2" {
		t.Fatalf("backspace: %+v", chosen)
	}
	if chosen, _ := run("zzz\r\x1b"); chosen != nil {
		t.Fatalf("Enter with nothing shown chose %+v", chosen)
	}
	if chosen, _ := run("\x1b"); chosen != nil {
		t.Fatalf("Esc chose %+v", chosen)
	}
	if chosen, _ := run(""); chosen != nil {
		t.Fatalf("end of input chose %+v", chosen)
	}
	_, screen := run("\x1b")
	if !strings.HasPrefix(screen, enterAttach) || !strings.HasSuffix(screen, leaveAttach) {
		t.Fatalf("the picker must take and give back the screen")
	}
}

func TestPickerLeavesInputToTheCaller(t *testing.T) {
	// What follows the choosing Enter has not been read: the attached
	// Session's first keys are not eaten by the picker.
	in := strings.NewReader("\rhello")
	var out bytes.Buffer
	size := func() (int, int) { return 100, 20 }
	// A reader that hands out one byte per Read models a slow typist.
	chosen, err := runPicker(io.LimitReader(oneByteReader{in}, 1), &out, listedAttachEntries(testAttachEntries()), size, termStyle{}, func() time.Time { return attachNow })
	if err != nil || chosen == nil {
		t.Fatalf("%v %v", chosen, err)
	}
	if rest, _ := io.ReadAll(in); string(rest) != "hello" {
		t.Fatalf("picker consumed past Enter: left %q", rest)
	}
}

type oneByteReader struct{ r io.Reader }

func (o oneByteReader) Read(p []byte) (int, error) { return o.r.Read(p[:1]) }

func TestRenderPickerShowsColumns(t *testing.T) {
	m := &pickerModel{entries: listedAttachEntries(testAttachEntries())}
	screen := renderPicker(m, 100, 20, termStyle{}, attachNow)
	for _, want := range []string{"Attach to a Session", "› ", "KairoWebPreview", "claude", "working", "idle", "needs input", "/work/Docs", "20m", "3h", "2d"} {
		if !strings.Contains(screen, want) {
			t.Errorf("screen lacks %q:\n%s", want, screen)
		}
	}
	if strings.Contains(screen, "Hidden helper") || strings.Contains(screen, "mh-old") {
		t.Fatalf("a hidden or ended Session is shown:\n%s", screen)
	}
	for _, line := range strings.Split(screen, "\r\n") {
		if len([]rune(line)) > 120 { // escapes aside, no line outruns the width by much
			t.Errorf("line too long (%d): %q", len([]rune(line)), line)
		}
	}
}

func TestRenderPickerScrollsToTheChosenRow(t *testing.T) {
	var many []attachEntry
	for i := 0; i < 30; i++ {
		many = append(many, attachEntry{ID: "mh-" + string(rune('a'+i%26)) + string(rune('a'+i/26)), Name: "Worker" + string(rune('A'+i%26)) + string(rune('a'+i/26)), Live: true, Agent: "claude", State: "idle"})
	}
	m := &pickerModel{entries: many}
	for i := 0; i < 25; i++ {
		m.apply(pickerKey{kind: keyDown}, pickerRows(10))
	}
	screen := renderPicker(m, 80, 10, termStyle{}, attachNow)
	if !strings.Contains(screen, "› "+"Worker") || strings.Count(screen, "\r\n") > 10 {
		t.Fatalf("scrolled screen:\n%s", screen)
	}
	if m.sel != 25 || m.offset+pickerRows(10) <= 25 {
		t.Fatalf("sel=%d offset=%d", m.sel, m.offset)
	}
}

func TestRenderPickerEmpty(t *testing.T) {
	m := &pickerModel{}
	if screen := renderPicker(m, 80, 10, termStyle{}, attachNow); !strings.Contains(screen, "No live Sessions.") {
		t.Fatalf("%s", screen)
	}
	m = &pickerModel{entries: listedAttachEntries(testAttachEntries()), query: []rune("zzz")}
	if screen := renderPicker(m, 80, 10, termStyle{}, attachNow); !strings.Contains(screen, "No Session matches.") {
		t.Fatalf("%s", screen)
	}
}

func TestWriteAttachTableListsIDs(t *testing.T) {
	var out bytes.Buffer
	writeAttachTable(&out, listedAttachEntries(testAttachEntries()), attachNow, true)
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 6 || !strings.HasPrefix(lines[0], "ID") {
		t.Fatalf("table:\n%s", out.String())
	}
	if !strings.Contains(lines[1], "mh-ccc") || !strings.Contains(lines[1], "Shell") {
		t.Fatalf("newest line: %q", lines[1])
	}
}

func TestBareWorkerName(t *testing.T) {
	for in, want := range map[string]string{
		"MewlaReleaseGates (%222)":  "MewlaReleaseGates",
		"mh-d2c (mh-d2c)":           "mh-d2c",
		"Plain":                     "Plain",
		"With (parens) inside (%7)": "With (parens) inside",
		"":                          "",
	} {
		if got := bareWorkerName(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}

func TestHumanAge(t *testing.T) {
	for d, want := range map[time.Duration]string{
		10 * time.Second: "now", 5 * time.Minute: "5m", 90 * time.Minute: "1h", 47 * time.Hour: "47h", 72 * time.Hour: "3d",
	} {
		if got := humanAge(attachNow.Add(-d), attachNow); got != want {
			t.Errorf("%v -> %s, want %s", d, got, want)
		}
	}
	if humanAge(time.Time{}, attachNow) != "-" {
		t.Fatalf("unknown age")
	}
}
