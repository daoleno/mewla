package screen

import (
	"bytes"
	"fmt"
	"image"
	"io"
	"os/exec"
	"strconv"
	"sync"
	"testing"
	"time"
)

// ffmpegEncoder is a software x264 stand-in for the Mac's hardware encoder,
// so the video path can be measured and checked against a real decoder on
// machines that have ffmpeg. It is test-only: frames are told apart by a
// short quiet on the pipe, which a hardware encoder never needs.
type ffmpegEncoder struct {
	w, h, kbps int
	cmd        *exec.Cmd
	in         io.WriteCloser
	out        chan []byte
	pending    int
	mu         sync.Mutex
}

func ffmpegAvailable() bool { _, err := exec.LookPath("ffmpeg"); return err == nil }

func newFFmpegEncoder(w, h, kbps int) (videoEncoder, error) {
	e := &ffmpegEncoder{w: w, h: h, kbps: kbps}
	return e, e.start()
}

func (e *ffmpegEncoder) start() error {
	args := []string{"-loglevel", "error", "-f", "rawvideo", "-pix_fmt", "rgba",
		"-s", fmt.Sprintf("%dx%d", e.w, e.h), "-framerate", strconv.Itoa(videoFPS), "-i", "pipe:0",
		"-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency", "-profile:v", "high",
		"-pix_fmt", "yuv420p", "-bf", "0", "-g", "900",
		"-x264-params", "aud=1:repeat-headers=1:slices=1:threads=1",
		"-b:v", fmt.Sprintf("%dk", e.kbps), "-maxrate", fmt.Sprintf("%dk", e.kbps*3/2),
		"-bufsize", fmt.Sprintf("%dk", e.kbps/2), "-f", "h264", "pipe:1"}
	e.cmd = exec.Command("ffmpeg", args...)
	in, err := e.cmd.StdinPipe()
	if err != nil {
		return err
	}
	out, err := e.cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := e.cmd.Start(); err != nil {
		return err
	}
	e.in = in
	e.out = make(chan []byte, 64)
	go func(ch chan []byte) {
		buf := make([]byte, 1<<16)
		for {
			n, err := out.Read(buf)
			if n > 0 {
				ch <- append([]byte(nil), buf[:n]...)
			}
			if err != nil {
				close(ch)
				return
			}
		}
	}(e.out)
	e.pending = 0
	return nil
}

func (e *ffmpegEncoder) stop() {
	_ = e.in.Close()
	_ = e.cmd.Wait()
}

func (e *ffmpegEncoder) SetBitrate(kbps int) { e.mu.Lock(); e.kbps = kbps; e.mu.Unlock() }

func (e *ffmpegEncoder) Encode(img *image.RGBA, key bool) ([]byte, bool, error) {
	e.mu.Lock()
	want := e.kbps
	e.mu.Unlock()
	if key || want > e.kbps*12/10 || want < e.kbps*8/10 {
		e.stop()
		e.kbps = want
		if err := e.start(); err != nil {
			return nil, false, err
		}
		key = true
	}
	row := e.w * 4
	frame := make([]byte, 0, row*e.h)
	for y := 0; y < e.h; y++ {
		frame = append(frame, img.Pix[y*img.Stride:y*img.Stride+row]...)
	}
	if _, err := e.in.Write(frame); err != nil {
		return nil, false, err
	}
	var data []byte
	// The first frame waits for x264's start; later ones for a quiet pipe.
	wait := 3 * time.Second
	for {
		select {
		case chunk, ok := <-e.out:
			if !ok {
				return nil, false, io.ErrUnexpectedEOF
			}
			data = append(data, chunk...)
			wait = 12 * time.Millisecond
		case <-time.After(wait):
			if len(data) == 0 {
				return nil, false, io.ErrNoProgress
			}
			return data, hasNAL(data, 5), nil
		}
	}
}

func (e *ffmpegEncoder) Close() { e.stop() }

// hasNAL reports whether an Annex B stream holds a NAL unit of this type.
func hasNAL(data []byte, kind byte) bool {
	for i := 0; i+4 < len(data); i++ {
		if bytes.Equal(data[i:i+3], []byte{0, 0, 1}) && data[i+3]&0x1f == kind {
			return true
		}
	}
	return false
}

func TestFFmpegEncoderMakesDecodableAnnexB(t *testing.T) {
	if !ffmpegAvailable() {
		t.Skip("ffmpeg not installed")
	}
	fake := NewFakeBackend(320, 200)
	fake.Scroll = 4
	ctx, cancel := contextWithCancel(t)
	defer cancel()
	session, _ := fake.Start(ctx)
	enc, err := newFFmpegEncoder(320, 200, 2000)
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()
	var stream []byte
	for i := 0; i < 5; i++ {
		fake.ScrollTick()
		frame, _ := session.Frame(ctx, 0)
		data, key, err := enc.Encode(frame.Image, i == 0)
		if err != nil {
			t.Fatal(err)
		}
		if (i == 0) != key || (i == 0 && (!hasNAL(data, 7) || !hasNAL(data, 8))) {
			t.Fatalf("frame %d: key=%v sps=%v pps=%v", i, key, hasNAL(data, 7), hasNAL(data, 8))
		}
		stream = append(stream, data...)
	}
	if !hasNAL(stream, 9) {
		t.Fatal("no access unit delimiters")
	}
}
