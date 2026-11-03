package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/andiapps-dev/doxie-scanner/internal/pageimage"
	"github.com/andiapps-dev/doxie-scanner/internal/scanjobs"
	"github.com/andiapps-dev/doxie-scanner/internal/storage"
)

type scanOptionsResponse struct {
	Defaults    storage.ScanSettings `json:"defaults"`
	Resolutions []int                `json:"resolutions"`
	Sides       []string             `json:"sides"`
	Colors      []string             `json:"colors"`
}

func (s *Server) handleScanOptions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, scanOptionsResponse{
		Defaults:    scanjobs.DefaultSettings,
		Resolutions: scanjobs.Resolutions,
		Sides:       []string{scanjobs.SidesAuto, scanjobs.SidesDouble, scanjobs.SidesSingle},
		Colors:      []string{scanjobs.ColorAuto, string(pageimage.ColorColor), string(pageimage.ColorGray), string(pageimage.ColorBW)},
	})
}

type startScanResponse struct {
	JobID  string `json:"jobId"`
	Status string `json:"status"`
}

// handleStartScan begins a new scan job. Any setting left out of the body
// (or a missing body) takes the default.
func (s *Server) handleStartScan(w http.ResponseWriter, r *http.Request) {
	var req storage.ScanSettings
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	settings := scanjobs.WithDefaults(req)
	if err := scanjobs.Validate(settings); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}

	id, err := s.jobs.StartScan(settings)
	switch {
	case errors.Is(err, scanjobs.ErrNoPaper):
		writeError(w, http.StatusConflict, "no_paper", "Load paper into the scanner first.")
		return
	case errors.Is(err, scanjobs.ErrScannerUnavailable):
		writeError(w, http.StatusServiceUnavailable, "scanner_unavailable", asleepMessage)
		return
	}
	if errors.Is(err, scanjobs.ErrAlreadyRunning) {
		jobID := ""
		if current := s.jobs.CurrentJob(); current != nil {
			jobID = current.ID
		}
		writeJSON(w, http.StatusConflict, startScanResponse{JobID: jobID, Status: "running"})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, startScanResponse{JobID: id, Status: "running"})
}

type jobSummary struct {
	ID        string               `json:"id"`
	Name      string               `json:"name"`
	CreatedAt string               `json:"createdAt"`
	Status    storage.JobStatus    `json:"status"`
	PageCount int                  `json:"pageCount"`
	Settings  storage.ScanSettings `json:"settings"`
	CoverPage string               `json:"coverPage,omitempty"`
	CoverKey  string               `json:"coverKey,omitempty"`
}

func toSummary(j storage.JobMeta) jobSummary {
	visible := j.VisiblePages()
	sum := jobSummary{
		ID:        j.ID,
		Name:      j.Name,
		CreatedAt: j.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		Status:    j.Status,
		PageCount: len(visible),
		Settings:  j.Settings,
	}
	if len(visible) > 0 {
		sum.CoverPage = visible[0].ID
		sum.CoverKey = pageimage.CacheKey(visible[0].Edits, "thumb")
	}
	return sum
}

func (s *Server) handleListScans(w http.ResponseWriter, r *http.Request) {
	jobs, err := s.store.ListJobs()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	summaries := make([]jobSummary, 0, len(jobs))
	for _, j := range jobs {
		summaries = append(summaries, toSummary(j))
	}
	writeJSON(w, http.StatusOK, summaries)
}

// pageResponse adds the cache keys the UI appends to image URLs, so a
// page's thumbnail URL changes exactly when its edits do.
type pageResponse struct {
	storage.PageMeta
	ThumbKey   string `json:"thumbKey"`
	PreviewKey string `json:"previewKey"`
}

type jobDetailResponse struct {
	storage.JobMeta
	Pages        []pageResponse `json:"pages"`
	PagesScanned *int           `json:"pagesScanned,omitempty"`
}

func toPageResponse(p storage.PageMeta) pageResponse {
	return pageResponse{
		PageMeta:   p,
		ThumbKey:   pageimage.CacheKey(p.Edits, "thumb"),
		PreviewKey: pageimage.CacheKey(p.Edits, "preview"),
	}
}

func toDetail(meta storage.JobMeta) jobDetailResponse {
	resp := jobDetailResponse{JobMeta: meta, Pages: make([]pageResponse, 0, len(meta.Pages))}
	for _, p := range meta.Pages {
		resp.Pages = append(resp.Pages, toPageResponse(p))
	}
	return resp
}

// handleGetScan returns a job with its pages. While it's the running job,
// it also carries live progress, since meta.json is only checkpointed
// once per sheet.
func (s *Server) handleGetScan(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	meta, err := s.store.LoadMeta(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "job not found")
		return
	}
	resp := toDetail(meta)
	if current := s.jobs.CurrentJob(); current != nil && current.ID == id && current.Status == storage.StatusRunning {
		n := current.PagesScanned
		resp.PagesScanned = &n
	}
	writeJSON(w, http.StatusOK, resp)
}

type renameRequest struct {
	Name string `json:"name"`
}

func (s *Server) handleRenameScan(w http.ResponseWriter, r *http.Request) {
	var req renameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "name must not be empty")
		return
	}
	if meta, ok := s.mutateJob(w, r, func(meta *storage.JobMeta) (int, string) {
		meta.Name = name
		return 0, ""
	}); ok {
		writeJSON(w, http.StatusOK, toDetail(meta))
	}
}

func (s *Server) handleDeleteScan(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if s.isScanning(id) {
		writeError(w, http.StatusConflict, "scan_running", "this job is still scanning")
		return
	}
	if _, err := s.store.LoadMeta(id); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "job not found")
		return
	}
	if err := s.store.DeleteJob(id); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) isScanning(jobID string) bool {
	current := s.jobs.CurrentJob()
	return current != nil && current.ID == jobID && current.Status == storage.StatusRunning
}

// mutateJob runs a read-modify-write of a job's metadata under metaMu.
// change returns a non-zero status plus a message to reject the update.
// On failure it has already written the error response. A job that's
// still scanning is locked: the scan loop holds its own copy of the
// metadata and would overwrite the change.
func (s *Server) mutateJob(w http.ResponseWriter, r *http.Request, change func(*storage.JobMeta) (int, string)) (storage.JobMeta, bool) {
	id := r.PathValue("id")
	if s.isScanning(id) {
		writeError(w, http.StatusConflict, "scan_running", "this job is still scanning")
		return storage.JobMeta{}, false
	}

	s.metaMu.Lock()
	defer s.metaMu.Unlock()
	meta, err := s.store.LoadMeta(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "job not found")
		return storage.JobMeta{}, false
	}
	if status, msg := change(&meta); status != 0 {
		writeError(w, status, errorCode(status), msg)
		return storage.JobMeta{}, false
	}
	if err := s.store.SaveMeta(meta); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return storage.JobMeta{}, false
	}
	return meta, true
}

func errorCode(status int) string {
	switch status {
	case http.StatusNotFound:
		return "not_found"
	case http.StatusBadRequest:
		return "bad_request"
	default:
		return "internal_error"
	}
}
