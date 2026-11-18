package screen

import (
	"errors"
	"image"
)

// This computer's own H.264 encoder, where it has one (VideoToolbox on a
// Mac, Media Foundation on Windows), for Screen's pushed video (CastEncoder).
// Linux has none of its own: package screencast uses the computer's ffmpeg.

// videoFPS is what an encoder is told to expect.
const videoFPS = 30

// videoEncoder compresses full-screen frames into one H.264 stream (Annex B,
// no B-frames, every key frame led by its SPS and PPS), so that each call's
// output is a picture a viewer can show the moment it has the stream's first
// key frame.
type videoEncoder interface {
	// Encode compresses img. A true key forces a key frame; the second
	// result says whether the output is one.
	Encode(img *image.RGBA, key bool) ([]byte, bool, error)
	// SetBitrate asks for about kbps kbit/s from now on.
	SetBitrate(kbps int)
	Close()
}

// errVideoDropped means the encoder chose to skip a frame, which leaves its
// stream as it was.
var errVideoDropped = errors.New("the encoder dropped the frame")

// videoFactory makes an encoder for a screen of this size.
type videoFactory func(width, height, kbps int) (videoEncoder, error)

// defaultVideo is this build's encoder, or nil when it has none; platforms
// set it, and defaultVideoName, in an init function.
var defaultVideo videoFactory

// defaultVideoName names defaultVideo, for the Connection panel.
var defaultVideoName string
