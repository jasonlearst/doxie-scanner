package pageimage

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"testing"
)

// A short 300 dpi sheet on slightly tinted stock whose back carries one
// small block of print in a corner: about 1.5% of the page as IsBlankPaper
// measures it, about 31% in its densest half-inch.
func sparseBack(ink color.Color) *image.NRGBA {
	img := solid(2550, 1100, color.Gray{Y: 225})
	for y := 120; y < 340; y += 24 {
		draw.Draw(img, image.Rect(1800, y, 2350, y+5), &image.Uniform{ink}, image.Point{}, draw.Src)
	}
	return img
}

func TestIsBlankPaper_SparsePrintIsNotBlank(t *testing.T) {
	if IsBlankPaper(sparseBack(color.Black), Edits{}) {
		t.Error("a back with a block of print in one corner was judged blank")
	}
}

func TestIsBlankPaper_FaintShowThroughStaysBlank(t *testing.T) {
	if !IsBlankPaper(sparseBack(color.Gray{Y: 205}), Edits{}) {
		t.Error("faint show-through was judged printed")
	}
}

func TestMeasureInk_DensestFindsPrintTheSheetAverageMisses(t *testing.T) {
	ink := MeasureInk(sparseBack(color.Black), Edits{})
	if ink.Sheet >= blankSheetThreshold {
		t.Fatalf("sheet %v: the whole-sheet share should miss this print, or this test proves nothing", ink.Sheet)
	}
	if ink.Densest < 0.15 {
		t.Errorf("densest %v, want the block's own share of about 0.2", ink.Densest)
	}
	if ink.Densest != round4(ink.Densest) || ink.Sheet != round4(ink.Sheet) {
		t.Errorf("measurements are stored rounded: %+v", ink)
	}
}

// A quarter-inch patch of print, a fifteenth of a half-inch square. A fixed
// grid of half-inch squares splits it at some offsets and leaves a quarter
// of it in each.
func TestMeasureInk_PrintCountsWhereverItFalls(t *testing.T) {
	for off := 0; off <= 150; off += 26 {
		img := solid(2550, 1100, color.White)
		for y := 400 + off; y < 475+off; y += 15 {
			draw.Draw(img, image.Rect(400+off, y, 475+off, y+4), image.Black, image.Point{}, draw.Src)
		}
		if ink := MeasureInk(img, Edits{}); ink.Blank() {
			t.Errorf("offset %d: judged blank, %+v", off, ink)
		}
	}
}

func TestMeasureInk_IgnoresASliverAlongTheSheetsEdges(t *testing.T) {
	img := solid(2550, 1100, color.White)
	draw.Draw(img, image.Rect(0, 0, 2550, 10), image.Black, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(0, 0, 10, 1100), image.Black, image.Point{}, draw.Src)
	if ink := MeasureInk(img, Edits{}); !ink.Blank() {
		t.Errorf("a sliver of backing left by the crop was judged print: %+v", ink)
	}
}

func TestMeasureInk_MeasuresOnlyTheCrop(t *testing.T) {
	img := sparseBack(color.Black)
	if ink := MeasureInk(img, Edits{Crop: &Crop{X: 0, Y: 0, W: 1700, H: 1100}}); ink.Densest != 0 {
		t.Errorf("print outside the crop was counted: %+v", ink)
	}
	if ink := MeasureInk(img, Edits{Crop: &Crop{X: 1800, Y: 120, W: 40, H: 40}}); ink.Densest != 0 {
		t.Errorf("a crop inside the margins has no squares to measure: %+v", ink)
	}
}

func TestMeasureInk_ReadsEveryKindOfImage(t *testing.T) {
	src := sparseBack(color.Black)
	want := MeasureInk(src, Edits{})

	decoded, err := Decode(encodeJPEG(t, src), 300, 300)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded.(*image.YCbCr); !ok {
		t.Fatalf("decoded to %T, want the JPEG's own planes", decoded)
	}
	gray := image.NewGray(src.Rect)
	draw.Draw(gray, gray.Rect, src, image.Point{}, draw.Src)

	for name, img := range map[string]image.Image{"jpeg": decoded, "gray": gray} {
		got := MeasureInk(img, Edits{})
		if math.Abs(got.Densest-want.Densest) > 0.02 || math.Abs(got.Sheet-want.Sheet) > 0.005 {
			t.Errorf("%s: %+v, want about %+v", name, got, want)
		}
	}
}
