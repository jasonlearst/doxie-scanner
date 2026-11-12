// Package pageimage turns a scanned side into what the user sees:
// decoding the scanner's JPEG to square pixels, judging whether a side is
// blank or in color, and rendering non-destructive edits on top of the
// untouched original.
package pageimage

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"

	"github.com/disintegration/imaging"
)

// Decode decodes a scanned side. When the scanner read fewer lines per
// inch than columns (200 dpi is read at 300 across), the result is
// resampled to square pixels at ydpi.
func Decode(jpg []byte, xdpi, ydpi int) (image.Image, error) {
	img, err := jpeg.Decode(bytes.NewReader(jpg))
	if err != nil {
		return nil, fmt.Errorf("pageimage: decode: %w", err)
	}
	if xdpi == ydpi || xdpi <= 0 || ydpi <= 0 {
		return img, nil
	}
	b := img.Bounds()
	return imaging.Resize(img, b.Dx()*ydpi/xdpi, b.Dy(), imaging.Lanczos), nil
}

// Analysis works on a downscaled copy: the decisions below don't need
// full resolution, and it keeps them quick on a Raspberry Pi.
const analysisWidth = 600

func analysisCopy(img image.Image) *image.NRGBA {
	if img.Bounds().Dx() <= analysisWidth {
		return imaging.Clone(img)
	}
	return imaging.Resize(img, analysisWidth, 0, imaging.Box)
}

// Auto color only counts pixels in flat neighborhoods: real color areas
// and gray shading have smooth interiors, while text edges (blurred into
// mid-tones by downscaling) and the color fringes a sheet-fed scanner
// leaves on black text sit where brightness changes sharply. A page is
// color if enough flat pixels carry real chroma (paper tint stays under
// the chroma floor), grayscale if enough flat pixels are mid-tones, and
// black & white otherwise.
const (
	flatRange         = 32
	chromaFloor       = 48
	colorFlatFraction = 0.001
	midToneLow        = 60
	midToneHigh       = 190
	grayFlatFraction  = 0.01
)

// DetectColorMode picks the color mode the Doxie app's "Auto" would.
func DetectColorMode(img image.Image) ColorMode {
	small := analysisCopy(img)
	w, h := small.Rect.Dx(), small.Rect.Dy()
	if w < 3 || h < 3 {
		return ColorBW
	}
	lum := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			o := y*small.Stride + x*4
			lum[y*w+x] = luma(small.Pix[o], small.Pix[o+1], small.Pix[o+2])
		}
	}

	total, colored, mid := 0, 0, 0
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			total++
			if !flat(lum, w, x, y) {
				continue
			}
			o := y*small.Stride + x*4
			if chroma(small.Pix[o], small.Pix[o+1], small.Pix[o+2]) >= chromaFloor {
				colored++
			}
			if v := lum[y*w+x]; v >= midToneLow && v <= midToneHigh {
				mid++
			}
		}
	}
	switch {
	case float64(colored)/float64(total) >= colorFlatFraction:
		return ColorColor
	case float64(mid)/float64(total) >= grayFlatFraction:
		return ColorGray
	default:
		return ColorBW
	}
}

func flat(lum []uint8, w, x, y int) bool {
	lo, hi := uint8(255), uint8(0)
	for dy := -1; dy <= 1; dy++ {
		for _, v := range lum[(y+dy)*w+x-1 : (y+dy)*w+x+2] {
			lo, hi = min(lo, v), max(hi, v)
		}
	}
	return hi-lo < flatRange
}

func forEachPixel(img *image.NRGBA, fn func(r, g, b uint8)) {
	for y := 0; y < img.Rect.Dy(); y++ {
		row := img.Pix[y*img.Stride : y*img.Stride+img.Rect.Dx()*4]
		for x := 0; x < len(row); x += 4 {
			fn(row[x], row[x+1], row[x+2])
		}
	}
}

func luma(r, g, b uint8) uint8 {
	return uint8((299*int(r) + 587*int(g) + 114*int(b)) / 1000)
}

func chroma(r, g, b uint8) uint8 {
	return max(r, g, b) - min(r, g, b)
}
