package screen

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/jpeg"
	"io"
	"net"
	"strconv"
	"testing"
	"time"
)

// testViewer is the client half of RFB, enough to check what the server sends.
type testViewer struct {
	t    *testing.T
	conn net.Conn
	fb   *image.RGBA
}

func (v *testViewer) read(n int) []byte {
	v.t.Helper()
	b := make([]byte, n)
	_ = v.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := io.ReadFull(v.conn, b); err != nil {
		v.t.Fatalf("read %d: %v", n, err)
	}
	return b
}

func (v *testViewer) write(b []byte) {
	v.t.Helper()
	_ = v.conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	if _, err := v.conn.Write(b); err != nil {
		v.t.Fatal(err)
	}
}

// noVNC's pixel format: 32 bpp little-endian RGBX.
var novncFormat = pixelFormat{bpp: 32, depth: 24, trueColor: true, redMax: 255, greenMax: 255, blueMax: 255, redShift: 0, greenShift: 8, blueShift: 16}

func (v *testViewer) setEncodings(encs ...int32) {
	msg := []byte{msgSetEncodings, 0}
	msg = binary.BigEndian.AppendUint16(msg, uint16(len(encs)))
	for _, e := range encs {
		msg = binary.BigEndian.AppendUint32(msg, uint32(e))
	}
	v.write(msg)
}

