package friends

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// Limits of what a post may carry. The service enforces the same ones.
const (
	MaxImages        = 4
	MaxVideoSeconds  = 60
	MaxVideoBytes    = 50 << 20
	MaxImageBytes    = 10 << 20
	maxSourceBytes   = 200 << 20
	maxImageSide     = 2048
	maxVideoSide     = 1280
	posterSide       = 640
	videoCrf         = "26"
	ffmpegTimeout    = 4 * time.Minute
	jpegQuality      = 88
	maxDecodedPixels = 64 << 20
)

// Draft media kinds.
const (
	KindImage = "image"
	KindVideo = "video"
)

// MediaRefusedError is a file the computer will not send, with the reason in
// words the owner can act on.
type MediaRefusedError struct{ Reason string }

func (e *MediaRefusedError) Error() string { return e.Reason }

func refuse(format string, args ...any) error {
	return &MediaRefusedError{Reason: fmt.Sprintf(format, args...)}
}

// MediaFile is one prepared image or video kept with a draft. Prepared files
// are stripped of metadata and re-encoded on this computer; the originals are
// never kept or sent.
type MediaFile struct {
	// File is the prepared file's name inside the draft's media directory.
	File       string `json:"file"`
	Kind       string `json:"kind"`
	Mime       string `json:"mime"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	Bytes      int64  `json:"bytes"`
	DurationMS int    `json:"duration_ms,omitempty"`
	SHA256     string `json:"sha256"`
	// Poster is a video's poster frame, a JPEG in the same directory.
	Poster *MediaFile `json:"poster,omitempty"`
}

// lookFFmpeg finds ffmpeg and ffprobe on PATH; tests replace it.
var lookFFmpeg = func() (string, string, error) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return "", "", err
	}
	ffprobe, err := exec.LookPath("ffprobe")
	return ffmpeg, ffprobe, err
}

// PrepareMedia checks the file at source, strips what identifies, and writes
// the result into dir. The format comes from the bytes, never the name.
func PrepareMedia(ctx context.Context, source, dir string) (MediaFile, error) {
	info, err := os.Stat(source)
	if err != nil {
		return MediaFile{}, refuse("cannot read %s", filepath.Base(source))
	}
	if !info.Mode().IsRegular() {
		return MediaFile{}, refuse("%s is not a file", filepath.Base(source))
	}
	if info.Size() == 0 || info.Size() > maxSourceBytes {
		return MediaFile{}, refuse("%s is empty or larger than %d MB", filepath.Base(source), maxSourceBytes>>20)
	}
	head := make([]byte, 16)
	file, err := os.Open(source)
	if err != nil {
		return MediaFile{}, refuse("cannot read %s", filepath.Base(source))
	}
	n, _ := io.ReadFull(file, head)
	file.Close()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return MediaFile{}, err
	}
	switch sniff(head[:n]) {
	case "jpeg", "png", "webp":
		return prepareImage(source, dir)
	case "mp4", "webm":
		return prepareVideo(ctx, source, dir)
	}
	return MediaFile{}, refuse("%s is not a JPEG, PNG, WebP, MP4 or WebM file", filepath.Base(source))
}

func sniff(head []byte) string {
	switch {
	case len(head) >= 3 && head[0] == 0xFF && head[1] == 0xD8 && head[2] == 0xFF:
		return "jpeg"
	case len(head) >= 8 && string(head[:8]) == "\x89PNG\r\n\x1a\n":
		return "png"
	case len(head) >= 12 && string(head[:4]) == "RIFF" && string(head[8:12]) == "WEBP":
		return "webp"
	case len(head) >= 12 && string(head[4:8]) == "ftyp":
		return "mp4"
	case len(head) >= 4 && head[0] == 0x1A && head[1] == 0x45 && head[2] == 0xDF && head[3] == 0xA3:
		return "webm"
	}
	return ""
}

func randomName() (string, error) {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// prepareImage decodes the image, applies its orientation, shrinks it, and
// writes a fresh file: nothing of the original container (EXIF, GPS, ICC,
// text chunks, thumbnails) survives because only pixels are carried over.
func prepareImage(source, dir string) (MediaFile, error) {
	raw, err := os.ReadFile(source)
	if err != nil {
		return MediaFile{}, refuse("cannot read %s", filepath.Base(source))
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || config.Width < 1 || config.Height < 1 {
		return MediaFile{}, refuse("%s is not an image this computer can read", filepath.Base(source))
	}
	if config.Width*config.Height > maxDecodedPixels {
		return MediaFile{}, refuse("%s is too large (%dx%d pixels)", filepath.Base(source), config.Width, config.Height)
	}
	decoded, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return MediaFile{}, refuse("%s is not an image this computer can read", filepath.Base(source))
	}
	if format == "jpeg" {
		decoded = orient(decoded, jpegOrientation(raw))
	}
	decoded = fit(decoded, maxImageSide)
	// Pictures with transparency stay PNG; the rest become JPEG.
	asPNG := format == "png" && hasAlpha(decoded)
	var out bytes.Buffer
	name, mime := "", ""
	if asPNG {
		err = (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&out, decoded)
		mime, name = "image/png", "png"
	} else {
		err = jpeg.Encode(&out, flatten(decoded), &jpeg.Options{Quality: jpegQuality})
		mime, name = "image/jpeg", "jpg"
	}
	if err != nil {
		return MediaFile{}, err
	}
	if out.Len() > MaxImageBytes {
		return MediaFile{}, refuse("%s is still over %d MB after shrinking", filepath.Base(source), MaxImageBytes>>20)
	}
	stem, err := randomName()
	if err != nil {
		return MediaFile{}, err
	}
	target := stem + "." + name
	if err := os.WriteFile(filepath.Join(dir, target), out.Bytes(), 0o600); err != nil {
		return MediaFile{}, err
	}
	sum := sha256.Sum256(out.Bytes())
	bounds := decoded.Bounds()
	return MediaFile{File: target, Kind: KindImage, Mime: mime, Width: bounds.Dx(), Height: bounds.Dy(),
		Bytes: int64(out.Len()), SHA256: hex.EncodeToString(sum[:])}, nil
}

func hasAlpha(img image.Image) bool {
	switch typed := img.(type) {
	case *image.RGBA:
		return !typed.Opaque()
	case *image.NRGBA:
		return !typed.Opaque()
	case *image.Paletted, *image.Gray, *image.YCbCr:
		return false
	}
	if opaque, ok := img.(interface{ Opaque() bool }); ok {
		return !opaque.Opaque()
	}
	return false
}

// flatten puts any transparency on white, which JPEG cannot carry.
func flatten(img image.Image) image.Image {
	if !hasAlpha(img) {
		return img
	}
	out := image.NewRGBA(img.Bounds())
	draw.Draw(out, out.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(out, out.Bounds(), img, img.Bounds().Min, draw.Over)
	return out
}

func fit(img image.Image, side int) image.Image {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= side && h <= side {
		return img
	}
	scale := float64(side) / float64(max(w, h))
	nw, nh := max(1, int(float64(w)*scale)), max(1, int(float64(h)*scale))
	out := image.NewRGBA(image.Rect(0, 0, nw, nh))
	draw.CatmullRom.Scale(out, out.Bounds(), img, bounds, draw.Src, nil)
	return out
}

// jpegOrientation reads the EXIF orientation (1 to 8) of a JPEG, or 1.
func jpegOrientation(raw []byte) int {
	if len(raw) < 4 || raw[0] != 0xFF || raw[1] != 0xD8 {
		return 1
	}
	for i := 2; i+4 <= len(raw); {
		if raw[i] != 0xFF {
			return 1
		}
		marker := raw[i+1]
		if marker == 0xDA || marker == 0xD9 {
			return 1
		}
		length := int(raw[i+2])<<8 | int(raw[i+3])
		if length < 2 || i+2+length > len(raw) {
			return 1
		}
		segment := raw[i+4 : i+2+length]
		if marker == 0xE1 && len(segment) >= 14 && string(segment[:6]) == "Exif\x00\x00" {
			return exifOrientation(segment[6:])
		}
		i += 2 + length
	}
	return 1
}

func exifOrientation(tiff []byte) int {
	if len(tiff) < 8 {
		return 1
	}
	var u16 func([]byte) int
	var u32 func([]byte) int
	switch string(tiff[:2]) {
	case "II":
		u16 = func(b []byte) int { return int(b[0]) | int(b[1])<<8 }
		u32 = func(b []byte) int { return int(b[0]) | int(b[1])<<8 | int(b[2])<<16 | int(b[3])<<24 }
	case "MM":
		u16 = func(b []byte) int { return int(b[0])<<8 | int(b[1]) }
		u32 = func(b []byte) int { return int(b[0])<<24 | int(b[1])<<16 | int(b[2])<<8 | int(b[3]) }
	default:
		return 1
	}
	offset := u32(tiff[4:8])
	if offset < 8 || offset+2 > len(tiff) {
		return 1
	}
	entries := u16(tiff[offset:])
	for i := 0; i < entries; i++ {
		at := offset + 2 + i*12
		if at+12 > len(tiff) {
			return 1
		}
		if u16(tiff[at:]) == 0x0112 {
			if value := u16(tiff[at+8:]); value >= 1 && value <= 8 {
				return value
			}
			return 1
		}
	}
	return 1
}

// orient turns the pixels so the picture is upright, as EXIF orientation says.
func orient(img image.Image, orientation int) image.Image {
	if orientation <= 1 || orientation > 8 {
		return img
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	nw, nh := w, h
	if orientation >= 5 {
		nw, nh = h, w
	}
	out := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var dx, dy int
			switch orientation {
			case 2:
				dx, dy = w-1-x, y
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dx, dy = x, h-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			out.Set(dx, dy, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return out
}

type probe struct {
	Streams []struct {
		CodecType string `json:"codec_type"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

// prepareVideo re-encodes the video to H.264 and AAC in MP4 with every piece
// of metadata dropped (location, device, creation time, titles, chapters,
// rotation tags) and the index at the front for streaming, and cuts a poster
// frame. It needs ffmpeg on this computer.
func prepareVideo(ctx context.Context, source, dir string) (MediaFile, error) {
	ffmpeg, ffprobe, err := lookFFmpeg()
	if err != nil {
		return MediaFile{}, refuse("videos need ffmpeg (with ffprobe) on this computer to be re-encoded before they are sent; install it and try again")
	}
	ctx, cancel := context.WithTimeout(ctx, ffmpegTimeout)
	defer cancel()
	found, err := runProbe(ctx, ffprobe, source)
	if err != nil {
		return MediaFile{}, refuse("%s is not a video this computer can read", filepath.Base(source))
	}
	seconds, _ := strconv.ParseFloat(found.Format.Duration, 64)
	hasVideo := false
	for _, stream := range found.Streams {
		if stream.CodecType == "video" && stream.Width > 0 {
			hasVideo = true
		}
	}
	if !hasVideo || seconds <= 0 {
		return MediaFile{}, refuse("%s has no video to send", filepath.Base(source))
	}
	if seconds > MaxVideoSeconds+1 {
		return MediaFile{}, refuse("%s is %d seconds long; a post video is at most %d", filepath.Base(source), int(seconds+0.5), MaxVideoSeconds)
	}
	stem, err := randomName()
	if err != nil {
		return MediaFile{}, err
	}
	target := filepath.Join(dir, stem+".mp4")
	// The scale keeps the picture inside maxVideoSide and even-sized, which
	// yuv420p needs. -map 0:v:0 -map 0:a:0? takes one video and, if there is
	// one, one audio track, so no data, subtitle or attachment stream goes along.
	scale := fmt.Sprintf("scale='if(gt(iw,ih),min(%d,iw),-2)':'if(gt(iw,ih),-2,min(%d,ih))',format=yuv420p", maxVideoSide, maxVideoSide)
	args := []string{"-nostdin", "-y", "-v", "error", "-i", source, "-map", "0:v:0", "-map", "0:a:0?",
		"-map_metadata", "-1", "-map_chapters", "-1", "-vf", scale,
		"-c:v", "libx264", "-profile:v", "high", "-level", "4.0", "-crf", videoCrf, "-preset", "medium", "-r", "30",
		"-c:a", "aac", "-b:a", "96k", "-ac", "2",
		"-fflags", "+bitexact", "-flags:v", "+bitexact", "-flags:a", "+bitexact",
		"-metadata:s:v", "rotate=0", "-movflags", "+faststart+use_metadata_tags", "-t", strconv.Itoa(MaxVideoSeconds), target}
	if output, err := exec.CommandContext(ctx, ffmpeg, args...).CombinedOutput(); err != nil {
		_ = os.Remove(target)
		return MediaFile{}, refuse("could not re-encode %s: %s", filepath.Base(source), firstLine(string(output)))
	}
	result, err := runProbe(ctx, ffprobe, target)
	if err != nil {
		_ = os.Remove(target)
		return MediaFile{}, refuse("could not read the re-encoded video of %s", filepath.Base(source))
	}
	var width, height int
	for _, stream := range result.Streams {
		if stream.CodecType == "video" {
			width, height = stream.Width, stream.Height
		}
	}
	outSeconds, _ := strconv.ParseFloat(result.Format.Duration, 64)
	info, err := os.Stat(target)
	if err != nil || width < 1 || height < 1 || outSeconds <= 0 {
		_ = os.Remove(target)
		return MediaFile{}, refuse("could not re-encode %s", filepath.Base(source))
	}
	if info.Size() > MaxVideoBytes {
		_ = os.Remove(target)
		return MediaFile{}, refuse("%s is %d MB after re-encoding; a post video is at most %d MB", filepath.Base(source), info.Size()>>20, MaxVideoBytes>>20)
	}
	sum, err := fileSHA256(target)
	if err != nil {
		return MediaFile{}, err
	}
	posterFile := stem + "-poster.jpg"
	poster, err := cutPoster(ctx, ffmpeg, target, filepath.Join(dir, posterFile), outSeconds)
	if err != nil {
		_ = os.Remove(target)
		return MediaFile{}, refuse("could not cut a poster frame from %s", filepath.Base(source))
	}
	return MediaFile{File: stem + ".mp4", Kind: KindVideo, Mime: "video/mp4", Width: width, Height: height,
		Bytes: info.Size(), DurationMS: int(outSeconds * 1000), SHA256: sum, Poster: &poster}, nil
}

func runProbe(ctx context.Context, ffprobe, path string) (probe, error) {
	output, err := exec.CommandContext(ctx, ffprobe, "-v", "error", "-print_format", "json", "-show_streams", "-show_format", path).Output()
	if err != nil {
		return probe{}, err
	}
	var out probe
	return out, json.Unmarshal(output, &out)
}

// cutPoster writes a JPEG frame from about a fifth into the video (the very
// first frame is often black), scaled to posterSide.
func cutPoster(ctx context.Context, ffmpeg, video, target string, seconds float64) (MediaFile, error) {
	at := strconv.FormatFloat(min(seconds*0.2, 3), 'f', 2, 64)
	scale := fmt.Sprintf("scale='if(gt(iw,ih),min(%d,iw),-2)':'if(gt(iw,ih),-2,min(%d,ih))'", posterSide, posterSide)
	args := []string{"-nostdin", "-y", "-v", "error", "-ss", at, "-i", video, "-frames:v", "1", "-map_metadata", "-1",
		"-vf", scale, "-q:v", "4", "-update", "1", target}
	if output, err := exec.CommandContext(ctx, ffmpeg, args...).CombinedOutput(); err != nil {
		return MediaFile{}, errors.New(firstLine(string(output)))
	}
	// Decoding it again and writing only pixels is the same strip images get.
	prepared, err := prepareImage(target, filepath.Dir(target))
	if err != nil {
		return MediaFile{}, err
	}
	_ = os.Remove(target)
	if prepared.Mime != "image/jpeg" {
		return MediaFile{}, errors.New("poster is not a JPEG")
	}
	return prepared, nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func firstLine(text string) string {
	text = strings.TrimSpace(text)
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		text = text[:i]
	}
	if len(text) > 200 {
		text = text[:200]
	}
	return text
}

// CheckMediaSet says whether prepared files make a valid post: up to four
// images, or exactly one video, nothing else.
func CheckMediaSet(files []MediaFile) error {
	if len(files) == 0 {
		return nil
	}
	videos := 0
	for _, file := range files {
		if file.Kind == KindVideo {
			videos++
		}
	}
	switch {
	case videos > 0 && len(files) > 1:
		return refuse("a post carries one video, or up to %d images, not both", MaxImages)
	case len(files) > MaxImages:
		return refuse("a post carries at most %d images", MaxImages)
	}
	return nil
}
