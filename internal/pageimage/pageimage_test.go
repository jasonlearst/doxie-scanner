package pageimage

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"testing"
)

func solid(w, h int, c color.Color) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{c}, image.Point{}, draw.Src)
	return img
}

func textLike(w, h int) *image.NRGBA {
	img := solid(w, h, color.White)
	for y := 10; y < h; y += 30 {
		draw.Draw(img, image.Rect(20, y, w-20, y+8), image.Black, image.Point{}, draw.Src)
	}
	return img
}

func encodeJPEG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestDecode(t *testing.T) {
	jpg := encodeJPEG(t, solid(300, 100, color.White))

	img, err := Decode(jpg, 300, 300)
	if err != nil {
		t.Fatal(err)
	}
	if got := img.Bounds().Size(); got != (image.Point{300, 100}) {
		t.Errorf("square: got %v", got)
	}

	img, err = Decode(jpg, 300, 200)
	if err != nil {
		t.Fatal(err)
	}
	if got := img.Bounds().Size(); got != (image.Point{200, 100}) {
		t.Errorf("300x200 dpi should be squared to 200 across: got %v", got)
	}

	if _, err := Decode([]byte("nope"), 300, 300); err == nil {
		t.Error("expected a decode error")
	}
}

func TestIsBlank(t *testing.T) {
	cases := map[string]struct {
		img  image.Image
		want bool
	}{
		"white":        {solid(800, 1000, color.White), true},
		"show-through": {solid(800, 1000, color.Gray{Y: 210}), true},
		"text":         {textLike(800, 1000), false},
		"empty":        {image.NewNRGBA(image.Rectangle{}), true},
	}
	for name, c := range cases {
		if got := IsBlank(c.img); got != c.want {
			t.Errorf("%s: got %v, want %v", name, got, c.want)
		}
	}
}

func TestDetectColorMode(t *testing.T) {
	gradient := image.NewNRGBA(image.Rect(0, 0, 256, 200))
	for x := 0; x < 256; x++ {
		draw.Draw(gradient, image.Rect(x, 0, x+1, 200), &image.Uniform{color.Gray{Y: uint8(x)}}, image.Point{}, draw.Src)
	}
	logo := textLike(800, 1000)
	draw.Draw(logo, image.Rect(600, 20, 700, 120), &image.Uniform{color.RGBA{200, 30, 30, 255}}, image.Point{}, draw.Src)

	cases := map[string]struct {
		img  image.Image
		want ColorMode
	}{
		"text":       {textLike(800, 1000), ColorBW},
		"gradient":   {gradient, ColorGray},
		"red logo":   {logo, ColorColor},
		"empty":      {image.NewNRGBA(image.Rectangle{}), ColorBW},
		"paper tint": {solid(800, 1000, color.RGBA{245, 238, 220, 255}), ColorBW},
	}
	for name, c := range cases {
		if got := DetectColorMode(c.img); got != c.want {
			t.Errorf("%s: got %q, want %q", name, got, c.want)
		}
	}
}

func TestDetectColorMode_IgnoresFringesOnTextEdges(t *testing.T) {
	img := textLike(800, 1000)
	for y := 10; y < 1000; y += 30 {
		draw.Draw(img, image.Rect(20, y-1, 780, y), &image.Uniform{color.RGBA{255, 0, 0, 255}}, image.Point{}, draw.Src)
		draw.Draw(img, image.Rect(20, y+8, 780, y+9), &image.Uniform{color.RGBA{0, 0, 255, 255}}, image.Point{}, draw.Src)
	}
	if got := DetectColorMode(img); got != ColorBW {
		t.Errorf("scanner color fringes on black text should stay bw, got %q", got)
	}
}

func TestDetectColorMode_SmallGrayPatchCounts(t *testing.T) {
	img := textLike(2550, 3300)
	draw.Draw(img, image.Rect(200, 2000, 1400, 2150), &image.Uniform{color.Gray{Y: 128}}, image.Point{}, draw.Src)
	if got := DetectColorMode(img); got != ColorGray {
		t.Errorf("a gray-step strip like the test page's should make it gray, got %q", got)
	}
}
