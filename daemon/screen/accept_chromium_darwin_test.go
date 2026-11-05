//go:build darwin && !screenfake

package screen

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// Does the background click and key recipe reach a Chromium app? A Chrome for
// Testing window with a page of its own is behind the person's TextEdit; the
// page counts the clicks it gets and shows what is typed in it, read through
// the DevTools port (the page's truth, not the agent's report).
//
//	MEWLA_ACCEPT=chromium MEWLA_ACCEPT_CHROME=<Chrome for Testing binary> go test ./screen -run AcceptChromium -v -count=1
func TestAcceptChromium(t *testing.T) {
	chromeBin := os.Getenv("MEWLA_ACCEPT_CHROME")
	if os.Getenv("MEWLA_ACCEPT") != "chromium" || chromeBin == "" {
		t.Skip("set MEWLA_ACCEPT=chromium and MEWLA_ACCEPT_CHROME=<binary>")
	}
	if err := loadMac(); err != nil {
		t.Fatal(err)
	}
	say := func(format string, args ...any) {
		fmt.Printf("%s "+format+"\n", append([]any{time.Now().Format("15:04:05.000")}, args...)...)
	}
	if idle := secondsIdle(); idle < 20 {
		t.Skipf("the Mac has been used %.0f s ago; this run needs 20 s of no input", idle)
	}
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `<title>probe</title><body style="margin:0"><button id=b style="position:absolute;left:100px;top:100px;width:200px;height:60px" onclick="window.clicks=(window.clicks||0)+1">count</button><input id=i style="position:absolute;left:100px;top:220px;width:300px;height:40px">`)
	}))
	defer site.Close()
	dir := t.TempDir()
	chrome := exec.Command(chromeBin, "--user-data-dir="+dir+"/profile", "--no-first-run", "--no-default-browser-check", "--remote-debugging-port=9335", "--window-size=900,700", "--window-position=200,150", site.URL)
	if err := chrome.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = chrome.Process.Kill(); _ = chrome.Wait() }()
	var ws string
	for i := 0; i < 100; i++ {
		resp, err := http.Get("http://127.0.0.1:9335/json/list")
		if err == nil {
			var targets []map[string]any
			_ = json.NewDecoder(resp.Body).Decode(&targets)
			resp.Body.Close()
			for _, tg := range targets {
				if tg["type"] == "page" && strings.HasPrefix(fmt.Sprint(tg["url"]), site.URL) {
					ws = fmt.Sprint(tg["webSocketDebuggerUrl"])
				}
			}
		}
		if ws != "" {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if ws == "" {
		t.Fatal("the page never opened")
	}
	conn, _, err := websocket.DefaultDialer.Dial(ws, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	id := 0
	eval := func(expr string) string {
		id++
		_ = conn.WriteJSON(map[string]any{"id": id, "method": "Runtime.evaluate", "params": map[string]any{"expression": expr, "returnByValue": true}})
		for {
			var m struct {
				ID     int `json:"id"`
				Result struct {
					Result struct {
						Value any `json:"value"`
					} `json:"result"`
				} `json:"result"`
			}
			_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
			if err := conn.ReadJSON(&m); err != nil {
				t.Fatal(err)
			}
			if m.ID == id {
				return fmt.Sprint(m.Result.Result.Value)
			}
		}
	}

	// The person's TextEdit goes in front last.
	personApp := launchTextEdit(t, dir, "person.txt", "")
	defer personApp.quit()
	if front := workspaceFrontPID(); front != personApp.pid {
		t.Fatalf("the person's TextEdit is not in front (%d)", front)
	}
	backend := newBackend()
	session, err := backend.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	clicker := session.(*macSession)
	mb := backend.(*macBackend)
	personArea := textAreaOf(t, mb, personApp.pid)

	window := windowOf(t, mb, chrome.Process.Pid)
	say("Chrome %d window %d at %v; person TextEdit %d in front", chrome.Process.Pid, window.ID, window.Bounds, personApp.pid)
	// The button's centre on the screen: the window's origin, the toolbar's
	// height (outer minus inner), and the button's place in the page.
	geo := eval(`JSON.stringify({sx: screenX, sy: screenY, chrome: outerHeight - innerHeight, b: document.getElementById('b').getBoundingClientRect().toJSON(), i: document.getElementById('i').getBoundingClientRect().toJSON()})`)
	var g struct {
		SX, SY, Chrome float64
		B, I           struct{ X, Y, Width, Height float64 }
	}
	_ = json.Unmarshal([]byte(geo), &g)
	say("page geometry %s", geo)
	button := image.Pt(int(g.SX+g.B.X+g.B.Width/2), int(g.SY+g.Chrome+g.B.Y+g.B.Height/2))
	input := image.Pt(int(g.SX+g.I.X+g.I.Width/2), int(g.SY+g.Chrome+g.I.Y+g.I.Height/2))
	say("button at %v, input at %v", button, input)

	source := mac.CGEventSourceCreate(1)
	defer mac.CFRelease(source)
	parked := cgPoint{900, 600}
	parkPointer(source, parked)
	say("pointer at %v, front app %d", pointerAt(), workspaceFrontPID())

	count := func() string { return eval(`String(window.clicks||0)`) }
	for _, c := range clickStyles {
		before := count()
		err := clicker.clickTo(chrome.Process.Pid, window, button.X, button.Y, 0, 1, c.style)
		time.Sleep(600 * time.Millisecond)
		after := count()
		say("click %-20s err=%v clicks %s -> %s landed=%v front=%d pointer=%v", c.name, err, before, after, before != after, workspaceFrontPID(), pointerAt())
	}
	// Keys into the input, after focusing it with the best click.
	for _, c := range []struct {
		name  string
		style keyStyle
	}{
		{"CGEventPostToPid", keyStyle{}},
		{"focus record + CGEventPostToPid", keyStyle{focus: true}},
		{"focus + make-key + SkyLight", keyStyle{focus: true, makeKey: true, skyLight: true}},
	} {
		eval(`document.getElementById('i').value=''`)
		_ = clicker.clickTo(chrome.Process.Pid, window, input.X, input.Y, 0, 1, clickStyle{})
		time.Sleep(400 * time.Millisecond)
		err := clicker.typeTo(chrome.Process.Pid, window, "chromekeys", c.style)
		time.Sleep(500 * time.Millisecond)
		got := eval(`document.getElementById('i').value`)
		say("keys  %-34s err=%v input=%q landed=%v front=%d", c.name, err, got, got == "chromekeys", workspaceFrontPID())
	}
	var person string
	withPool(func() { person = elementValue(mb, personApp.pid, personArea) })
	say("person's TextEdit text %q (empty: nothing of the agent's went to it); pointer %v; front %d", person, pointerAt(), workspaceFrontPID())
	if person != "" {
		t.Fatalf("the agent's input reached the person's TextEdit: %q", person)
	}
}
