//go:build linux

package vdisplay

import (
	"bytes"
	"context"
	"image/png"
	"net"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"
)

func startDisplay(t *testing.T) *Display {
	t.Helper()
	if err := Capability(); err != nil {
		t.Skip(err)
	}
	d, err := Start(context.Background(), t.TempDir()+"/d", 640, 400)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Stop)
	return d
}

func TestDisplayIsPrivate(t *testing.T) {
	d := startDisplay(t)
	info, err := os.Stat(d.SocketPath())
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("socket = %v, %v", info, err)
	}
	dir, _ := os.Stat(d.Dir)
	if dir.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %v", dir.Mode())
	}
	// Nothing listens on TCP for this display's RFB or X.
	for _, port := range []int{5900 + d.Number, 6000 + d.Number} {
		if c, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 300*time.Millisecond); err == nil {
			c.Close()
			t.Fatalf("something listens on tcp port %d", port)
		}
	}
	// An X client without the cookie is refused.
	cmd := exec.Command("xmessage", "-print", "x")
	cmd.Env = []string{"DISPLAY=:" + strconv.Itoa(d.Number)}
	out, _ := cmd.CombinedOutput()
	if !bytes.Contains(out, []byte("Authorization required")) && !bytes.Contains(out, []byte("unable to open display")) && !bytes.Contains(out, []byte("Cannot open display")) {
		t.Fatalf("a client without the cookie got in: %s", out)
	}
}

func TestAppDrawsAndInputReachesIt(t *testing.T) {
	if _, err := exec.LookPath("xmessage"); err != nil {
		t.Skip("xmessage is not installed")
	}
	d := startDisplay(t)
	c, err := d.Client()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	empty, err := c.Screenshot(5 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	app, err := d.Launch(t.TempDir(), "xmessage", "-center", "-buttons", "Yes:10", "-print", "hello")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(1500 * time.Millisecond)
	shot, err := c.Screenshot(5 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(empty, shot) {
		t.Fatal("the app did not change the picture")
	}
	img, err := png.Decode(bytes.NewReader(shot))
	if err != nil || img.Bounds().Dx() != 640 || img.Bounds().Dy() != 400 {
		t.Fatalf("picture: %v %v", err, img)
	}
	// Click the window's one button: xmessage exits with its code, which only
	// happens if the pointer events reached the app.
	deadline := time.After(10 * time.Second)
	for y := 196; ; y += 6 {
		if y > 260 {
			y = 196
		}
		if err := c.Pointer(320, y, 0); err != nil {
			t.Fatal(err)
		}
		if err := c.Pointer(320, y, 1); err != nil {
			t.Fatal(err)
		}
		if err := c.Pointer(320, y, 0); err != nil {
			t.Fatal(err)
		}
		select {
		case <-app.Done:
			if app.Code != 10 {
				t.Fatalf("exit code = %d, want 10 (the Yes button)", app.Code)
			}
			return
		case <-deadline:
			t.Fatal("clicks did not reach the app")
		case <-time.After(150 * time.Millisecond):
		}
	}
}

func TestChord(t *testing.T) {
	mods, key, err := Chord("ctrl+shift+t")
	if err != nil || len(mods) != 2 || key != 't' {
		t.Fatalf("%v %v %v", mods, key, err)
	}
	if _, key, _ := Chord("F5"); key != 0xffbe+4 {
		t.Fatalf("F5 = %x", key)
	}
	if _, key, _ := Chord("Enter"); key != 0xff0d {
		t.Fatalf("Enter = %x", key)
	}
	if _, _, err := Chord("hyper+x"); err == nil {
		t.Fatal("unknown modifier accepted")
	}
	if _, _, err := Chord("ctrl+"); err == nil {
		t.Fatal("empty key accepted")
	}
}
