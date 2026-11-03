package api

import (
	"fmt"
	"image"
	"net/http"
	"regexp"
	"strings"

	"github.com/andiapps-dev/doxie-scanner/internal/pageimage"
	"github.com/andiapps-dev/doxie-scanner/internal/pdfexport"
	"github.com/andiapps-dev/doxie-scanner/internal/storage"
)

// buildPDF assembles pages into a PDF at their physical size. Unedited
// pages carry the scanner's JPEG byte for byte; edited ones are rendered
// and encoded one at a time so memory stays flat on a Raspberry Pi.
func (s *Server) buildPDF(jobID string, pages []storage.PageMeta) ([]byte, error) {
	out := make([]pdfexport.Page, 0, len(pages))
	for _, p := range pages {
		dpi := float64(p.YDPI)
		if p.Edits.IsIdentity() {
			original, err := s.store.LoadPageFile(jobID, p.File)
			if err != nil {
				return nil, err
			}
			out = append(out, pdfexport.FromJPEG(original, float64(p.WidthPx)/dpi, float64(p.HeightPx)/dpi))
			continue
		}
		err := s.withDecodedPage(jobID, p, func(img image.Image) error {
			rendered := pageimage.Render(img, p.Edits)
			b := rendered.Bounds()
			page, err := pdfexport.FromImage(rendered, float64(b.Dx())/dpi, float64(b.Dy())/dpi)
			out = append(out, page)
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("page %s: %w", p.ID, err)
		}
	}
	return pdfexport.Build(out)
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
