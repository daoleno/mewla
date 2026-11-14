package attachment

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"math/rand"
	"testing"
)

func screenshotPNG(t *testing.T, w, h int, alpha uint8) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	r := rand.New(rand.NewSource(1))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := uint8(200 + r.Intn(40))
			img.SetNRGBA(x, y, color.NRGBA{v, v - 20, v - 40, alpha})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestFitImageScalesAScreenshotDownToAJPEG(t *testing.T) {
	data := screenshotPNG(t, 1080, 2400, 255)
	out, contentType := FitImage(data, 1600)
	if contentType != "image/jpeg" || len(out) >= len(data)/2 {
		t.Fatalf("got %s of %d bytes from %d", contentType, len(out), len(data))
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil || config.Width != 720 || config.Height != 1600 {
		t.Fatalf("got %dx%d (%v), want 720x1600", config.Width, config.Height, err)
	}
}

func TestFitImageKeepsTransparencyAsPNG(t *testing.T) {
	data := screenshotPNG(t, 400, 3200, 128)
	out, contentType := FitImage(data, 800)
	if contentType != "image/png" {
		t.Fatalf("a translucent picture became %s", contentType)
	}
	config, _, _ := image.DecodeConfig(bytes.NewReader(out))
	if config.Height != 800 || config.Width != 100 {
		t.Fatalf("got %dx%d, want 100x800", config.Width, config.Height)
	}
}

func TestFitImageLeavesWhatItCannotImprove(t *testing.T) {
	small := screenshotPNG(t, 4, 4, 255)
	if out, contentType := FitImage(small, 1600); !bytes.Equal(out, small) || contentType != "image/png" {
		t.Fatalf("a tiny PNG changed to %s of %d bytes", contentType, len(out))
	}
	var animated bytes.Buffer
	frame := image.NewPaletted(image.Rect(0, 0, 3000, 10), color.Palette{color.Black, color.White})
	_ = gif.EncodeAll(&animated, &gif.GIF{Image: []*image.Paletted{frame, frame}, Delay: []int{10, 10}})
	if out, contentType := FitImage(animated.Bytes(), 1600); !bytes.Equal(out, animated.Bytes()) || contentType != "image/gif" {
		t.Fatal("a GIF changed (its animation would be lost)")
	}
	if out, _ := FitImage([]byte("not a picture"), 1600); string(out) != "not a picture" {
		t.Fatal("not a picture changed")
	}
	data := screenshotPNG(t, 1080, 2400, 255)
	if out, _ := FitImage(data, 0); !bytes.Equal(out, data) {
		t.Fatal("max edge 0 must leave the original")
	}
}
