//go:build screenfake

package screen

import (
	"context"
	"os"
	"slices"
	"strings"
)

// The screenfake build serves FakeBackend instead of the real screen, for
// end-to-end tests of the viewer. It is never a release build.
// MEWLA_SCREEN_FAKE_MISSING lists permission ids that start ungranted; opening
// a permission's settings grants it.
func newBackend() Backend {
	f := NewFakeBackend(1280, 800)
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
