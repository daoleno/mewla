package screencast

import (
	"bytes"
	"os/exec"
	"sort"
	"testing"
	"time"
)

func testPicture(w, h, n int) []byte {
	pix := make([]byte, w*h*4)
	for y := range h {
		for x := range w {
			i := (y*w + x) * 4
			pix[i] = byte(x + n*7)
			pix[i+1] = byte(y + n*3)
			pix[i+2] = byte((x ^ y) + n)
		}
	}
	return pix
}

func nalTypes(au []byte) []int {
	var types []int
	for i := 0; i+4 < len(au); i++ {
		if bytes.Equal(au[i:i+4], startCode) {
			types = append(types, int(au[i+4]&0x1f))
		}
	}
	return types
}

func TestFFmpegEncoders(t *testing.T) {
	bin, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("no ffmpeg")
	}
	for i := range ffmpegKinds {
		kind := &ffmpegKinds[i]
		t.Run(kind.name, func(t *testing.T) {
			const w, h = 960, 600
			p, err := startFFmpeg(bin, kind, w, h, 4000)
			if err != nil {
				t.Skip(err)
			}
			defer p.close()
			var times []time.Duration
			total := 0
			for n := range 40 {
				start := time.Now()
				au, err := p.encode(testPicture(w, h, n))
				if err != nil {
					if n == 0 {
						t.Skipf("%s cannot encode here: %v", kind.name, err)
					}
					t.Fatal(err)
				}
				times = append(times, time.Since(start))
				total += len(au)
				types := nalTypes(au)
				if n == 0 && (len(types) < 3 || types[0] != 7 || types[1] != 8) {
					t.Fatalf("first picture is not led by SPS and PPS: %v", types)
				}
				if n > 0 && len(types) > 0 && (types[0] == 7 || types[0] == 5) {
					t.Fatalf("picture %d is a key frame: %v", n, types)
				}
			}
			sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
			t.Logf("%s: per picture p50 %v p95 %v (write+encode+read), %d KB for 40", kind.name, times[len(times)/2], times[len(times)*95/100], total>>10)
		})
	}
}

func TestEncoderKeyFrameOnDemand(t *testing.T) {
	enc, err := newFFmpegEncoder(3000)
	if err != nil {
		t.Skip(err)
	}
	defer enc.Close()
	const w, h = 320, 240
	for n := range 6 {
		start := time.Now()
		au, err := enc.Encode(testPicture(w, h, n), w, h, n == 3)
		if err != nil {
			t.Fatal(err)
		}
		types := nalTypes(au)
		key := len(types) > 0 && types[0] == 7
		if key != (n == 0 || n == 3) {
			t.Fatalf("picture %d: key %v, NALs %v", n, key, types)
		}
		t.Logf("%s picture %d key=%v %d bytes in %v", enc.Kind, n, key, len(au), time.Since(start))
	}
}
