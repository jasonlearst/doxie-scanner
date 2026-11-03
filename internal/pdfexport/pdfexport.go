// Package pdfexport assembles pages into a PDF, each page sized to its
// real physical dimensions. It knows nothing about jobs or storage.
package pdfexport

import (
	"bytes"
	"fmt"
	"image"
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
}

// FromJPEG embeds a JPEG byte for byte, so an unedited scan never loses
// quality.
func FromJPEG(jpg []byte, widthIn, heightIn float64) Page {
	return Page{Data: jpg, Format: "JPEG", WidthIn: widthIn, HeightIn: heightIn}
}

var (
	pngEncode  = png.Encode
	jpegEncode = func(w io.Writer, img image.Image) error {
		return jpeg.Encode(w, img, &jpeg.Options{Quality: JPEGQuality})
	}
)

// FromImage encodes an edited page right away: pure black & white as
// lossless PNG (tiny, and crisp for OCR), anything else as JPEG.
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
		pdf.ImageOptions(name, 0, 0, p.WidthIn, p.HeightIn, false, opts, 0, "")
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
