//go:build windows && !screenfake

package screen

import (
	"image"
	"sort"
	"testing"
	"time"
)

func nalKinds(au []byte) []byte {
	var kinds []byte
	for i := 0; i+3 < len(au); i++ {
		if au[i] == 0 && au[i+1] == 0 && au[i+2] == 1 {
			if kind := au[i+3] & 0x1f; kind != 9 { // access unit delimiters lead, and are dropped in RTP
				kinds = append(kinds, kind)
			}
			i += 2
		}
	}
	return kinds
}

func windowsTestPicture(w, h, n int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			i := img.PixOffset(x, y)
			img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = byte(x+n*7), byte(y+n*3), byte((x^y)+n), 255
		}
	}
	return img
}

// Media Foundation's encoder gives each picture back at once (low latency),
// its first and asked-for key frames led by SPS and PPS, and no key frame
// otherwise.
func TestMediaFoundationEncoder(t *testing.T) {
	for _, size := range [][2]int{{1280, 800}, {1920, 1080}} {
		w, h := size[0], size[1]
		enc, err := newMFEncoder(w, h, 6000)
		if err != nil {
			t.Fatal(err)
		}
		var times []time.Duration
		total := 0
		for n := range 40 {
			img := windowsTestPicture(w, h, n)
			start := time.Now()
			au, key, err := enc.Encode(img, n == 20)
			if err != nil {
				enc.Close()
				t.Fatalf("%dx%d picture %d: %v", w, h, n, err)
			}
			times = append(times, time.Since(start))
			total += len(au)
			kinds := nalKinds(au)
			wantKey := n == 0 || n == 20
			if key != wantKey {
				t.Fatalf("%dx%d picture %d: key %v, NALs %v", w, h, n, key, kinds)
			}
			if wantKey && (len(kinds) < 3 || kinds[0] != 7 || kinds[1] != 8) {
				t.Fatalf("%dx%d key picture %d is not led by SPS and PPS: %v", w, h, n, kinds)
			}
			if n == 0 && len(au) > 5 {
				// profile_idc right after the SPS header byte.
				for i := 0; i+5 < len(au); i++ {
					if au[i] == 0 && au[i+1] == 0 && au[i+2] == 1 && au[i+3]&0x1f == 7 {
						if au[i+4] != 66 {
							t.Fatalf("profile_idc %d, want 66 (baseline)", au[i+4])
						}
						t.Logf("SPS profile %d constraints 0x%02x level %d", au[i+4], au[i+5], au[i+6])
						break
					}
				}
			}
		}
		enc.SetBitrate(2000)
		if _, _, err := enc.Encode(windowsTestPicture(w, h, 41), false); err != nil {
			t.Fatalf("after a new rate: %v", err)
		}
		enc.Close()
		sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
		t.Logf("%dx%d: per picture p50 %v p95 %v (convert+encode), %d KB for 40", w, h, times[len(times)/2], times[len(times)*95/100], total>>10)
	}
}
