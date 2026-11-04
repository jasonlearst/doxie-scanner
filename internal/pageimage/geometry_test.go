package pageimage

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"testing"
)

// markerAt is the centre of the dark pixels in img.
func markerAt(img image.Image) (float64, float64, bool) {
	b := img.Bounds()
	var sx, sy, n float64
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if r, _, _, _ := img.At(x, y).RGBA(); r < 0x6000 {
				sx, sy, n = sx+float64(x-b.Min.X)+0.5, sy+float64(y-b.Min.Y)+0.5, n+1
			}
		}
	}
	return sx / n, sy / n, n > 0
}

func TestGeometry_PutsPixelsWhereRenderDoes(t *testing.T) {
	img := solid(300, 200, color.White)
	draw.Draw(img, image.Rect(180, 60, 186, 66), image.Black, image.Point{}, draw.Src)
	const mx, my = 183.0, 63.0

	for _, straighten := range []float64{0, 2.5, -4} {
		for _, rotation := range []int{0, 90, 180, 270} {
			for _, crop := range []*Crop{nil, {X: 100, Y: 30, W: 150, H: 120}} {
				e := Edits{Straighten: straighten}
				if crop != nil {
					e.Crop = &Crop{X: crop.X, Y: crop.Y, W: crop.W, H: crop.H}
				}
				e = e.WithRotation(rotation, 300, 200)

				out := Render(img, e)
				m, w, h := Geometry(e, 300, 200)
				if got := out.Bounds().Size(); got != (image.Point{w, h}) {
					t.Errorf("%+v: Geometry says %dx%d, Render made %v", e, w, h, got)
					continue
				}
				gx, gy, ok := markerAt(out)
				wx, wy := m.Apply(mx, my)
				if !ok || math.Hypot(gx-wx, gy-wy) > 1.5 {
					t.Errorf("%+v: marker at (%.1f, %.1f), Geometry says (%.1f, %.1f)", e, gx, gy, wx, wy)
				}
			}
		}
	}
}

func TestAffine_InverseUndoes(t *testing.T) {
	m, _, _ := Geometry(Edits{Straighten: 3, Rotation: 90, Crop: &Crop{X: 5, Y: 6, W: 70, H: 80}}, 100, 120)
	x, y := m.Then(m.Inverse()).Apply(17, 29)
	if math.Abs(x-17) > 1e-9 || math.Abs(y-29) > 1e-9 {
		t.Errorf("round trip gave (%v, %v)", x, y)
	}
	if x, y := Scale(2, 3).Then(Translate(1, 1)).Apply(4, 5); x != 9 || y != 16 {
		t.Errorf("scale then move gave (%v, %v), want (9, 16)", x, y)
	}
}

func TestVisible_CoversWhatShows(t *testing.T) {
	m, w, h := Geometry(Edits{Crop: &Crop{X: 10, Y: 20, W: 50, H: 40}}, 100, 100)
	if got := Visible(m, w, h, 100, 100); got != image.Rect(9, 19, 61, 61) {
		t.Errorf("crop only: %v", got)
	}
	m, w, h = Geometry(Edits{Straighten: 3, Crop: &Crop{X: 10, Y: 20, W: 50, H: 40}}, 100, 100)
	r := Visible(m, w, h, 100, 100)
	inv := m.Inverse()
	for _, c := range [4][2]float64{{0, 0}, {50, 0}, {0, 40}, {50, 40}} {
		if x, y := inv.Apply(c[0], c[1]); !image.Pt(int(x), int(y)).In(r) {
			t.Errorf("corner (%v, %v) of the result comes from (%.1f, %.1f), outside %v", c[0], c[1], x, y, r)
		}
	}
	if NeedsPixels := (Edits{Straighten: 2, Rotation: 90, Crop: &Crop{W: 1, H: 1}}).NeedsPixels(); NeedsPixels {
		t.Error("moving pixels doesn't need them changed")
	}
	for _, e := range []Edits{{Brightness: 5}, {Contrast: -5}, {Color: ColorGray}, {Color: ColorBW}} {
		if !e.NeedsPixels() {
			t.Errorf("%+v changes pixel values", e)
		}
	}
}
