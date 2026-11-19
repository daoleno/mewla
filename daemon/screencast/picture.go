package screencast

import "context"

// Picture is one picture of a screen.
type Picture struct {
	// Pix holds Height rows of Stride bytes, 4 bytes a pixel.
	Pix           []byte
	Width, Height int
	Stride        int
	// RGB: each pixel is R G B X (an image.RGBA); otherwise B G R X.
	RGB bool
	// Seq numbers the picture; a newer one has a higher number.
	Seq uint64
}

// Source is the screen a session sends.
type Source interface {
	// Next blocks until the screen differs from the picture numbered after
	// (0: none yet), then returns the newest picture. It is the caller's
	// until the next call.
	Next(ctx context.Context, after uint64) (Picture, error)
}

// Encoder compresses pictures into one H.264 stream (Annex B, no B-frames,
// every key frame led by its SPS and PPS).
type Encoder interface {
	// Encode compresses p, whose size is even; key asks for a key frame.
	Encode(p Picture, key bool) ([]byte, error)
	// SetBitrate asks for about kbps kbit/s from now on.
	SetBitrate(kbps int)
	// Name says which encoder this is, for the Connection panel and logs.
	Name() string
	Close()
}

// even returns p cropped to an even size (4:2:0 needs one), packed with no
// row padding; it copies only when it has to.
func even(p Picture) Picture {
	w, h := p.Width&^1, p.Height&^1
	if p.Stride == w*4 && h == p.Height {
		p.Pix = p.Pix[:w*h*4]
		p.Width = w
		return p
	}
	out := make([]byte, w*h*4)
	for y := range h {
		copy(out[y*w*4:(y+1)*w*4], p.Pix[y*p.Stride:y*p.Stride+w*4])
	}
	return Picture{Pix: out, Width: w, Height: h, Stride: w * 4, RGB: p.RGB, Seq: p.Seq}
}
