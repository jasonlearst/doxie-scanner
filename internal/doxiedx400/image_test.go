package doxiedx400

import (
	"image"
	"image/color"
	"image/draw"
	"testing"
)

func TestIsBlank(t *testing.T) {
	white := image.NewNRGBA(image.Rect(0, 0, 400, 400))
	draw.Draw(white, white.Bounds(), image.White, image.Point{}, draw.Src)
	if !isBlank(white) {
		t.Error("an all-white page should be blank")
	}

	inked := image.NewNRGBA(white.Bounds())
	draw.Draw(inked, inked.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(inked, image.Rect(0, 0, 400, 40), image.Black, image.Point{}, draw.Src)
	if isBlank(inked) {
		t.Error("a page with a 10% ink band should not be blank")
	}

	faint := image.NewNRGBA(white.Bounds())
	draw.Draw(faint, faint.Bounds(), &image.Uniform{color.Gray{Y: 210}}, image.Point{}, draw.Src)
	if !isBlank(faint) {
		t.Error("uniform show-through (gray 210) should still count as blank")
	}

	if !isBlank(image.NewNRGBA(image.Rectangle{})) {
		t.Error("an empty image should be blank")
	}
}

func TestDecodeSide_SquaresUp200dpi(t *testing.T) {
	jpg := wrapJPEG(2550, 32, quantTables[300], fixture(t, "ink-300.scan"))

	w300, _ := newWindow(300, true)
	img, err := decodeSide(jpg, w300)
	if err != nil {
		t.Fatal(err)
	}
	if got := img.Bounds().Size(); got != (image.Point{2550, 32}) {
		t.Errorf("300 dpi: got %v", got)
	}

	w200, _ := newWindow(200, true)
	img, err = decodeSide(jpg, w200)
	if err != nil {
		t.Fatal(err)
	}
	if got := img.Bounds().Size(); got != (image.Point{1700, 32}) {
		t.Errorf("200 dpi should be resampled from 300 across: got %v", got)
	}
}

func TestDecodeSide_RejectsGarbage(t *testing.T) {
	w, _ := newWindow(300, false)
	if _, err := decodeSide([]byte("not a jpeg"), w); err == nil {
		t.Fatal("expected a decode error")
	}
}

func TestBlankFixturesClassifyCorrectly(t *testing.T) {
	w, _ := newWindow(300, false)
	for name, wantBlank := range map[string]bool{"blank-300.scan": true, "ink-300.scan": false} {
		img, err := decodeSide(wrapJPEG(2550, 32, quantTables[300], fixture(t, name)), w)
		if err != nil {
			t.Fatal(err)
		}
		if got := isBlank(img); got != wantBlank {
			t.Errorf("%s: isBlank=%v, want %v", name, got, wantBlank)
		}
	}
}
