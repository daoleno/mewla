package screencast

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

// The encoder is the computer's own ffmpeg, run as a helper process: Mewla
// ships no H.264 encoder of its own. A hardware encoder (VA-API) is tried
// first, then x264 and OpenH264 in software. The process reads raw B G R X
// frames on its standard input and writes FLV on its output, which frames
// each picture with its length, so a picture's bytes are known to be complete
// the moment they arrive (a raw H.264 stream only shows where a picture ends
// when the next one starts).
//
// An ffmpeg process cannot be asked for a key frame or a new bit rate while it
// runs, so both come from a fresh process: a spare one is kept started, and
// switching to it costs one key frame, which is what a new bit rate and a
// lost picture need anyway.

// ErrNoEncoder means this computer has no ffmpeg that can encode H.264.
var ErrNoEncoder = errors.New("Screen needs ffmpeg with an H.264 encoder on this computer (install ffmpeg)")

type ffmpegKind struct {
	name string
	args func(kbps int) []string
}

var ffmpegKinds = []ffmpegKind{
	{"vaapi", func(kbps int) []string {
		return []string{"-vaapi_device", "/dev/dri/renderD128", "-vf", "format=nv12,hwupload",
			"-c:v", "h264_vaapi", "-profile:v", "constrained_baseline", "-bf", "0", "-g", "100000", "-async_depth", "1",
			"-rc_mode", "VBR", "-b:v", strconv.Itoa(kbps) + "k", "-maxrate", strconv.Itoa(kbps) + "k"}
	}},
	{"x264", func(kbps int) []string {
		return []string{"-c:v", "libx264", "-preset", "veryfast", "-tune", "zerolatency", "-profile:v", "baseline",
			"-pix_fmt", "yuv420p", "-bf", "0", "-g", "100000", "-crf", "21",
			"-maxrate", strconv.Itoa(kbps) + "k", "-bufsize", strconv.Itoa(max(kbps/4, 200)) + "k",
			"-x264-params", "scenecut=0:rc-lookahead=0:sync-lookahead=0:sliced-threads=1"}
	}},
	{"openh264", func(kbps int) []string {
		return []string{"-c:v", "libopenh264", "-profile:v", "constrained_baseline", "-pix_fmt", "yuv420p",
			"-g", "100000", "-b:v", strconv.Itoa(kbps) + "k", "-maxrate", strconv.Itoa(kbps) + "k"}
	}},
}

var probe struct {
	mu     sync.Mutex
	bin    string
	kinds  []*ffmpegKind // those that encode here, best first
	err    error
	failed time.Time
}

// probeAgain is how long a computer without a working ffmpeg is believed: one
// installed meanwhile is found on a view after that, without a restart.
const probeAgain = 30 * time.Second

// findEncoders returns the kinds that encode a test picture here, best first.
// What works is kept for the daemon's life.
func findEncoders() (string, []*ffmpegKind, error) {
	probe.mu.Lock()
	defer probe.mu.Unlock()
	if len(probe.kinds) > 0 {
		return probe.bin, probe.kinds, nil
	}
	if probe.err != nil && time.Since(probe.failed) < probeAgain {
		return "", nil, probe.err
	}
	probe.err = ErrNoEncoder
	probe.failed = time.Now()
	bin, err := exec.LookPath("ffmpeg")
	if err != nil {
		return "", nil, probe.err
	}
	var kinds []*ffmpegKind
	for i := range ffmpegKinds {
		kind := &ffmpegKinds[i]
		if kind.name == "vaapi" {
			if _, err := os.Stat("/dev/dri/renderD128"); err != nil {
				continue
			}
		}
		p, err := startFFmpeg(bin, kind, 320, 240, 1000, false)
		if err != nil {
			continue
		}
		pix := make([]byte, 320*240*4)
		_, err = p.encode(pix)
		p.close()
		if err == nil {
			kinds = append(kinds, kind)
		}
	}
	if len(kinds) == 0 {
		return "", nil, probe.err
	}
	probe.bin, probe.kinds, probe.err = bin, kinds, nil
	return bin, kinds, nil
}

// findEncoder is the best kind that encodes here.
func findEncoder() (string, *ffmpegKind, error) {
	bin, kinds, err := findEncoders()
	if err != nil {
		return "", nil, err
	}
	return bin, kinds[0], nil
}

// ffmpegProcess is one running encoder of one size and bit rate.
type ffmpegProcess struct {
	cmd    *exec.Cmd
	in     io.WriteCloser
	out    *bufio.Reader
	w, h   int
	kbps   int
	rgb    bool
	nalLen int
	sps    [][]byte
	pps    [][]byte
	used   bool

	closeOnce sync.Once
}

