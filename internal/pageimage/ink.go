package pageimage

import (
	"image"
	"math"

	"github.com/disintegration/imaging"
)

// Ink is how much of a side is darker than paper, as shares of its pixels.
// Blank detection decides on it, and it is kept with the page so a back
// that was wrongly hidden or shown can be explained from numbers alone.
type Ink struct {
	// Sheet is the share across the whole sheet, on the analysis copy.
	Sheet float64 `json:"sheet"`
	// Densest is the share in the most printed half-inch square, from the
	// scan's own pixels so thin strokes count in full.
	Densest float64 `json:"densest"`
}

// Genuine blank backs measure a Sheet of roughly 0.003–0.012 (show-through
// and shading) and a Densest under 0.002; printed pages measure a Sheet of
// 0.05+. Print confined to one area can stay under the Sheet threshold, and
// a back with one block of print measured a Densest of 0.19. Both
// thresholds err towards showing a side: hiding a printed page costs more
// than showing a blank one.
const (
	inkDarkThreshold      = 180
	blankSheetThreshold   = 0.02
	blankDensestThreshold = 0.03
)

// Blank reports whether a side with this ink looks unprinted.
func (i Ink) Blank() bool {
	return i.Sheet < blankSheetThreshold && i.Densest < blankDensestThreshold
}

// The DX400 always reads 8.5 inches across, so a scan's width gives its
// resolution. Densest is the darkest square of two by two cells, sliding a
// cell at a time so print straddling a cell boundary still fills a square.
// It leaves out a margin inside the crop, where a sliver of backing or the
// sheet's shadow can remain.
const (
	scanWidthInches   = 8.5
	inkCellInches     = 0.25
	inkMarginInches   = 0.125
	inkSamplesPerInch = 150
)

// MeasureInk measures the sheet as straightened and cropped by e. Judging
// the whole scan would count the darker backing beside a narrow sheet as ink.
func MeasureInk(img image.Image, e Edits) Ink {
	if img.Bounds().Empty() {
		return Ink{}
	}
	geometry := Edits{Straighten: e.Straighten, Rotation: e.Rotation, Crop: e.Crop}
	small := analysisCopy(img)
	f := float64(small.Rect.Dx()) / float64(img.Bounds().Dx())
	return Ink{
		Sheet:   round4(sheetInk(Render(small, geometry.scaled(f)))),
		Densest: round4(densestInk(img, geometry)),
	}
}

// IsBlankPaper reports whether the sheet, as straightened and cropped by e,
// looks unprinted.
func IsBlankPaper(img image.Image, e Edits) bool {
	return MeasureInk(img, e).Blank()
}

// IsBlank reports whether a side looks unprinted.
func IsBlank(img image.Image) bool {
	return IsBlankPaper(img, Edits{})
}

func sheetInk(img image.Image) float64 {
	small := analysisCopy(img)
	total, dark := 0, 0
	forEachPixel(small, func(r, g, b uint8) {
		total++
		if luma(r, g, b) < inkDarkThreshold {
			dark++
		}
	})
	if total == 0 {
		return 0
	}
	return float64(dark) / float64(total)
}

// densestInk samples the original's pixels without resampling them, which
// would blend thin strokes into the paper, and finds where e puts each one.
func densestInk(img image.Image, e Edits) float64 {
	b := img.Bounds()
	inch := float64(b.Dx()) / scanWidthInches
	m, outW, outH := Geometry(e, b.Dx(), b.Dy())
	margin := inkMarginInches * inch
	w, h := float64(outW)-2*margin, float64(outH)-2*margin
	if w <= 0 || h <= 0 {
		return 0
	}
	cols := max(1, int(math.Round(w/(inkCellInches*inch))))
	rows := max(1, int(math.Round(h/(inkCellInches*inch))))
	total, dark := make([]int, cols*rows), make([]int, cols*rows)

	lum := lumaPlane(img)
	step := max(1, int(inch/inkSamplesPerInch))
	area := Visible(m, outW, outH, b.Dx(), b.Dy())
	for y := area.Min.Y; y < area.Max.Y; y += step {
		for x := area.Min.X; x < area.Max.X; x += step {
			u, v := m.Apply(float64(x)+0.5, float64(y)+0.5)
			u, v = u-margin, v-margin
			if u < 0 || v < 0 || u >= w || v >= h {
				continue
			}
			i := min(rows-1, int(v*float64(rows)/h))*cols + min(cols-1, int(u*float64(cols)/w))
			total[i]++
			if lum.Pix[y*lum.Stride+x] < inkDarkThreshold {
				dark[i]++
			}
		}
	}
	return densestSquare(total, dark, cols, rows)
}

func densestSquare(total, dark []int, cols, rows int) float64 {
	sw, sh := min(2, cols), min(2, rows)
	densest := 0.0
	for r := 0; r+sh <= rows; r++ {
		for c := 0; c+sw <= cols; c++ {
			t, d := 0, 0
			for dr := range sh {
				for dc := range sw {
					i := (r+dr)*cols + c + dc
					t, d = t+total[i], d+dark[i]
				}
			}
			if t > 0 {
				densest = max(densest, float64(d)/float64(t))
			}
		}
	}
	return densest
}

// lumaPlane is img's brightness: a JPEG's own Y plane when it has one.
func lumaPlane(img image.Image) *image.Gray {
	switch m := img.(type) {
	case *image.YCbCr:
		return &image.Gray{Pix: m.Y, Stride: m.YStride, Rect: m.Rect}
	case *image.Gray:
		return m
	}
	n := imaging.Clone(img)
	g := image.NewGray(n.Rect)
	for y := 0; y < n.Rect.Dy(); y++ {
		for x := 0; x < n.Rect.Dx(); x++ {
			o := y*n.Stride + x*4
			g.Pix[y*g.Stride+x] = luma(n.Pix[o], n.Pix[o+1], n.Pix[o+2])
		}
	}
	return g
}

func round4(v float64) float64 {
	return math.Round(v*1e4) / 1e4
}
