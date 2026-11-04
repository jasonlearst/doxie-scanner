package pageimage

import (
	"image"
	"image/color"
	"math"
	"testing"
)

// Measured on DX400 scans: the backing beside a narrow sheet, the shadow
// along the sheet's edges, and the dark line across the first rows.
var (
	backing   = color.NRGBA{176, 178, 177, 255}
	shadow    = color.NRGBA{167, 165, 162, 255}
	startLine = color.NRGBA{77, 71, 54, 255}
	white     = color.NRGBA{240, 240, 238, 255}
	canary    = color.NRGBA{226, 211, 159, 255}
)

// sheet is a page lying on the scanner, sizes in pixels.
type sheet struct {
	w, h   float64
	dx     float64 // its center's offset from the scan's center
	tilt   float64 // degrees clockwise
	paper  color.NRGBA
	inkBar bool // a solid bar of ink along its left edge
	blank  bool // no text
}

// scanOf renders what the scanner would deliver for s: the scan is as
// wide as the scanner and just tall enough for the tilted sheet.
func scanOf(width int, s sheet) (*image.NRGBA, image.Rectangle) {
	sin, cos := math.Sincos(math.Abs(s.tilt) * math.Pi / 180)
	height := int(s.w*sin+s.h*cos) + 8
	img := tilted(width, height, s.tilt, func(x, y float64) color.Color {
		x -= s.dx
		ax, ay := math.Abs(x), math.Abs(y)
		switch {
		case ax <= s.w/2 && ay <= s.h/2:
			fromLeft, fromTop := x+s.w/2, y+s.h/2
			if s.inkBar && fromLeft < 30 {
				return color.Black
			}
			if !s.blank && fromLeft > 75 && fromLeft < s.w-75 && fromTop > 75 && fromTop < s.h-75 && int(fromTop)%24 < 5 {
				return color.NRGBA{20, 20, 20, 255}
			}
			return s.paper
		case ax <= s.w/2+3 && ay <= s.h/2+3:
			return shadow
		}
		return backing
	})
	for y := 0; y < 4; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, startLine)
		}
	}
	for i := range img.Pix {
		if i%4 != 3 {
			img.Pix[i] = uint8(max(0, min(255, int(img.Pix[i])+(i*7919%7)-3)))
		}
	}
	cx, cy := float64(width)/2+s.dx, float64(height)/2
	want := image.Rect(int(cx-s.w/2), int(cy-s.h/2), int(cx+s.w/2), int(cy+s.h/2))
	return img, want
}

func near(got, want, tolerance int) bool { return got >= want-tolerance && got <= want+tolerance }

func checkCrop(t *testing.T, name string, got Crop, want image.Rectangle, tolerance int) {
	t.Helper()
	g := image.Rect(got.X, got.Y, got.X+got.W, got.Y+got.H)
	if !near(g.Min.X, want.Min.X, tolerance) || !near(g.Max.X, want.Max.X, tolerance) ||
		!near(g.Min.Y, want.Min.Y, tolerance) || !near(g.Max.Y, want.Max.Y, tolerance) {
		t.Errorf("%s: crop %v, want the sheet at %v (±%d)", name, g, want, tolerance)
	}
}

func TestDetectPaper_CropsANarrowSheetToItsEdges(t *testing.T) {
	for name, paper := range map[string]color.NRGBA{"white": white, "canary": canary} {
		img, want := scanOf(1275, sheet{w: 1200, h: 1500, dx: 4, paper: paper})
		got, ok := DetectPaper(img)
		if !ok {
			t.Fatalf("%s: no paper found", name)
		}
		if got.Straighten != 0 {
			t.Errorf("%s: a straight sheet needs no straightening, got %.2f°", name, got.Straighten)
		}
		checkCrop(t, name, got.Crop, want, 6)
	}
}

func TestDetectPaper_StraightensATiltedSheet(t *testing.T) {
	for _, tilt := range []float64{1.5, -2, 0.6, -3.5} {
		img, want := scanOf(1275, sheet{w: 1150, h: 1500, tilt: tilt, paper: white})
		got, ok := DetectPaper(img)
		if !ok {
			t.Fatalf("%.1f°: no paper found", tilt)
		}
		if math.Abs(got.Straighten-tilt) > 0.15 {
			t.Errorf("%.1f°: straighten %.2f°, want %.1f°", tilt, got.Straighten, tilt)
		}
		checkCrop(t, "tilted", got.Crop, want, 8)

		out := Render(img, Edits{Straighten: got.Straighten, Crop: &got.Crop})
		if n := backingAlongEdges(out, 10); n > 0 {
			t.Errorf("%.1f°: %d pixels of backing or filler left along the crop's edges", tilt, n)
		}
	}
}

