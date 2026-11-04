//go:build darwin && !screenfake

package screen

import (
	"errors"
	"image"
	"log"
	"sync"
	"time"

	"github.com/ebitengine/purego"
)

// SkyLight is the private framework under the window server. Its
// SLEventPostToPid delivers an event to one app as if it came through the
// window server, so a click reaches a window behind others without the
// pointer. It is private: a macOS without it, or that changed it, only loses
// clicks behind other windows, which then go on the screen.
var skyLight struct {
	once sync.Once
	post func(pid int32, event uintptr)
	why  string
}

func skyLightPost() (func(pid int32, event uintptr), string) {
	skyLight.once.Do(func() {
		lib, err := purego.Dlopen("/System/Library/PrivateFrameworks/SkyLight.framework/SkyLight", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			skyLight.why = "this macOS has no SkyLight to click behind other windows"
			log.Printf("screen: clicks behind other windows unavailable: %v", err)
			return
		}
		if _, err := purego.Dlsym(lib, "SLEventPostToPid"); err != nil {
			skyLight.why = "this macOS's SkyLight can't post to one app"
			log.Printf("screen: clicks behind other windows unavailable: no SLEventPostToPid")
			return
		}
		purego.RegisterLibFunc(&skyLight.post, lib, "SLEventPostToPid")
	})
	return skyLight.post, skyLight.why
}

// Mouse event fields naming the window under the pointer, so the app routes
// a click to that window although it isn't the one in front.
const (
	cgMouseWindowUnderPointer          = 91
	cgMouseWindowUnderPointerThatTakes = 92
)

func (s *macSession) BackgroundClicks() (bool, string) {
	post, why := skyLightPost()
	return post != nil, why
}

// ClickTo clicks one window of the app with pid, by framebuffer pixel,
// without moving the pointer or bringing the app forward.
func (s *macSession) ClickTo(pid int, window uint32, x, y, button, count int) error {
	return s.clickTo(pid, window, x, y, button, count, clickStyle{})
}

// clickStyle varies how a background click is posted, for the tests that
// find what an app takes: a mouse-moved event first, and a pause between
// the press and the release.
type clickStyle struct {
	moveFirst bool
	gap       time.Duration
}

func (s *macSession) clickTo(pid int, window uint32, x, y, button, count int, style clickStyle) error {
	post, why := skyLightPost()
	if post == nil {
		return errors.New(why)
	}
	px, py := s.input.point(image.Pt(x, y))
	at := cgPoint{px, py}
	send := func(kind uint32, state int64) {
		event := mac.CGEventCreateMouseEvent(s.source0, kind, at, uint32(button))
		if event == 0 {
			return
		}
		if state > 0 {
			mac.CGEventSetIntegerValueField(event, cgMouseClickStateField, state)
		}
		mac.CGEventSetIntegerValueField(event, cgMouseWindowUnderPointer, int64(window))
		mac.CGEventSetIntegerValueField(event, cgMouseWindowUnderPointerThatTakes, int64(window))
		post(int32(pid), event)
		mac.CFRelease(event)
	}
	if style.moveFirst {
		send(cgEventMouseMoved, 0)
		time.Sleep(style.gap)
	}
	for n := 1; n <= count; n++ {
		send(macDownTypes[button], int64(n))
		time.Sleep(style.gap)
		send(macUpTypes[button], int64(n))
	}
	return nil
}

var _ appClicker = (*macSession)(nil)
