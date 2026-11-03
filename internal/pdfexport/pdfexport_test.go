package pdfexport

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"io"
	"testing"
)

func solid(w, h int, c color.Color) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{c}, image.Point{}, draw.Src)
	return img
}

func scannedJPEG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, solid(85, 110, color.RGBA{200, 180, 160, 255}), nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func bwPage(w, h int) *image.Gray {
	g := image.NewGray(image.Rect(0, 0, w, h))
	for i := range g.Pix {
		if (i/w)%10 < 3 {
			g.Pix[i] = 0
		} else {
			g.Pix[i] = 0xff
		}
	}
	return g
}

func TestBuild_EmbedsOriginalJPEGUntouched(t *testing.T) {
	jpg := scannedJPEG(t)
	pdf, err := Build([]Page{FromJPEG(jpg, 8.5, 11)})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(pdf, jpg) {
		t.Error("an unedited page must be embedded byte for byte")
	}
	if !bytes.Contains(pdf, []byte("/MediaBox [0 0 612.00 792.00]")) {
		t.Error("an 8.5x11in page should be 612x792pt")
	}
}

func TestBuild_EncodesEditedPagesByContent(t *testing.T) {
	gray := image.NewGray(image.Rect(0, 0, 50, 50))
	gray.Pix[0] = 100
	cases := map[string]struct {
		img    image.Image
		filter string
	}{
		"black & white": {bwPage(200, 200), "/FlateDecode"},
		"gray":          {gray, "/DCTDecode"},
		"color":         {solid(50, 50, color.RGBA{10, 200, 30, 255}), "/DCTDecode"},
	}
	for name, c := range cases {
		page, err := FromImage(c.img, 1, 1)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		pdf, err := Build([]Page{page})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !bytes.Contains(pdf, []byte(c.filter)) {
			t.Errorf("%s: expected %s", name, c.filter)
		}
	}
}

func TestBuild_MultiplePagesKeepOrderAndSize(t *testing.T) {
	bw, err := FromImage(bwPage(100, 50), 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	pdf, err := Build([]Page{FromJPEG(scannedJPEG(t), 8.5, 11), bw})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/Count 2", "/MediaBox [0 0 612.00 792.00]", "/MediaBox [0 0 144.00 72.00]"} {
		if !bytes.Contains(pdf, []byte(want)) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestBuild_Errors(t *testing.T) {
	cases := map[string][]Page{
		"no pages": nil,
		"no size":  {FromJPEG(scannedJPEG(t), 0, 0)},
		"no data":  {{Format: "JPEG", WidthIn: 1, HeightIn: 1}},
		"bad JPEG": {FromJPEG([]byte("not a jpeg"), 1, 1)},
	}
	for name, pages := range cases {
		if _, err := Build(pages); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestBuild_EncodeErrors(t *testing.T) {
	fail := func(io.Writer, image.Image) error { return errors.New("encode failed") }
	origPNG, origJPEG := pngEncode, jpegEncode
	pngEncode, jpegEncode = fail, fail
	defer func() { pngEncode, jpegEncode = origPNG, origJPEG }()

	for _, img := range []image.Image{bwPage(10, 10), solid(10, 10, color.White)} {
		if _, err := FromImage(img, 1, 1); err == nil {
			t.Errorf("%T: expected the encode error to surface", img)
		}
	}
}

func TestBilevel(t *testing.T) {
	if !bilevel(bwPage(10, 10)) {
		t.Error("pure black & white gray image is bilevel")
	}
	g := bwPage(10, 10)
	g.Pix[3] = 128
	if bilevel(g) || bilevel(solid(2, 2, color.White)) {
		t.Error("mid-tones or non-gray images are not bilevel")
	}
}
