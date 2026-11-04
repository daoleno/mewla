//go:build darwin && !screenfake

package screen

import (
	"context"
	"fmt"
	"image"
	"os"
	"strconv"
	"testing"
	"time"
)

// clickStyles are the ways a background click is posted, tried in turn.
var clickStyles = []struct {
	name  string
	style clickStyle
}{
	{"plain", clickStyle{}},
	{"gap 30ms", clickStyle{gap: 30 * time.Millisecond}},
	{"move first + gap 30ms", clickStyle{moveFirst: true, gap: 30 * time.Millisecond}},
}

// The control for stage 1, on the real display, so a click that does not move
// the caret on the virtual display can be told from a click that never does:
//
//	MEWLA_CLICK_CONTROL=<TextEdit pid> go test ./screen -run ClickControl -v
//
// TextEdit must hold only an owned scratch file. For each style it sets the
// text (caret to the end), clicks the top-left of the text area and prints
// where the caret went.
func TestClickControl(t *testing.T) {
	pid, _ := strconv.Atoi(os.Getenv("MEWLA_CLICK_CONTROL"))
	if pid <= 0 {
		t.Skip("set MEWLA_CLICK_CONTROL=<TextEdit pid>")
	}
	if err := loadMac(); err != nil {
		t.Fatal(err)
	}
	say := func(format string, args ...any) {
		fmt.Printf("%s "+format+"\n", append([]any{time.Now().Format("15:04:05.000")}, args...)...)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	backend := newBackend()
	session, err := backend.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	apps := backend.(*macBackend)
	tree, err := apps.ReadApp(ctx, pid, 500)
	if err != nil {
		t.Fatal(err)
	}
	var area *Element
	for i := range tree.Elements {
		if tree.Elements[i].Role == "AXTextArea" {
			area = &tree.Elements[i]
		}
	}
	if area == nil {
		t.Fatal("no text area")
	}
	list, _ := apps.Apps(ctx)
	var window AppWindow
	for _, a := range list {
		if a.PID == pid && len(a.Windows) > 0 {
			window = a.Windows[0]
		}
	}
	caret := func() string {
		var out string
		withPool(func() { out = selectedRange(area.ref) })
		return out
	}
	at := area.Frame.Min.Add(image.Pt(10, 10))
	say("window %d %v, text area %v, click at %v", window.ID, window.Bounds, area.Frame, at)
	clicker := session.(*macSession)
	for _, c := range clickStyles {
		say("%s: set text: %v", c.name, apps.ActOnElement(ctx, *area, "set_value", "hello world, hello again"))
		time.Sleep(300 * time.Millisecond)
		before := caret()
		err := clicker.clickTo(pid, window.ID, at.X, at.Y, 0, 1, c.style)
		time.Sleep(500 * time.Millisecond)
		say("%s: caret %s -> %s (click: %v)", c.name, before, caret(), err)
	}
}
