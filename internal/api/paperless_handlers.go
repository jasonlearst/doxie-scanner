package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/andiapps-dev/doxie-scanner/internal/paperless"
	"github.com/andiapps-dev/doxie-scanner/internal/storage"
)

type paperlessStatusResponse struct {
	Configured bool   `json:"configured"`
	Connected  bool   `json:"connected"`
	URL        string `json:"url,omitempty"`
	Error      string `json:"error,omitempty"`
}

func (s *Server) handlePaperlessStatus(w http.ResponseWriter, r *http.Request) {
	if s.paperless == nil {
		writeJSON(w, http.StatusOK, paperlessStatusResponse{})
		return
	}
	resp := paperlessStatusResponse{Configured: true, URL: s.paperless.BaseURL()}
	if err := s.paperless.Ping(r.Context()); err != nil {
		resp.Error = err.Error()
	} else {
		resp.Connected = true
	}
	writeJSON(w, http.StatusOK, resp)
}

// requirePaperless writes a 503 when sending isn't set up.
func (s *Server) requirePaperless(w http.ResponseWriter) bool {
	if s.paperless == nil {
		writeError(w, http.StatusServiceUnavailable, "paperless_not_configured",
			"Paperless isn't set up: set DOXIE_PAPERLESS_URL and DOXIE_PAPERLESS_TOKEN.")
		return false
	}
	return true
}

func paperlessError(w http.ResponseWriter, err error) {
	if errors.Is(err, paperless.ErrUnauthorized) {
		writeError(w, http.StatusBadGateway, "paperless_unauthorized", "Paperless rejected the API token.")
		return
	}
	writeError(w, http.StatusBadGateway, "paperless_unreachable", err.Error())
}

func (s *Server) handlePaperlessMetadata(w http.ResponseWriter, r *http.Request) {
	if !s.requirePaperless(w) {
		return
	}
	m, err := s.paperless.Metadata(r.Context())
	if err != nil {
		paperlessError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

// sendDocument is one Paperless document cut from a job's pages. Zero IDs
// leave the choice to Paperless's matching rules and workflows.
type sendDocument struct {
	Pages         []string `json:"pages"`
	Title         string   `json:"title"`
	Created       string   `json:"created"`
	Correspondent int      `json:"correspondent"`
	DocumentType  int      `json:"documentType"`
	Tags          []int    `json:"tags"`
}

type sendRequest struct {
	Documents []sendDocument `json:"documents"`
}

// handleSend splits a job into one or more documents and uploads each to
// Paperless. A document that fails to build or upload is recorded as
// failed rather than stopping the rest.
func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	if !s.requirePaperless(w) {
		return
	}
	var req sendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	id := r.PathValue("id")
	if s.isScanning(id) {
		writeError(w, http.StatusConflict, "scan_running", "this job is still scanning")
		return
	}
	meta, err := s.store.LoadMeta(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "job not found")
		return
	}
	if len(req.Documents) == 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "nothing to send")
		return
	}
	pagesPerDoc := make([][]storage.PageMeta, len(req.Documents))
	for i, d := range req.Documents {
		if len(d.Pages) == 0 {
			writeError(w, http.StatusBadRequest, "bad_request", "every document needs at least one page")
			return
		}
		if pagesPerDoc[i], err = selectPages(meta, d.Pages); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
	}

	results := make([]storage.SentDocument, 0, len(req.Documents))
	for i, d := range req.Documents {
		title := strings.TrimSpace(d.Title)
		if title == "" {
			title = meta.Name
		}
		sent := storage.SentDocument{Title: title, Pages: d.Pages, Status: paperless.TaskQueued, SentAt: s.now()}
		if pdf, err := s.buildPDF(meta.ID, pagesPerDoc[i]); err != nil {
			sent.Status, sent.Error = paperless.TaskFailed, err.Error()
		} else if taskID, err := s.paperless.Upload(r.Context(), paperless.Document{
			PDF: pdf, Filename: pdfFilename(title), Title: title, Created: d.Created,
			Correspondent: d.Correspondent, DocumentType: d.DocumentType, Tags: d.Tags,
		}); err != nil {
			sent.Status, sent.Error = paperless.TaskFailed, err.Error()
		} else {
			sent.TaskID = taskID
		}
		results = append(results, sent)
	}

	updated, ok := s.mutateJob(w, r, func(m *storage.JobMeta) (int, string) {
		m.Sent = append(m.Sent, results...)
		return 0, ""
	})
	if ok {
		writeJSON(w, http.StatusOK, toDetail(updated))
	}
}

// handleRefreshSent asks Paperless how each pending upload is doing.
func (s *Server) handleRefreshSent(w http.ResponseWriter, r *http.Request) {
	if !s.requirePaperless(w) {
		return
	}
	meta, err := s.store.LoadMeta(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "job not found")
		return
	}
	updates := map[string]paperless.Task{}
	for _, sent := range meta.Sent {
		if sent.TaskID == "" || sent.Status == paperless.TaskDone || sent.Status == paperless.TaskFailed {
			continue
		}
		task, err := s.paperless.Task(r.Context(), sent.TaskID)
		if err != nil {
			paperlessError(w, err)
			return
		}
		updates[sent.TaskID] = task
	}

	updated, ok := s.mutateJob(w, r, func(m *storage.JobMeta) (int, string) {
		for i, sent := range m.Sent {
			if task, found := updates[sent.TaskID]; found {
				m.Sent[i].Status, m.Sent[i].DocumentID = task.Status, task.DocumentID
				if task.Status == paperless.TaskFailed {
					m.Sent[i].Error = task.Message
				}
			}
		}
		return 0, ""
	})
	if ok {
		writeJSON(w, http.StatusOK, toDetail(updated))
	}
}