func startFFmpeg(bin string, kind *ffmpegKind, w, h, kbps int, rgb bool) (*ffmpegProcess, error) {
	format := "bgr0"
	if rgb {
		format = "rgb0"
	}
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin",
		"-f", "rawvideo", "-pix_fmt", format, "-s", fmt.Sprintf("%dx%d", w, h), "-framerate", "30", "-i", "pipe:0"}
	args = append(args, kind.args(kbps)...)
	args = append(args, "-fps_mode", "passthrough", "-an", "-f", "flv", "-flush_packets", "1", "pipe:1")
	cmd := exec.Command(bin, args...)
	cmd.Stderr = nil
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &ffmpegProcess{cmd: cmd, in: in, out: bufio.NewReaderSize(out, 256<<10), w: w, h: h, kbps: kbps, rgb: rgb, nalLen: 4}
	return p, nil
}

func (p *ffmpegProcess) close() {
	p.closeOnce.Do(func() {
		_ = p.in.Close()
		if p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
		}
		go func() { _ = p.cmd.Wait() }()
	})
}

// encode sends one picture and returns its H.264 access unit in Annex B,
// led by the SPS and PPS when it is a key frame.
func (p *ffmpegProcess) encode(pix []byte) ([]byte, error) {
	// Written beside the reads: a big key frame can fill the output pipe
	// before ffmpeg has taken the whole picture.
	errc := make(chan error, 1)
	go func() {
		_, err := p.in.Write(pix)
		errc <- err
	}()
	// A process that takes a picture and gives nothing back is stuck.
	stuck := time.AfterFunc(3*time.Second, p.close)
	au, err := p.read()
	stuck.Stop()
	if err != nil {
		p.close() // and the write with it
	}
	if werr := <-errc; err == nil && werr != nil {
		err = werr
	}
	return au, err
}

func (p *ffmpegProcess) read() ([]byte, error) {
	if !p.used {
		p.used = true
		var head [13]byte // FLV header (9) and the first previous-tag size (4)
		if _, err := io.ReadFull(p.out, head[:]); err != nil {
			return nil, err
		}
		if !bytes.HasPrefix(head[:], []byte("FLV")) {
			return nil, errors.New("ffmpeg did not write FLV")
		}
	}
	for {
		var tag [11]byte
		if _, err := io.ReadFull(p.out, tag[:]); err != nil {
			return nil, err
		}
		size := int(tag[1])<<16 | int(tag[2])<<8 | int(tag[3])
		body := make([]byte, size+4) // and the previous-tag size after it
		if _, err := io.ReadFull(p.out, body); err != nil {
			return nil, err
		}
		body = body[:size]
		if tag[0] != 9 || len(body) < 5 || body[0]&0x0f != 7 {
			continue // script data, or not H.264
		}
		switch body[1] {
		case 0: // AVCDecoderConfigurationRecord
			if err := p.config(body[5:]); err != nil {
				return nil, err
			}
		case 1:
			return p.annexB(body[5:], body[0]>>4 == 1)
		}
	}
}

func (p *ffmpegProcess) config(rec []byte) error {
	if len(rec) < 7 {
		return errors.New("short H.264 configuration")
	}
	p.nalLen = int(rec[4]&3) + 1
	p.sps, p.pps = nil, nil
	n := int(rec[5] & 0x1f)
	rest := rec[6:]
	read := func() ([]byte, bool) {
		if len(rest) < 2 {
			return nil, false
		}
		l := int(binary.BigEndian.Uint16(rest))
		if len(rest) < 2+l {
			return nil, false
		}
		b := append([]byte(nil), rest[2:2+l]...)
		rest = rest[2+l:]
		return b, true
	}
	for range n {
		b, ok := read()
		if !ok {
			return errors.New("bad SPS")
		}
		p.sps = append(p.sps, b)
	}
	if len(rest) < 1 {
		return errors.New("no PPS")
	}
	n = int(rest[0])
	rest = rest[1:]
	for range n {
		b, ok := read()
		if !ok {
			return errors.New("bad PPS")
		}
		p.pps = append(p.pps, b)
	}
	return nil
}

var startCode = []byte{0, 0, 0, 1}

func (p *ffmpegProcess) annexB(data []byte, key bool) ([]byte, error) {
	var out bytes.Buffer
	idr := false
	var nals [][]byte
	for len(data) > 0 {
		if len(data) < p.nalLen {
			return nil, errors.New("short NAL length")
		}
		l := 0
		for i := 0; i < p.nalLen; i++ {
			l = l<<8 | int(data[i])
		}
		data = data[p.nalLen:]
		if l > len(data) {
			return nil, errors.New("short NAL")
		}
		nal := data[:l]
		data = data[l:]
		if len(nal) > 0 && nal[0]&0x1f == 5 {
			idr = true
		}
		nals = append(nals, nal)
	}
	if key || idr {
		for _, s := range p.sps {
			out.Write(startCode)
			out.Write(s)
		}
		for _, s := range p.pps {
			out.Write(startCode)
			out.Write(s)
		}
	}
	for _, nal := range nals {
		t := nal[0] & 0x1f
		if (key || idr) && (t == 7 || t == 8) {
			continue // already led by them
		}
		out.Write(startCode)
		out.Write(nal)
	}
	return out.Bytes(), nil
}

