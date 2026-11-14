package attachment

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"

	"golang.org/x/image/draw"
)

// FitImageMaxPixels bounds what FitImage decodes (a 40 MP picture is some 160
// MB of RGBA).
const FitImageMaxPixels = 40_000_000

// FitImage returns a picture no longer than maxEdge on its long side, for a
// phone that shows it in a row of a thread: scaled down when larger, and
// re-encoded (JPEG when every pixel is opaque, else PNG) when that is smaller.
// The original comes back unchanged when it is already smaller, when it is a
// GIF (its animation would be lost) or not a picture this decodes. A 1080x2400
// PNG screenshot of 690 KB is some 150 KB at 1600 px.
func FitImage(data []byte, maxEdge int) ([]byte, string) {
	original := http.DetectContentType(data)
	if maxEdge <= 0 || original == "image/gif" {
		return data, original
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > FitImageMaxPixels {
		return data, original
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return data, original
	}
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if long := max(width, height); long > maxEdge {
		width, height = max(1, width*maxEdge/long), max(1, height*maxEdge/long)
		scaled := image.NewRGBA(image.Rect(0, 0, width, height))
		draw.CatmullRom.Scale(scaled, scaled.Bounds(), img, bounds, draw.Src, nil)
		img = scaled
	}
	var out bytes.Buffer
	contentType := "image/jpeg"
	if opaque(img) {
		err = jpeg.Encode(&out, img, &jpeg.Options{Quality: 85})
	} else {
		contentType = "image/png"
		err = (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&out, img)
	}
	if err != nil || out.Len() >= len(data) {
		return data, original
	}
	return out.Bytes(), contentType
}

func opaque(img image.Image) bool {
	if o, ok := img.(interface{ Opaque() bool }); ok {
		return o.Opaque()
	}
	return false
}
