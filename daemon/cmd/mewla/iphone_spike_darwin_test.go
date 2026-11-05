//go:build darwin

package main

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/iphone"
	"github.com/daoleno/mewla/daemon/phone"
	"github.com/daoleno/mewla/daemon/screen"
)

// A spike, not a test: the iPhone tools as a Worker calls them, on a Mac with
// iPhone Mirroring set up and the iPhone locked nearby.
//
//	MEWLA_IPHONE_TOOLS=1 go test ./cmd/mewla -run IPhoneToolsSpike -v
//
// It reads the screen, opens Settings by its name, swipes, opens Clock, goes
// home, and prints each answer with how long it took. Nothing is sent or
// changed on the iPhone.
func TestIPhoneToolsSpike(t *testing.T) {
	if os.Getenv("MEWLA_IPHONE_TOOLS") == "" {
		t.Skip("set MEWLA_IPHONE_TOOLS=1 to run")
	}
	s := screen.NewService(t.TempDir())
	device := iphone.New(newMirrorDriver(s), iphone.Options{})
	agent := phone.NewDeviceAgent(device, nil, iphone.Profile)
	t.Cleanup(func() { device.Release(); s.StopAgent(); time.Sleep(500 * time.Millisecond) })
	ctx := context.Background()
	for _, act := range []phone.AgentAction{
		{Action: phone.ActScreen},
		{Action: phone.ActLaunch, App: "设置"},
		{Action: phone.ActSwipe, Direction: "up"},
		{Action: phone.ActSwipe, Direction: "down"},
		{Action: phone.ActKey, Key: "home"},
		{Action: phone.ActLaunch, App: "时钟"},
		{Action: phone.ActKey, Key: "home", Image: true},
	} {
		started := time.Now()
		res, err := agent.Act(ctx, "%spike", "spike", act)
		text := res.Text
		if len(text) > 900 {
			text = text[:900] + "…"
		}
		fmt.Printf("== %s %s%s (%s, picture %d bytes): err %v\n%s\n", act.Action, act.App, act.Key+act.Direction, time.Since(started).Round(time.Millisecond), len(res.Image), err, text)
	}
}
