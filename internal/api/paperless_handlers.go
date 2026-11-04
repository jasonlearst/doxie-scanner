package api

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/andiapps-dev/doxie-scanner/internal/housekeeping"
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

// handleSend splits a job into one or more documents and hands them to
// Paperless in the background: it records each as sending and answers
// right away, so nobody waits for PDFs to be built and uploaded.
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
	batch := uploadBatch{jobID: id, docs: make([]pendingUpload, len(req.Documents))}
	for i, d := range req.Documents {
		if len(d.Pages) == 0 {
			writeError(w, http.StatusBadRequest, "bad_request", "every document needs at least one page")
			return
		}
		pages, err := selectPages(meta, d.Pages)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		title := strings.TrimSpace(d.Title)
		if title == "" {
			title = meta.Name
		}
		batch.docs[i] = pendingUpload{pages: pages, doc: paperless.Document{
			Filename: pdfFilename(title), Title: title, Created: d.Created,
			Correspondent: d.Correspondent, DocumentType: d.DocumentType, Tags: d.Tags,
		}}
	}

	now := s.now()
	updated, ok := s.mutateJob(w, r, func(m *storage.JobMeta) (int, string) {
		batch.first = len(m.Sent)
		for i, d := range req.Documents {
			m.Sent = append(m.Sent, storage.SentDocument{Title: batch.docs[i].doc.Title, Pages: d.Pages, Status: storage.SentSending, SentAt: now})
		}
		return 0, ""
	})
	if !ok {
		return
	}
	s.inBackground(func() { s.upload(batch) })
	writeJSON(w, http.StatusAccepted, s.toDetail(updated))
}

// uploadBatch is one send: documents recorded in a job's Sent from index
// first on, in order.
type uploadBatch struct {
	jobID string
	first int
	docs  []pendingUpload
}

type pendingUpload struct {
	pages []storage.PageMeta
	doc   paperless.Document
}

// uploadTimeout bounds one batch's uploads, so a hung Paperless can't hold
// up every later send.
const uploadTimeout = 10 * time.Minute

func (s *Server) inBackground(f func()) {
	s.background.Add(1)
	go func() {
		defer s.background.Done()
		f()
	}()
}

// upload builds each document's PDF and hands it to Paperless, recording
// the task to follow, or why it couldn't be sent.
func (s *Server) upload(b uploadBatch) {
	s.uploadMu.Lock()
	defer s.uploadMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), uploadTimeout)
	defer cancel()
	for i, u := range b.docs {
		status, taskID, failure := paperless.TaskQueued, "", ""
		if pdf, err := s.buildPDF(b.jobID, u.pages); err != nil {
			status, failure = paperless.TaskFailed, err.Error()
		} else {
			doc := u.doc
			doc.PDF = pdf
			if taskID, err = s.paperless.Upload(ctx, doc); err != nil {
				status, failure = paperless.TaskFailed, err.Error()
			}
		}
		_, err := s.store.UpdateMeta(b.jobID, func(m *storage.JobMeta) error {
			if at := b.first + i; at < len(m.Sent) {
				m.Sent[at].Status, m.Sent[at].TaskID, m.Sent[at].Error = status, taskID, failure
			}
			return nil
		})
		if err != nil {
			log.Printf("send %s: %v", b.jobID, err)
			return
		}
	}
	s.inBackground(func() { s.follow(b.jobID) })
}

// follow asks Paperless about a job's uploads until it has filed them all,
// for up to followFor; the hourly sweep picks up anything slower.
func (s *Server) follow(jobID string) {
	ticker := time.NewTicker(s.followEvery)
	defer ticker.Stop()
	deadline := time.After(s.followFor)
	for {
		select {
		case <-deadline:
			return
		case <-ticker.C:
		}
		meta, err := s.keeper.RefreshSent(context.Background(), jobID)
		if errors.Is(err, fs.ErrNotExist) || (err == nil && !housekeeping.Waiting(meta)) {
			return
		}
	}
}

// handleRefreshSent asks Paperless how each pending upload is doing.
func (s *Server) handleRefreshSent(w http.ResponseWriter, r *http.Request) {
	if !s.requirePaperless(w) {
		return
	}
	meta, err := s.keeper.RefreshSent(r.Context(), r.PathValue("id"))
	if err != nil {
		if _, loadErr := s.store.LoadMeta(r.PathValue("id")); loadErr != nil {
			writeError(w, http.StatusNotFound, "not_found", "job not found")
			return
		}
		paperlessError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.toDetail(meta))
}
