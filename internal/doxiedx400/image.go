package doxiedx400

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"

	"github.com/disintegration/imaging"
)

// Genuine blank backs measure an ink fraction of roughly 0.003–0.012
// (show-through and shading), printed content 0.05+; the threshold sits
// in that gap. Sampling every 4th pixel each way is plenty for that
// decision and keeps it fast on a Raspberry Pi.
const (
	blankDarkThreshold        = 180
	blankInkFractionThreshold = 0.02
	blankSampleStep           = 4
)

// decodeSide decodes one side and, when the scan used a different
// resolution across than down (200 dpi is read at 300 across), resamples
// it to square pixels.
func decodeSide(jpg []byte, w window) (image.Image, error) {
	img, err := jpeg.Decode(bytes.NewReader(jpg))
	if err != nil {
		return nil, fmt.Errorf("doxiedx400: decode scanned side: %w", err)
	}
	if w.xRes == w.yRes {
		return img, nil
	}
	b := img.Bounds()
	return imaging.Resize(img, b.Dx()*w.yRes/w.xRes, b.Dy(), imaging.Lanczos), nil
}

func isBlank(img image.Image) bool {
	b := img.Bounds()
	total, dark := 0, 0
	for y := b.Min.Y; y < b.Max.Y; y += blankSampleStep {
		for x := b.Min.X; x < b.Max.X; x += blankSampleStep {
			if luma(img, x, y) < blankDarkThreshold {
				dark++
			}
			total++
		}
	}
	if total == 0 {
		return true
	}
	return float64(dark)/float64(total) < blankInkFractionThreshold
}

func luma(img image.Image, x, y int) uint8 {
	if ycc, ok := img.(*image.YCbCr); ok {
		return ycc.Y[ycc.YOffset(x, y)]
	}
	return color.GrayModel.Convert(img.At(x, y)).(color.Gray).Y
}