func (v *testViewer) request(incremental bool) {
	msg := []byte{msgFBUpdateRequest, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	if incremental {
		msg[1] = 1
	}
	binary.BigEndian.PutUint16(msg[6:], uint16(v.fb.Rect.Dx()))
	binary.BigEndian.PutUint16(msg[8:], uint16(v.fb.Rect.Dy()))
	v.write(msg)
}

type serverMessage struct {
	kind  byte
	rects []image.Rectangle
	encs  []int32
	fence string
}

// next reads one server message, applying framebuffer updates to v.fb.
func (v *testViewer) next() serverMessage {
	v.t.Helper()
	kind := v.read(1)[0]
	msg := serverMessage{kind: kind}
	switch kind {
	case msgFramebufferUpdate:
		count := int(binary.BigEndian.Uint16(v.read(3)[1:]))
		for i := 0; i < count; i++ {
			h := v.read(12)
			r := image.Rect(0, 0, int(binary.BigEndian.Uint16(h[4:])), int(binary.BigEndian.Uint16(h[6:]))).
				Add(image.Pt(int(binary.BigEndian.Uint16(h[0:])), int(binary.BigEndian.Uint16(h[2:]))))
			enc := int32(binary.BigEndian.Uint32(h[8:]))
			msg.rects = append(msg.rects, r)
			msg.encs = append(msg.encs, enc)
			switch enc {
			case encRaw:
				px := v.read(r.Dx() * r.Dy() * 4)
				for y := 0; y < r.Dy(); y++ {
					for x := 0; x < r.Dx(); x++ {
						p := px[(y*r.Dx()+x)*4:]
						i := v.fb.PixOffset(r.Min.X+x, r.Min.Y+y)
						copy(v.fb.Pix[i:i+3], p[:3]) // RGBX → RGB
						v.fb.Pix[i+3] = 255
					}
				}
			case encTight:
				ctl := v.read(1)[0]
				switch ctl {
				case tightFill:
					c := v.read(3)
					for y := r.Min.Y; y < r.Max.Y; y++ {
						for x := r.Min.X; x < r.Max.X; x++ {
							i := v.fb.PixOffset(x, y)
							copy(v.fb.Pix[i:], []byte{c[0], c[1], c[2], 255})
						}
					}
				case tightJPEG:
					n := 0
					for shift := 0; shift < 21; shift += 7 {
						b := v.read(1)[0]
						if shift == 14 {
							n |= int(b) << 14
							break
						}
						n |= int(b&0x7f) << shift
						if b&0x80 == 0 {
							break
						}
					}
					img, err := jpeg.Decode(bytes.NewReader(v.read(n)))
					if err != nil {
						v.t.Fatalf("tight JPEG: %v", err)
					}
					if img.Bounds().Dx() != r.Dx() || img.Bounds().Dy() != r.Dy() {
						v.t.Fatalf("JPEG %v for rect %v", img.Bounds(), r)
					}
					for y := 0; y < r.Dy(); y++ {
						for x := 0; x < r.Dx(); x++ {
							cr, cg, cb, _ := img.At(img.Bounds().Min.X+x, img.Bounds().Min.Y+y).RGBA()
							i := v.fb.PixOffset(r.Min.X+x, r.Min.Y+y)
							copy(v.fb.Pix[i:], []byte{byte(cr >> 8), byte(cg >> 8), byte(cb >> 8), 255})
						}
					}
				default:
					v.t.Fatalf("unexpected tight control %#x", ctl)
				}
			case pseudoDesktopSize:
				v.fb = image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
			default:
				v.t.Fatalf("unexpected encoding %d", enc)
			}
		}
	case msgEndOfContinuous:
	case msgFence:
		h := v.read(8)
		msg.fence = string(v.read(int(h[7])))
		if binary.BigEndian.Uint32(h[3:7])&fenceRequest == 0 {
			v.t.Fatal("server fence without the request flag")
		}
	default:
		v.t.Fatalf("unexpected server message %d", kind)
	}
	return msg
}

func openViewer(t *testing.T, fake *FakeBackend) *testViewer {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	viewer, server := net.Pipe()
	session, _ := fake.Start(ctx)
	go func() { _ = serveRFB(ctx, server, session, "Test screen") }()
	t.Cleanup(func() { viewer.Close() })
	v := &testViewer{t: t, conn: viewer}
	v.write([]byte{1}) // ClientInit, shared
	head := v.read(24)
	width, height := int(binary.BigEndian.Uint16(head[0:])), int(binary.BigEndian.Uint16(head[2:]))
	if width != fake.Width || height != fake.Height {
		t.Fatalf("ServerInit %dx%d", width, height)
	}
	if name := string(v.read(int(binary.BigEndian.Uint32(head[20:])))); name != "Test screen" {
		t.Fatalf("name %q", name)
	}
	v.fb = image.NewRGBA(image.Rect(0, 0, width, height))
	pf := append([]byte{msgSetPixelFormat, 0, 0, 0}, novncFormat.bytes()...)
	v.write(pf)
	return v
}

func frameOf(t *testing.T, fake *FakeBackend) *image.RGBA {
	t.Helper()
	s := &fakeSession{backend: fake, image: image.NewRGBA(image.Rect(0, 0, fake.Width, fake.Height))}
	f, err := s.Frame(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	return f.Image
}

func TestRawUpdatesMatchTheScreenExactly(t *testing.T) {
	fake := NewFakeBackend(200, 130)
	v := openViewer(t, fake)
	v.setEncodings(encRaw)
	v.request(false)
	msg := v.next()
	if msg.kind != msgFramebufferUpdate || msg.encs[0] != encRaw {
		t.Fatalf("first update %+v", msg)
	}
	if want := frameOf(t, fake); !bytes.Equal(v.fb.Pix, want.Pix) {
		t.Fatal("raw framebuffer differs from the screen")
	}

	// Input reaches the backend, and an incremental update sends only the
	// changed tiles.
	v.write([]byte{msgPointerEvent, 1, 0, 150, 0, 100})
	v.write([]byte{msgKeyEvent, 1, 0, 0, 0, 0, 0, 'a'})
	v.request(true)
	msg = v.next()
	for _, r := range msg.rects {
		if r.Dx()*r.Dy() >= 200*130 {
			t.Fatalf("incremental update resent the whole screen: %v", msg.rects)
		}
	}
	for v.fb == nil || !bytes.Equal(v.fb.Pix, frameOf(t, fake).Pix) {
		v.request(true)
		v.next()
	}
	pointers, keys := fake.Snapshot()
	if len(pointers) != 1 || pointers[0] != (FakePointer{X: 150, Y: 100, Buttons: 1}) {
		t.Fatalf("pointers %+v", pointers)
	}
	if len(keys) != 1 || keys[0] != (FakeKey{Keysym: 'a', Down: true}) {
		t.Fatalf("keys %+v", keys)
	}
}

func TestTightContinuousUpdatesAreFencedAndDecodable(t *testing.T) {
	fake := NewFakeBackend(320, 200)
	v := openViewer(t, fake)
	v.setEncodings(encTight, encRaw, pseudoQualityLevel0+9, pseudoDesktopSize, pseudoFence, pseudoContinuousUpdts)
	if msg := v.next(); msg.kind != msgEndOfContinuous {
		t.Fatalf("server did not announce continuous updates: %+v", msg)
	}
	// Enable continuous updates over the whole screen, as noVNC does.
	v.write([]byte{msgEnableContinuous, 1, 0, 0, 0, 0, 1, 64, 0, 200})

	msg := v.next()
	if msg.kind != msgFramebufferUpdate || msg.encs[0] != encTight {
		t.Fatalf("first update %+v", msg)
	}
	fence := v.next()
	if fence.kind != msgFence || fence.fence != "1" {
		t.Fatalf("fence %+v", fence)
	}
	// JPEG at quality 9 stays close to the source.
	want := frameOf(t, fake)
	var diff int
	for i := range want.Pix {
		d := int(want.Pix[i]) - int(v.fb.Pix[i])
		diff += max(d, -d)
	}
	if avg := float64(diff) / float64(len(want.Pix)); avg > 6 {
		t.Fatalf("decoded JPEG differs by %.1f per channel", avg)
	}

	// Without fence replies the server stops after maxFramesInFlight.
	for i := 0; i < 6; i++ {
		v.write([]byte{msgKeyEvent, 1, 0, 0, 0, 0, 0, byte('a' + i)})
		time.Sleep(10 * time.Millisecond)
	}
	got := 1
	_ = v.next() // frame 2
	if f := v.next(); f.fence != "2" {
		t.Fatalf("fence 2 = %+v", f)
	}
	got++
	_ = v.conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	var one [1]byte
	if _, err := v.conn.Read(one[:]); err == nil {
		t.Fatalf("server sent message %d with %d frames unacknowledged", one[0], got)
	}
	// Acknowledging fence 2 releases the newest screen in one update.
	v.write(append([]byte{msgFence, 0, 0, 0, 0, 0, 0, 0, 1}, '2'))
	if msg := v.next(); msg.kind != msgFramebufferUpdate {
		t.Fatalf("after ack %+v", msg)
	}
	if f := v.next(); f.fence != strconv.Itoa(got+1) {
		t.Fatalf("fence %+v", f)
	}
	_, keys := fake.Snapshot()
	if len(keys) != 6 {
		t.Fatalf("keys %+v", keys)
	}
}

func TestSolidRectsUseTightFill(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 70, 70))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	out, err := encodeRect(img, image.Rect(0, 0, 64, 64), novncFormat, true, 6)
	if err != nil {
		t.Fatal(err)
	}
	if out[12] != tightFill || !bytes.Equal(out[13:16], []byte{200, 200, 200}) || len(out) != 16 {
		t.Fatalf("fill rect % x", out)
	}
}

func TestCompactLength(t *testing.T) {
	for n, want := range map[int][]byte{5: {5}, 200: {0xc8, 0x01}, 70000: {0xf0, 0xa2, 0x04}} {
		if got := appendCompactLength(nil, n); !bytes.Equal(got, want) {
			t.Fatalf("%d: % x, want % x", n, got, want)
		}
	}
}

func TestDirtyRectsMergeTiles(t *testing.T) {
	a := image.NewRGBA(image.Rect(0, 0, 300, 300))
	b := image.NewRGBA(image.Rect(0, 0, 300, 300))
	for y := 70; y < 190; y++ {
		for x := 70; x < 190; x++ {
			b.Pix[b.PixOffset(x, y)] = 1
		}
	}
	got := dirtyRects(a, b)
	if len(got) != 1 || got[0] != image.Rect(64, 64, 192, 192) {
		t.Fatalf("dirty %v", got)
	}
	if len(dirtyRects(a, a)) != 0 {
		t.Fatal("identical frames are dirty")
	}
}
