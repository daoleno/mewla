//go:build linux

package vdisplay

import (
	"encoding/binary"
	"fmt"
	"image"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/res"
	"github.com/jezek/xgb/xproto"
)

// The agent screen has no window manager, so a program's window appears at
// the screen's corner, on the parking monitor. The mover watches the screen's
// top-level windows and moves each one that belongs to an agent's program to
// that agent's monitor. A window belongs to the program whose process
// (_NET_WM_PID) is in the process group an agent's `open` started.
func init() { xgb.Logger = log.New(io.Discard, "", 0) }

type mover struct {
	conn *xgb.Conn
	once sync.Once
}

// startMover connects to the display and starts moving windows; monitorOf says
// where the program of a process belongs. If the display can't be reached it
// returns a mover that does nothing.
func startMover(d *Display, monitorOf func(pid int) (image.Rectangle, bool)) *mover {
	nc, err := net.Dial("unix", fmt.Sprintf("/tmp/.X11-unix/X%d", d.Number))
	if err != nil {
		return &mover{}
	}
	conn, err := xgb.NewConnNetWithCookieHex(nc, d.cookie)
	if err != nil {
		_ = nc.Close()
		return &mover{}
	}
	_ = res.Init(conn) // without it a window's process is read from its own property
	m := &mover{conn: conn}
	root := xproto.Setup(conn).DefaultScreen(conn).Root
	if err := xproto.ChangeWindowAttributesChecked(conn, root, xproto.CwEventMask, []uint32{xproto.EventMaskSubstructureNotify}).Check(); err != nil {
		conn.Close()
		return &mover{}
	}
	pidAtom, err := xproto.InternAtom(conn, false, uint16(len("_NET_WM_PID")), "_NET_WM_PID").Reply()
	if err != nil {
		conn.Close()
		return &mover{}
	}
	go func() {
		moved := map[xproto.Window]bool{}
		for {
			ev, err := conn.WaitForEvent()
			if ev == nil && err == nil {
				return // the connection closed
			}
			mapped, ok := ev.(xproto.MapNotifyEvent)
			if !ok || mapped.OverrideRedirect || moved[mapped.Window] {
				continue
			}
			moved[mapped.Window] = true
			go func(win xproto.Window) {
				pid := processOf(conn, win, pidAtom.Atom)
				if pid <= 0 {
					return
				}
				// The program is registered a moment after it starts: a window
				// that maps first waits for it.
				for i := 0; i < 20; i++ {
					if rect, ok := monitorOf(pid); ok {
						_ = xproto.ConfigureWindowChecked(conn, win, xproto.ConfigWindowX|xproto.ConfigWindowY, []uint32{uint32(rect.Min.X), uint32(rect.Min.Y)}).Check()
						return
					}
					time.Sleep(50 * time.Millisecond)
				}
			}(mapped.Window)
		}
	}()
	return m
}

// processOf is the process that owns a window: what the X server knows of
// the client (the RES extension), or else what the program says of itself
// (_NET_WM_PID, which toolkits set and old programs such as xmessage do not).
func processOf(conn *xgb.Conn, win xproto.Window, pidAtom xproto.Atom) int {
	if reply, err := res.QueryClientIds(conn, 1, []res.ClientIdSpec{{Client: uint32(win), Mask: res.ClientIdMaskLocalClientPID}}).Reply(); err == nil {
		for _, id := range reply.Ids {
			if id.Spec.Mask == res.ClientIdMaskLocalClientPID && len(id.Value) > 0 {
				return int(id.Value[0])
			}
		}
	}
	prop, err := xproto.GetProperty(conn, false, win, pidAtom, xproto.AtomCardinal, 0, 1).Reply()
	if err != nil || prop.Format != 32 || len(prop.Value) < 4 {
		return 0
	}
	return int(binary.LittleEndian.Uint32(prop.Value))
}

func (m *mover) stop() {
	if m == nil || m.conn == nil {
		return
	}
	m.once.Do(m.conn.Close)
}
