package files

import (
	"bytes"
	"errors"
	"image"
	_ "image/gif" // decoders for thumbnails
	"image/jpeg"
	"image/png"

	_ "golang.org/x/image/bmp"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// Thumbnails: images (PNG, JPEG, GIF, WebP, BMP) decoded with the standard
// library and golang.org/x/image (BSD-3), scaled to fit thumbSize, and
// re-encoded (JPEG, or PNG when there is transparency). Re-encoding also
// strips metadata and anything that isn't pixels. Pure (tested).

const (
	thumbSize       = 640
	maxThumbPixels  = 64_000_000 // decoding a bigger image would need too much memory
	maxThumbSource  = 40 << 20
	maxPosterUpload = 700 << 10
)

var errNotImage = errors.New("files: not a decodable image")

// makeThumb scales an image to fit thumbSize × thumbSize (never up) and
// returns the encoded thumbnail and its type.
func makeThumb(src []byte) ([]byte, string, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(src))
	if err != nil {
		return nil, "", errNotImage
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxThumbPixels {
		return nil, "", errNotImage
	}
	img, _, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return nil, "", errNotImage
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w > thumbSize || h > thumbSize {
		if w >= h {
			h, w = max(1, h*thumbSize/w), thumbSize
		} else {
			w, h = max(1, w*thumbSize/h), thumbSize
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
	var out bytes.Buffer
	if hasAlpha(dst) {
		if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&out, dst); err != nil {
			return nil, "", err
		}
		return out.Bytes(), "image/png", nil
	}
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 82}); err != nil {
		return nil, "", err
	}
	return out.Bytes(), "image/jpeg", nil
}

func hasAlpha(img *image.RGBA) bool {
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] != 0xff {
			return true
		}
	}
	return false
}
