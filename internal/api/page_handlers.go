package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"net/http"
	"slices"

	"github.com/andiapps-dev/doxie-scanner/internal/pageimage"
	"github.com/andiapps-dev/doxie-scanner/internal/storage"
)

// loadPage resolves {id} and {page}, writing a 404 if either is unknown.
func (s *Server) loadPage(w http.ResponseWriter, r *http.Request) (storage.JobMeta, storage.PageMeta, bool) {
	meta, err := s.store.LoadMeta(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "job not found")
		return meta, storage.PageMeta{}, false
	}
	page, _, ok := meta.FindPage(r.PathValue("page"))
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "page not found")
	}
	return meta, page, ok
}

// withDecodedPage decodes a page's original inside the render slot and
// hands it to use; the decoded image must not escape use.
func (s *Server) withDecodedPage(jobID string, page storage.PageMeta, use func(image.Image) error) error {
	original, err := s.store.LoadPageFile(jobID, page.File)
	if err != nil {
		return err
	}
	s.renderSlot <- struct{}{}
	defer func() { <-s.renderSlot }()
	img, err := pageimage.Decode(original, page.XDPI, page.YDPI)
	if err != nil {
		return err
	}
	return use(img)
}

// handlePageImage serves a page rendered with its edits at a preset size
// (?size=thumb|preview). ?crop=none leaves the crop out, for the crop
// tool to show the whole page. The ETag changes exactly when the edits
// do, so the UI only re-downloads what actually changed.
func (s *Server) handlePageImage(w http.ResponseWriter, r *http.Request) {
	meta, page, ok := s.loadPage(w, r)
	if !ok {
		return
	}
	if r.URL.Query().Get("crop") == "none" {
		page.Edits.Crop = nil
	}
	// The Adjust panel previews tone changes itself on top of this.
	if r.URL.Query().Get("tone") == "none" {
		page.Edits.Brightness, page.Edits.Contrast = 0, 0
	}
	size := r.URL.Query().Get("size")
	if size == "" {
		size = "preview"
	}
	if _, known := pageimage.Sizes[size]; !known {
		writeError(w, http.StatusBadRequest, "bad_request", "size must be thumb or preview")
		return
	}

	key := pageimage.CacheKey(page.Edits, size)
	etag := fmt.Sprintf("%q", page.ID+"-"+key)
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	data, cached := s.store.LoadRender(meta.ID, page.ID, key)
	if !cached {
		err := s.withDecodedPage(meta.ID, page, func(img image.Image) error {
			var err error
			data, err = pageimage.RenderView(img, page.Edits, size)
			return err
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		_ = s.store.SaveRender(meta.ID, page.ID, key, data) // a failed cache write only costs a re-render
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Write(data)
}

// handlePageOriginal serves the scanner's JPEG exactly as it was scanned.
func (s *Server) handlePageOriginal(w http.ResponseWriter, r *http.Request) {
	meta, page, ok := s.loadPage(w, r)
	if !ok {
		return
	}
	data, err := s.store.LoadPageFile(meta.ID, page.File)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", meta.ID+"-"+page.File))
	w.Write(data)
}

// editPageRequest is a partial update: absent fields are left alone. Crop
// is raw so that "crop": null (clear it) differs from no crop key at all.
// A crop given with a rotation is in the new rotation's frame.
type editPageRequest struct {
	Rotation   *int                 `json:"rotation"`
	Crop       json.RawMessage      `json:"crop"`
	Brightness *int                 `json:"brightness"`
	Contrast   *int                 `json:"contrast"`
	Color      *pageimage.ColorMode `json:"color"`
	Hidden     *bool                `json:"hidden"`
}

func (req editPageRequest) apply(p storage.PageMeta) (storage.PageMeta, error) {
	e := p.Edits
	if req.Rotation != nil {
		e = e.WithRotation(*req.Rotation, p.WidthPx, p.HeightPx)
	}
	switch {
	case len(req.Crop) == 0:
	case bytes.Equal(req.Crop, []byte("null")):
		e.Crop = nil
	default:
		var c pageimage.Crop
		if err := json.Unmarshal(req.Crop, &c); err != nil {
			return p, fmt.Errorf("invalid crop: %w", err)
		}
		e.Crop = &c
	}
	if req.Brightness != nil {
		e.Brightness = *req.Brightness
	}
	if req.Contrast != nil {
		e.Contrast = *req.Contrast
	}
	if req.Color != nil {
		e.Color = *req.Color
	}
	if err := e.Validate(p.WidthPx, p.HeightPx); err != nil {
		return p, err
	}
	p.Edits = e
	if req.Hidden != nil {
		p.Hidden = *req.Hidden
	}
	return p, nil
}

func (s *Server) handleEditPage(w http.ResponseWriter, r *http.Request) {
	var req editPageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	pageID := r.PathValue("page")
	var edited storage.PageMeta
	meta, ok := s.mutateJob(w, r, func(meta *storage.JobMeta) (int, string) {
		page, i, found := meta.FindPage(pageID)
		if !found {
			return http.StatusNotFound, "page not found"
		}
		var err error
		if edited, err = req.apply(page); err != nil {
			return http.StatusBadRequest, err.Error()
		}
		meta.Pages[i] = edited
		return 0, ""
	})
	if !ok {
		return
	}
	_ = s.store.DeleteRenders(meta.ID, pageID) // stale renders are only wasted disk; keys already differ
	writeJSON(w, http.StatusOK, toPageResponse(edited))
}

func (s *Server) handleDeletePage(w http.ResponseWriter, r *http.Request) {
	pageID := r.PathValue("page")
	var removed storage.PageMeta
	meta, ok := s.mutateJob(w, r, func(meta *storage.JobMeta) (int, string) {
		page, i, found := meta.FindPage(pageID)
		if !found {
			return http.StatusNotFound, "page not found"
		}
		removed = page
		meta.Pages = slices.Delete(meta.Pages, i, i+1)
		meta.Splits = slices.DeleteFunc(meta.Splits, func(id string) bool { return id == pageID })
		return 0, ""
	})
	if !ok {
		return
	}
	// meta.json no longer references the file, so a failed delete only
	// leaves an orphan on disk.
	_ = s.store.DeletePageFile(meta.ID, removed.File)
	w.WriteHeader(http.StatusNoContent)
}

type reorderPagesRequest struct {
	Order []string `json:"order"`
}

// handleReorderPages sets the page order. Order must list every page ID,
// hidden ones included, exactly once.
func (s *Server) handleReorderPages(w http.ResponseWriter, r *http.Request) {
	var req reorderPagesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	meta, ok := s.mutateJob(w, r, func(meta *storage.JobMeta) (int, string) {
		if len(req.Order) != len(meta.Pages) {
			return http.StatusBadRequest, "order must list every page exactly once"
		}
		reordered := make([]storage.PageMeta, 0, len(req.Order))
		seen := make(map[string]bool, len(req.Order))
		for _, id := range req.Order {
			page, _, found := meta.FindPage(id)
			if !found || seen[id] {
				return http.StatusBadRequest, "order must list every page exactly once"
			}
			seen[id] = true
			reordered = append(reordered, page)
		}
		meta.Pages = reordered
		return 0, ""
	})
	if ok {
		writeJSON(w, http.StatusOK, s.toDetail(meta))
	}
}
