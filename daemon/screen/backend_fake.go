//go:build screenfake

package screen

import (
	"context"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The screenfake build serves FakeBackend instead of the real screen, for
// end-to-end tests of the viewer. It is never a release build.
// MEWLA_SCREEN_FAKE_MISSING lists permission ids that start ungranted; opening
// a permission's settings grants it.
//
// MEWLA_SCREEN_FAKE_SCROLL=<pixels per step> shows a page that scrolls at 30
// steps a second, a fixture for measuring a viewer's link; with it,
// MEWLA_SCREEN_FAKE_SCROLL_FOR=<duration> stops the scrolling after that long
// (a stretch of motion, then a still screen).
func newBackend() Backend {
	f := NewFakeBackend(1280, 800)
	if scroll, _ := strconv.Atoi(os.Getenv("MEWLA_SCREEN_FAKE_SCROLL")); scroll > 0 {
		f.Scroll = scroll
		stop := time.Duration(0)
		if d, err := time.ParseDuration(os.Getenv("MEWLA_SCREEN_FAKE_SCROLL_FOR")); err == nil {
			stop = d
		}
		go func() {
			began := time.Now()
			for range time.Tick(time.Second / 30) {
				if stop > 0 && time.Since(began) > stop {
					return
				}
				f.ScrollTick()
			}
		}()
	}
	if missing := os.Getenv("MEWLA_SCREEN_FAKE_MISSING"); missing != "" {
		f.Missing = strings.Split(missing, ",")
	}
	return grantingFake{f}
}

type grantingFake struct{ *FakeBackend }

func (g grantingFake) OpenSettings(ctx context.Context, permission string) error {
	g.mu.Lock()
	g.Missing = slices.DeleteFunc(g.Missing, func(id string) bool { return id == permission })
	g.mu.Unlock()
	return g.FakeBackend.OpenSettings(ctx, permission)
}

func computerName() string { return "Fake screen" }

// ProcessPermissions has nothing to report without a permission model.
func ProcessPermissions() map[string]bool { return nil }
