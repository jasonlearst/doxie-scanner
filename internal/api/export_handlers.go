package api

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"net/http"
	"regexp"
	"strings"

	"github.com/disintegration/imaging"

	"github.com/andiapps-dev/doxie-scanner/internal/pageimage"
	"github.com/andiapps-dev/doxie-scanner/internal/pdfexport"
	"github.com/andiapps-dev/doxie-scanner/internal/storage"
)

// buildPDF assembles pages into a PDF at their physical size, one page at
// a time so memory stays flat on a Raspberry Pi.
func (s *Server) buildPDF(jobID string, pages []storage.PageMeta) ([]byte, error) {
	out := make([]pdfexport.Page, 0, len(pages))
	for _, p := range pages {
		page, err := s.pdfPage(jobID, p)
		if err != nil {
			return nil, fmt.Errorf("page %s: %w", p.ID, err)
		}
		out = append(out, page)
	}
	return pdfexport.Build(out)
}

// pdfPage leaves cropping, straightening and turning to the PDF. A page
// whose pixels keep their values goes in as the scanner's JPEG, byte for
// byte, with no decoding or recompressing. Only tone and color edits
// redraw pixels, and then just the part of the scan that shows.
func (s *Server) pdfPage(jobID string, p storage.PageMeta) (pdfexport.Page, error) {
	dpi := float64(p.YDPI)
	toInches := pageimage.Scale(1/dpi, 1/dpi)
	m, outW, outH := pageimage.Geometry(p.Edits, p.WidthPx, p.HeightPx)
	wIn, hIn := float64(outW)/dpi, float64(outH)/dpi

	if !p.Edits.NeedsPixels() {
		original, err := s.store.LoadPageFile(jobID, p.File)
		if err != nil {
			return pdfexport.Page{}, err
		}
		if p.Edits.IsIdentity() {
			return pdfexport.FromJPEG(original, wIn, hIn), nil
		}
		cfg, err := jpeg.DecodeConfig(bytes.NewReader(original))
		if err != nil {
			return pdfexport.Page{}, err
		}
		// At 200 dpi the scanner's pixels aren't square; Geometry works in
		// square ones.
		toSquare := pageimage.Scale(float64(p.WidthPx)/float64(cfg.Width), float64(p.HeightPx)/float64(cfg.Height))
		return pdfexport.PlacedJPEG(original, wIn, hIn, placement(cfg.Width, cfg.Height, toSquare.Then(m).Then(toInches))), nil
	}

	var page pdfexport.Page
	err := s.withDecodedPage(jobID, p, func(img image.Image) error {
		shown := pageimage.Visible(m, outW, outH, p.WidthPx, p.HeightPx)
		part := imaging.Crop(img, shown.Add(img.Bounds().Min))
		tone := pageimage.Edits{Brightness: p.Edits.Brightness, Contrast: p.Edits.Contrast, Color: p.Edits.Color}
		fromPart := pageimage.Translate(float64(shown.Min.X), float64(shown.Min.Y)).Then(m).Then(toInches)
		var err error
		page, err = pdfexport.PlacedImage(pageimage.Render(part, tone), wIn, hIn, placement(shown.Dx(), shown.Dy(), fromPart))
		return err
	})
	return page, err
}

func placement(w, h int, m pageimage.Affine) pdfexport.Placement {
	return pdfexport.Placement{W: w, H: h, Map: pdfexport.Affine{A: m.A, B: m.B, C: m.C, D: m.D, E: m.E, F: m.F}}
}

// selectPages returns the pages named in ids, in that order, or every
// visible page when ids is empty.
func selectPages(meta storage.JobMeta, ids []string) ([]storage.PageMeta, error) {
	if len(ids) == 0 {
		return meta.VisiblePages(), nil
	}
	pages := make([]storage.PageMeta, 0, len(ids))
	for _, id := range ids {
		p, _, ok := meta.FindPage(id)
		if !ok {
			return nil, fmt.Errorf("page %q not found", id)
		}
		pages = append(pages, p)
	}
	return pages, nil
}

var unsafeFilename = regexp.MustCompile(`[^A-Za-z0-9._ -]+`)

func pdfFilename(name string) string {
	clean := strings.TrimSpace(unsafeFilename.ReplaceAllString(name, "-"))
	if clean == "" {
		clean = "scan"
	}
	return clean + ".pdf"
}

// handleJobPDF downloads a job as a PDF: every visible page by default,
// or ?pages=001f,002f for a specific selection and order.
func (s *Server) handleJobPDF(w http.ResponseWriter, r *http.Request) {
	meta, err := s.store.LoadMeta(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "job not found")
		return
	}
	var ids []string
	if q := r.URL.Query().Get("pages"); q != "" {
		ids = strings.Split(q, ",")
	}
	pages, err := selectPages(meta, ids)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if len(pages) == 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "no pages to export")
		return
	}
	pdf, err := s.buildPDF(meta.ID, pages)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", pdfFilename(meta.Name)))
	w.Write(pdf)
}
