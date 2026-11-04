package pageimage

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"math"
	"testing"
)

func TestEditsValidate(t *testing.T) {
	ok := []Edits{
		{},
		{Rotation: 270, Brightness: -100, Contrast: 100, Color: ColorBW},
		{Crop: &Crop{X: 0, Y: 0, W: 100, H: 50}},
		{Rotation: 90, Crop: &Crop{X: 0, Y: 0, W: 50, H: 100}},
		{Straighten: -MaxStraighten}, {Straighten: 1.25},
	}
	for _, e := range ok {
		if err := e.Validate(100, 50); err != nil {
			t.Errorf("%+v: unexpected error %v", e, err)
		}
	}
	bad := []Edits{
		{Rotation: 45},
		{Brightness: 101},
		{Contrast: -101},
		{Color: "sepia"},
		{Crop: &Crop{X: 50, Y: 0, W: 51, H: 10}},
		{Crop: &Crop{X: 0, Y: 0, W: 0, H: 10}},
		{Rotation: 90, Crop: &Crop{X: 0, Y: 0, W: 100, H: 50}},
		{Straighten: MaxStraighten + 0.5}, {Straighten: math.NaN()},
	}
	for _, e := range bad {
		if err := e.Validate(100, 50); err == nil {
			t.Errorf("%+v: expected an error", e)
		}
	}
}

func TestEditsIsIdentity(t *testing.T) {
	if !(Edits{}).IsIdentity() || !(Edits{Color: ColorColor}).IsIdentity() {
		t.Error("empty edits and explicit color should be identity")
	}
	for _, e := range []Edits{{Straighten: 0.5}, {Rotation: 90}, {Crop: &Crop{W: 1, H: 1}}, {Brightness: 1}, {Contrast: 1}, {Color: ColorGray}} {
		if e.IsIdentity() {
			t.Errorf("%+v should not be identity", e)
		}
	}
}

func TestRender_CropIsInTheRotatedFrame(t *testing.T) {
	img := solid(40, 20, color.White)
	draw.Draw(img, image.Rect(0, 10, 10, 20), image.Black, image.Point{}, draw.Src) // bottom-left marker

	// Rotated 90° clockwise the page is 20x40 and the marker is top-left;
	// cropping the top 20x30 of what the user sees must keep it.
	out := Render(img, Edits{Rotation: 90, Crop: &Crop{X: 0, Y: 0, W: 20, H: 30}})
	if got := out.Bounds().Size(); got != (image.Point{20, 30}) {
		t.Fatalf("size: got %v, want 20x30", got)
	}
	if r, _, _, _ := out.At(0, 0).RGBA(); r != 0 {
		t.Error("the bottom-left marker should be top-left after a clockwise turn")
	}
	if r, _, _, _ := out.At(15, 25).RGBA(); r == 0 {
		t.Error("only the marker corner should be dark")
	}
}

// tilted draws a scene turned clockwise by degrees about the image center:
// paint gets each pixel's position in the untilted scene, relative to the
// center.
func tilted(w, h int, degrees float64, paint func(x, y float64) color.Color) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	sin, cos := math.Sincos(degrees * math.Pi / 180)
	cx, cy := float64(w)/2, float64(h)/2
	for py := 0; py < h; py++ {
		for px := 0; px < w; px++ {
			dx, dy := float64(px)+0.5-cx, float64(py)+0.5-cy
			img.Set(px, py, paint(dx*cos+dy*sin, -dx*sin+dy*cos))
		}
	}
	return img
}

// barColumn is the mean column of the dark pixels in row y.
func barColumn(img image.Image, y int) float64 {
	b := img.Bounds()
	sum, n := 0.0, 0
	for x := b.Min.X; x < b.Max.X; x++ {
		if r, _, _, _ := img.At(x, y).RGBA(); r < 0x8000 {
			sum += float64(x)
			n++
		}
	}
	return sum / float64(n)
}

func TestRender_StraightenTurnsCounterClockwise(t *testing.T) {
	bar := tilted(200, 200, 5, func(x, y float64) color.Color {
		if math.Abs(x) <= 3 {
			return color.Black
		}
		return color.White
	})
	if lean := barColumn(bar, 20) - barColumn(bar, 180); lean < 10 {
		t.Fatalf("a bar turned clockwise should lean right at the top, leans %.1f px", lean)
	}

	out := Render(bar, Edits{Straighten: 5})
	if got := out.Bounds().Size(); got != (image.Point{200, 200}) {
		t.Fatalf("size: got %v, want the original 200x200", got)
	}
	if lean := barColumn(out, 20) - barColumn(out, 180); math.Abs(lean) > 1.5 {
		t.Errorf("straightening by the tilt should stand the bar upright, it still leans %.1f px", lean)
	}
	if r, g, b, _ := out.At(0, 0).RGBA(); r != 0xffff || g != 0xffff || b != 0xffff {
		t.Error("uncovered corners should be white")
	}
}