// backingAlongEdges counts pixels in the band of the given width along
// img's edges that are darker than the paper: backing, shadow, or ink.
// The synthetic sheets keep ink well inside that band.
func backingAlongEdges(img image.Image, band int) int {
	n := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if x >= b.Min.X+band && x < b.Max.X-band && y >= b.Min.Y+band && y < b.Max.Y-band {
				continue
			}
			r, g, bl, _ := img.At(x, y).RGBA()
			if luma(uint8(r>>8), uint8(g>>8), uint8(bl>>8)) < 215 {
				n++
			}
		}
	}
	return n
}

func TestDetectPaper_Receipt(t *testing.T) {
	img, want := scanOf(1275, sheet{w: 450, h: 1400, dx: -120, tilt: 1, paper: white})
	got, ok := DetectPaper(img)
	if !ok || math.Abs(got.Straighten-1) > 0.2 {
		t.Fatalf("got %+v, %v", got, ok)
	}
	checkCrop(t, "receipt", got.Crop, want, 8)
}

func TestDetectPaper_InkAtTheSheetsEdgeIsKept(t *testing.T) {
	img, want := scanOf(1275, sheet{w: 1150, h: 1500, paper: white, inkBar: true})
	got, ok := DetectPaper(img)
	if !ok {
		t.Fatal("no paper found")
	}
	if got.Crop.X > want.Min.X+4 {
		t.Errorf("crop starts at %d, inside the ink along the sheet's edge at %d", got.Crop.X, want.Min.X)
	}
}

func TestDetectPaper_FullWidthSheetKeepsItsSides(t *testing.T) {
	img, _ := scanOf(1275, sheet{w: 1300, h: 1500, paper: white})
	got, ok := DetectPaper(img)
	if ok && (got.Crop.X > 0 || got.Crop.X+got.Crop.W < 1275 || got.Straighten != 0) {
		t.Errorf("a sheet filling the scanner's width should keep its sides, got %+v", got)
	}
}

func TestDetectPaper_TooTiltedIsCroppedButNotStraightened(t *testing.T) {
	img, _ := scanOf(1275, sheet{w: 1000, h: 1300, tilt: 8, paper: white})
	got, ok := DetectPaper(img)
	if !ok {
		t.Fatal("no paper found")
	}
	if got.Straighten != 0 {
		t.Errorf("8° is beyond a feeding error; got straighten %.2f°", got.Straighten)
	}
	out := Render(img, Edits{Crop: &got.Crop})
	sin, cos := math.Sincos(8 * math.Pi / 180)
	if b := out.Bounds(); b.Dx() < int(1000*cos+1300*sin)-8 {
		t.Errorf("the crop %v must keep the whole tilted sheet", b)
	}
}

func TestDetectPaper_NothingToFind(t *testing.T) {
	if _, ok := DetectPaper(solid(1275, 1650, white)); ok {
		t.Error("a scan with no backing showing has nothing to crop")
	}
	if _, ok := DetectPaper(solid(1275, 1650, backing)); ok {
		t.Error("an empty scanner has no paper to crop to")
	}
}

func TestIsBlankPaper_IgnoresTheBackingBesideTheSheet(t *testing.T) {
	blank, _ := scanOf(1275, sheet{w: 1150, h: 1500, tilt: 1, paper: canary, blank: true})
	printed, _ := scanOf(1275, sheet{w: 1150, h: 1500, tilt: 1, paper: canary})
	if IsBlank(blank) {
		t.Fatal("the backing strips should make the whole scan look printed, or this test proves nothing")
	}
	for name, c := range map[string]struct {
		img  *image.NRGBA
		want bool
	}{"blank sheet": {blank, true}, "printed sheet": {printed, false}} {
		p, ok := DetectPaper(c.img)
		if !ok {
			t.Fatalf("%s: no paper found", name)
		}
		e := p.Apply(Edits{}, c.img.Rect.Dx(), c.img.Rect.Dy())
		if got := IsBlankPaper(c.img, e); got != c.want {
			t.Errorf("%s: blank %v, want %v", name, got, c.want)
		}
	}
}

func TestPaperApply_PutsTheCropInTheRotatedFrame(t *testing.T) {
	p := Paper{Straighten: 1.2, Crop: Crop{X: 10, Y: 20, W: 100, H: 200}}
	e := p.Apply(Edits{Rotation: 90, Brightness: 10}, 300, 400)
	if e.Straighten != 1.2 || e.Brightness != 10 || e.Rotation != 90 {
		t.Errorf("other edits must be kept: %+v", e)
	}
	if want := (Crop{X: 180, Y: 10, W: 200, H: 100}); e.Crop == nil || *e.Crop != want {
		t.Errorf("crop %+v, want %+v for a page turned 90°", e.Crop, want)
	}
	if err := e.Validate(300, 400); err != nil {
		t.Error(err)
	}
}