// ffmpegEncoder turns pictures into one H.264 stream, switching to a fresh
// process for a key frame, a new size or a new bit rate.
type ffmpegEncoder struct {
	bin  string
	kind *ffmpegKind
	// next are the kinds to fall back to, in order, when kind fails (VA-API
	// at a size or alignment it does not take).
	next []*ffmpegKind

	cur   *ffmpegProcess
	spare chan *ffmpegProcess // being started, or ready
	kbps  int
}

func newFFmpegEncoder(kbps int) (*ffmpegEncoder, error) {
	bin, kinds, err := findEncoders()
	if err != nil {
		return nil, err
	}
	return &ffmpegEncoder{bin: bin, kind: kinds[0], next: kinds[1:], kbps: kbps}, nil
}

// RateCostsKeyFrame: a new bit rate takes a new ffmpeg process, which starts
// with a key frame.
func (e *ffmpegEncoder) RateCostsKeyFrame() bool { return true }

// Name is the kind of ffmpeg encoder: vaapi, x264 or openh264.
func (e *ffmpegEncoder) Name() string { return e.kind.name }

// warm starts a spare process for this size and the current bit rate.
func (e *ffmpegEncoder) warm(w, h int, rgb bool) {
	e.dropSpare()
	ch := make(chan *ffmpegProcess, 1)
	e.spare = ch
	bin, kind, kbps := e.bin, e.kind, e.kbps
	go func() {
		p, err := startFFmpeg(bin, kind, w, h, kbps, rgb)
		if err != nil {
			p = nil
		}
		ch <- p
	}()
}

// takeSpare returns a fresh process of this size and bit rate, waiting for the
// spare when it fits.
func (e *ffmpegEncoder) takeSpare(w, h int, rgb bool) (*ffmpegProcess, error) {
	if e.spare != nil {
		select {
		case p := <-e.spare:
			e.spare = nil
			if p != nil && p.w == w && p.h == h && p.kbps == e.kbps && p.rgb == rgb {
				return p, nil
			}
			if p != nil {
				p.close()
			}
		case <-time.After(2 * time.Second):
			e.dropSpare()
		}
	}
	return startFFmpeg(e.bin, e.kind, w, h, e.kbps, rgb)
}

// Encode compresses one picture; key asks for a key frame. A kind that fails
// gives way to the next for the rest of the view.
func (e *ffmpegEncoder) Encode(p Picture, key bool) ([]byte, error) {
	au, err := e.encode(p, key)
	for err != nil && len(e.next) > 0 {
		log.Printf("screencast: %s encoder failed at %dx%d (%v); %s from now on", e.kind.name, p.Width, p.Height, err, e.next[0].name)
		e.kind, e.next = e.next[0], e.next[1:]
		e.dropSpare()
		if e.cur != nil {
			e.cur.close()
			e.cur = nil
		}
		au, err = e.encode(p, true)
	}
	return au, err
}

func (e *ffmpegEncoder) encode(p Picture, key bool) ([]byte, error) {
	p = even(p)
	w, h := p.Width, p.Height
	if e.cur == nil || e.cur.w != w || e.cur.h != h || e.cur.rgb != p.RGB || key || e.cur.kbps != e.kbps {
		next, err := e.takeSpare(w, h, p.RGB)
		if err != nil {
			return nil, err
		}
		if e.cur != nil {
			e.cur.close()
		}
		e.cur = next
		e.warm(w, h, p.RGB)
	}
	au, err := e.cur.encode(p.Pix)
	if err != nil {
		e.cur.close()
		e.cur = nil
		return nil, err
	}
	return au, nil
}

// SetBitrate takes effect with the next key frame the stream needs, or at once
// on the next Encode when it differs enough from the rate in use.
func (e *ffmpegEncoder) SetBitrate(kbps int) {
	if kbps == e.kbps {
		return
	}
	e.kbps = kbps
	if e.cur != nil {
		e.warm(e.cur.w, e.cur.h, e.cur.rgb)
	}
}

// Bitrate is the rate in use now.
func (e *ffmpegEncoder) Bitrate() int {
	if e.cur != nil {
		return e.cur.kbps
	}
	return e.kbps
}

func (e *ffmpegEncoder) Close() {
	if e.cur != nil {
		e.cur.close()
		e.cur = nil
	}
	e.dropSpare()
}

// dropSpare lets the spare go, closing (and so reaping) it once it has
// started: a process left to the garbage collector exits unwaited.
func (e *ffmpegEncoder) dropSpare() {
	ch := e.spare
	if ch == nil {
		return
	}
	e.spare = nil
	go func() {
		if p := <-ch; p != nil {
			p.close()
		}
	}()
}
