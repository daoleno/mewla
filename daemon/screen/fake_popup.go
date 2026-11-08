package screen

import (
	"context"
	"time"
)

// FakePopupChoice is one pop-up item chosen on a fake app.
type FakePopupChoice struct {
	PID   int
	Popup string
	Item  string
}

// ChoosePopupItem opens a fake pop-up's menu, takes the item and closes it:
// the popup's value becomes the item.
func (f *FakeBackend) ChoosePopupItem(_ context.Context, e Element, title string) (PopupChoice, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	pid, index := int(e.ref>>16), int(e.ref&0xffff)-1
	for i := range f.RunningApps {
		app := &f.RunningApps[i]
		if app.PID != pid || index < 0 || index >= len(app.Elements) {
			continue
		}
		live := &app.Elements[index]
		if live.Role != e.Role || live.Title != e.Title {
			continue
		}
		options := f.PopupItems[live.Title]
		at := matchPopupItem(options, title)
		if at < 0 {
			return PopupChoice{}, &PopupItemError{Want: title, Options: options}
		}
		live.Value = options[at]
		f.PopupChoices = append(f.PopupChoices, FakePopupChoice{PID: pid, Popup: live.Title, Item: options[at]})
		f.ElementActions = append(f.ElementActions, FakeElementAction{PID: pid, Index: index, Action: "choose", Value: options[at]})
		return PopupChoice{Title: options[at], Open: 40 * time.Millisecond}, nil
	}
	return PopupChoice{}, ErrStaleElement
}

// FakeKeyGuard is one guard the fake was asked for.
type FakeKeyGuard struct {
	Front, Target int
	Ended         bool
	Stats         KeyGuardStats
}

// GuardKey arms a fake guard.
func (f *FakeBackend) GuardKey(front, target int) KeyGuard {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.NoKeyGuard {
		return nil
	}
	g := &FakeKeyGuard{Front: front, Target: target, Stats: KeyGuardStats{Held: f.GuardTakes}}
	f.KeyGuards = append(f.KeyGuards, g)
	return fakeGuard{f, g}
}

type fakeGuard struct {
	f *FakeBackend
	g *FakeKeyGuard
}

func (g fakeGuard) End() KeyGuardStats {
	g.f.mu.Lock()
	defer g.f.mu.Unlock()
	g.g.Ended = true
	return g.g.Stats
}

var (
	_ popupChooser = (*FakeBackend)(nil)
	_ keyGuarder   = (*FakeBackend)(nil)
)
