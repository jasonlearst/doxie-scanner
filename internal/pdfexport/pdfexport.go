// Package pdfexport assembles pages into a PDF, each page sized to its
// real physical dimensions. It knows nothing about jobs or storage.
package pdfexport

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"

	"github.com/go-pdf/fpdf"
)

// JPEGQuality is used for pages that had to be re-rendered (edited).
const JPEGQuality = 90

// Page is one encoded PDF page. Pages hold compressed bytes, not decoded
// images, so a long 600 dpi job doesn't need every page in memory at once.
type Page struct {
	Data     []byte
	Format   string // "JPEG" or "PNG"
	WidthIn  float64
	HeightIn float64
	// Place positions the image on the page; nil fills the page with it.
	Place *Placement
}

// Affine maps image pixels (x right, y down) to page inches (x right, y
// down from the top-left corner): (x, y) → (A·x + C·y + E, B·x + D·y + F).
type Affine struct{ A, B, C, D, E, F float64 }

// Placement puts an image of W x H pixels onto its page through Map, so
// the PDF crops and turns it instead of the pixels being redrawn. Whatever
// falls outside the page is cut off.
type Placement struct {
	W, H int
	Map  Affine
}

// PlacedJPEG embeds a JPEG byte for byte and lets the PDF crop and turn
// it, so a cropped or straightened scan loses nothing either.
func PlacedJPEG(jpg []byte, widthIn, heightIn float64, place Placement) Page {
	return Page{Data: jpg, Format: "JPEG", WidthIn: widthIn, HeightIn: heightIn, Place: &place}
}

// PlacedImage encodes img like FromImage and places it like PlacedJPEG.
func PlacedImage(img image.Image, widthIn, heightIn float64, place Placement) (Page, error) {
	p, err := FromImage(img, widthIn, heightIn)
	p.Place = &place
	return p, err
}

// FromJPEG embeds a JPEG byte for byte, so an unedited scan never loses
// quality.
func FromJPEG(jpg []byte, widthIn, heightIn float64) Page {
	return Page{Data: jpg, Format: "JPEG", WidthIn: widthIn, HeightIn: heightIn}
}

var (
	pngEncode  = encodeBilevel
	jpegEncode = func(w io.Writer, img image.Image) error {
		return jpeg.Encode(w, img, &jpeg.Options{Quality: JPEGQuality})
	}
)

// FromImage encodes an edited page right away: pure black & white as a
// 1-bit PNG (tiny, crisp for OCR, and quick to compress), anything else as
// JPEG.
func FromImage(img image.Image, widthIn, heightIn float64) (Page, error) {
	var buf bytes.Buffer
	encode, format := jpegEncode, "JPEG"
	if bilevel(img) {
		encode, format = pngEncode, "PNG"
	}
	if err := encode(&buf, img); err != nil {
		return Page{}, fmt.Errorf("pdfexport: encode page: %w", err)
	}
	return Page{Data: buf.Bytes(), Format: format, WidthIn: widthIn, HeightIn: heightIn}, nil
}

// Build assembles pages, in order, into one PDF.
func Build(pages []Page) ([]byte, error) {
	if len(pages) == 0 {
		return nil, fmt.Errorf("pdfexport: no pages given")
	}

	pdf := fpdf.New("P", "in", "Letter", "")
	pdf.SetMargins(0, 0, 0)
	pdf.SetAutoPageBreak(false, 0)

	for i, p := range pages {
		if p.WidthIn <= 0 || p.HeightIn <= 0 || len(p.Data) == 0 {
			return nil, fmt.Errorf("pdfexport: page %d is empty or has no size", i+1)
		}
		pdf.AddPageFormat("P", fpdf.SizeType{Wd: p.WidthIn, Ht: p.HeightIn})
		name := fmt.Sprintf("page%d", i)
		opts := fpdf.ImageOptions{ImageType: p.Format}
		pdf.RegisterImageOptionsReader(name, opts, bytes.NewReader(p.Data))
		if p.Place == nil {
			pdf.ImageOptions(name, 0, 0, p.WidthIn, p.HeightIn, false, opts, 0, "")
			continue
		}
		// The image is drawn as a 1pt square at the bottom-left corner,
		// which the transform then stretches and turns into place.
		pdf.TransformBegin()
		pdf.Transform(p.Place.pdfMatrix(p.HeightIn))
		pdf.ImageOptions(name, 0, p.HeightIn-1.0/pointsPerInch, 1.0/pointsPerInch, 1.0/pointsPerInch, false, opts, 0, "")
		pdf.TransformEnd()
	}

	if err := pdf.Error(); err != nil {
		return nil, fmt.Errorf("pdfexport: %w", err)
	}
	var out bytes.Buffer
	if err := pdf.Output(&out); err != nil {
		return nil, fmt.Errorf("pdfexport: output: %w", err)
	}
	return out.Bytes(), nil
}

const pointsPerInch = 72

// pdfMatrix maps the image's unit square, as PDF draws images, onto the
// page in points with the origin at the bottom left.
func (pl Placement) pdfMatrix(pageHeightIn float64) fpdf.TransformMatrix {
	at := func(u, v float64) (float64, float64) {
		x, y := u*float64(pl.W), (1-v)*float64(pl.H)
		m := pl.Map
		px, py := m.A*x+m.C*y+m.E, m.B*x+m.D*y+m.F
		return px * pointsPerInch, (pageHeightIn - py) * pointsPerInch
	}
	ox, oy := at(0, 0)
	ux, uy := at(1, 0)
	vx, vy := at(0, 1)
	return fpdf.TransformMatrix{A: ux - ox, B: uy - oy, C: vx - ox, D: vy - oy, E: ox, F: oy}
}

// encodeBilevel writes a black & white page as a 1-bit PNG: an eighth of
// the data an 8-bit one has to compress.
func encodeBilevel(w io.Writer, img image.Image) error {
	g := img.(*image.Gray)
	p := image.NewPaletted(g.Rect, color.Palette{color.Black, color.White})
	for y := 0; y < g.Rect.Dy(); y++ {
		for x := 0; x < g.Rect.Dx(); x++ {
			if g.Pix[y*g.Stride+x] != 0 {
				p.Pix[y*p.Stride+x] = 1
			}
		}
	}
	return png.Encode(w, p)
}

func bilevel(img image.Image) bool {
	g, ok := img.(*image.Gray)
	if !ok {
		return false
	}
	for _, v := range g.Pix {
		if v != 0 && v != 0xff {
			return false
		}
	}
	return true
}