func TestEditsWithRotationKeepsTheCroppedArea(t *testing.T) {
	img := solid(40, 20, color.White)
	draw.Draw(img, image.Rect(30, 0, 40, 5), image.Black, image.Point{}, draw.Src) // top-right marker
	e := Edits{Crop: &Crop{X: 25, Y: 0, W: 15, H: 10}}
	want := Render(img, e)

	for _, rot := range []int{90, 180, 270, 0} {
		e = e.WithRotation(rot, 40, 20)
		if err := e.Validate(40, 20); err != nil {
			t.Fatalf("rotation %d: %v", rot, err)
		}
		got := Render(img, Edits{Crop: e.Crop, Rotation: rot}.WithRotation(0, 40, 20))
		if got.Bounds().Size() != want.Bounds().Size() {
			t.Fatalf("rotation %d: crop no longer covers the same area: %v vs %v", rot, got.Bounds(), want.Bounds())
		}
	}
	if *e.Crop != (Crop{X: 25, Y: 0, W: 15, H: 10}) {
		t.Errorf("a full turn should restore the original crop, got %+v", *e.Crop)
	}
	if got := (Edits{}).WithRotation(90, 40, 20); got.Rotation != 90 || got.Crop != nil {
		t.Errorf("rotating without a crop: %+v", got)
	}
}

func TestRender_ToneAndColor(t *testing.T) {
	mid := solid(20, 20, color.RGBA{120, 120, 120, 255})

	brighter := Render(mid, Edits{Brightness: 40})
	if r, _, _, _ := brighter.At(5, 5).RGBA(); r>>8 <= 120 {
		t.Errorf("brightness +40 should lighten, got %d", r>>8)
	}

	gray := Render(mid, Edits{Color: ColorGray})
	if _, ok := gray.(*image.Gray); !ok {
		t.Errorf("gray mode should render *image.Gray, got %T", gray)
	}

	bw := Render(textLike(200, 200), Edits{Color: ColorBW}).(*image.Gray)
	for _, v := range bw.Pix {
		if v != 0 && v != 0xff {
			t.Fatalf("bw mode produced a mid-tone %d", v)
		}
	}

	if out := Render(mid, Edits{Contrast: 50}); out.Bounds() != mid.Bounds() {
		t.Error("contrast should not change size")
	}
	for _, rot := range []int{180, 270} {
		if out := Render(solid(40, 20, color.White), Edits{Rotation: rot}); rot == 270 && out.Bounds().Dx() != 20 {
			t.Errorf("rotation %d: got %v", rot, out.Bounds())
		}
	}
}

func TestOtsuSplitsBimodalPage(t *testing.T) {
	g := image.NewGray(image.Rect(0, 0, 10, 10))
	for i := range g.Pix {
		g.Pix[i] = 30
		if i%2 == 0 {
			g.Pix[i] = 220
		}
	}
	if th := otsu(g); th < 30 || th >= 220 {
		t.Errorf("threshold %d should fall between the two modes", th)
	}
}

func TestFit(t *testing.T) {
	small := solid(100, 100, color.White)
	if Fit(small, 200, 200) != image.Image(small) {
		t.Error("an image within bounds should be returned unchanged")
	}
	if got := Fit(solid(1000, 2000, color.White), 200, 200).Bounds().Size(); got != (image.Point{100, 200}) {
		t.Errorf("got %v, want 100x200", got)
	}
}

func TestCacheKey(t *testing.T) {
	a := CacheKey(Edits{}, "thumb")
	if a != CacheKey(Edits{}, "thumb") {
		t.Error("same edits and size must give the same key")
	}
	if a == CacheKey(Edits{Rotation: 90}, "thumb") || a == CacheKey(Edits{}, "preview") {
		t.Error("different edits or size must give a different key")
	}
}

func TestRenderView(t *testing.T) {
	jpg, err := RenderView(solid(2550, 3300, color.White), Edits{Rotation: 90}, "thumb")
	if err != nil {
		t.Fatal(err)
	}
	img, err := Decode(jpg, 300, 300)
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() > 360 || b.Dy() > 480 || b.Dx() < b.Dy() {
		t.Errorf("rotated thumb should be landscape within 360x480, got %v", b.Size())
	}
	if _, err := RenderView(solid(10, 10, color.White), Edits{}, "poster"); err == nil {
		t.Error("expected an unknown size to fail")
	}
}

func TestRenderView_ShrinksFirstWithoutChangingTheResult(t *testing.T) {
	img := textLike(2400, 3200)
	for _, e := range []Edits{
		{},
		{Straighten: 1.5, Crop: &Crop{X: 100, Y: 120, W: 2000, H: 2800}},
		{Rotation: 90, Crop: &Crop{X: 0, Y: 0, W: 3200, H: 1200}, Color: ColorBW},
	} {
		for size, box := range Sizes {
			data, err := RenderView(img, e, size)
			if err != nil {
				t.Fatal(err)
			}
			got, err := jpeg.DecodeConfig(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			want := Fit(Render(img, e), box.X, box.Y).Bounds()
			if abs(got.Width-want.Dx()) > 2 || abs(got.Height-want.Dy()) > 2 {
				t.Errorf("%+v at %s: %dx%d, want %dx%d", e, size, got.Width, got.Height, want.Dx(), want.Dy())
			}
		}
	}
}

func abs(v int) int { return max(v, -v) }
