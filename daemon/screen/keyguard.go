package screen

import (
	"context"
	"fmt"
)

// keyGuarder is a backend that can keep the person's key window while an app
// behind it acts. An app that is not in front and opens a menu (a pop-up
// button, a context menu) makes the window server take the keyboard from the
// person's window for as long as the menu is open: the window is told it lost
// key, and the person's typing stops landing in it. The guard stands between
// the window server and the person's app for those notices.
type keyGuarder interface {
	// GuardKey guards front's key window against target while it acts. It
	// never moves the keyboard itself. The guard lasts until End and a short
	// tail after it, because an app opens a pressed pop-up's menu a moment
	// after the press has returned.
	GuardKey(front, target int) KeyGuard
}

// KeyGuard is one armed guard.
type KeyGuard interface {
	End() KeyGuardStats
}

// KeyGuardStats is what a guard did since it was armed.
type KeyGuardStats struct {
	// Held is the number of times the app behind took the keyboard and the
	// person's window was not told.
	Held int
	// Missed counts the takings that could not be handed back.
	Missed int
	// Why says why there was no guard, when there was not.
	Why string
}

// note says what the guard did in words for the agent, or nothing when the
// app behind never touched the keyboard.
func (s KeyGuardStats) note(front string) string {
	switch {
	case s.Held > 0 && s.Missed == 0:
		return fmt.Sprintf(" The app took the keyboard %d time(s) and the guard kept it with %s: that window stayed key.", s.Held, front)
	case s.Held > 0:
		return fmt.Sprintf(" The app took the keyboard %d time(s); %s's window stayed key but %d could not be handed back, so the person's typing may have been blocked while a menu was open.", s.Held, front, s.Missed)
	}
	return ""
}

// guardKey guards the person's key window while the agent acts on app, when
// app is not in front. It gives back a func that ends the guard and words
// what it did; with no guard it says nothing.
func (a *agentSession) guardKey(ctx context.Context, app App) func() string {
	none := func() string { return "" }
	guarder, ok := a.apps.(keyGuarder)
	reader, canRead := a.apps.(frontReader)
	if !ok || !canRead || app.Active {
		return none
	}
	front, err := reader.FrontPID(ctx)
	if err != nil || front == 0 || front == app.PID {
		return none
	}
	guard := guarder.GuardKey(front, app.PID)
	if guard == nil {
		return none
	}
	name := "the person's app"
	if apps, err := a.apps.Apps(ctx); err == nil {
		for _, other := range apps {
			if other.PID == front {
				name = oneLine(other.Name)
			}
		}
	}
	return func() string { return guard.End().note(name) }
}
