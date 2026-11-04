package api

import (
	"io/fs"
	"net/http"
	"sync"
	"time"

	"github.com/andiapps-dev/doxie-scanner/internal/driver"
	"github.com/andiapps-dev/doxie-scanner/internal/housekeeping"
	"github.com/andiapps-dev/doxie-scanner/internal/paperless"
	"github.com/andiapps-dev/doxie-scanner/internal/scanjobs"
	"github.com/andiapps-dev/doxie-scanner/internal/storage"
)

// Server wires driver/scanjobs/storage together behind an HTTP API and
// serves the embedded frontend for everything else.
type Server struct {
	drv     driver.Driver
	jobs    *scanjobs.Manager
	store   *storage.Store
	version string
	mux     *http.ServeMux

	// renderSlot limits full-size decodes to one at a time: a single
	// 600 dpi page is ~135 MB decoded, which a 1 GB Raspberry Pi can only
	// afford once.
	renderSlot chan struct{}

	paperless *paperless.Client // nil when not configured
	now       func() time.Time
	keeper    *housekeeping.Keeper

	// Sends are handed off to Paperless in the background, one at a time,
	// and then followed until Paperless has filed them.
	uploadMu    sync.Mutex
	background  sync.WaitGroup
	followEvery time.Duration
	followFor   time.Duration
}

// Option configures optional integrations.
type Option func(*Server)

// WithPaperless enables sending pages to a Paperless-ngx instance.
func WithPaperless(c *paperless.Client) Option {
	return func(s *Server) { s.paperless = c }
}

// NewServer builds a Server and wires its full route table. staticFS
// serves the embedded frontend at "/"; it may be nil in tests.
func NewServer(drv driver.Driver, jobs *scanjobs.Manager, store *storage.Store, staticFS fs.FS, version string, opts ...Option) *Server {
	s := &Server{drv: drv, jobs: jobs, store: store, version: version, renderSlot: make(chan struct{}, 1), now: time.Now,
		followEvery: 3 * time.Second, followFor: 15 * time.Minute}
	for _, opt := range opts {
		opt(s)
	}
	s.keeper = &housekeeping.Keeper{Store: store, Paperless: s.paperless, Scanning: s.isScanning, Now: func() time.Time { return s.now() }}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/version", s.handleVersion)
	mux.HandleFunc("GET /api/scanner/status", s.handleScannerStatus)
	mux.HandleFunc("GET /api/scan-options", s.handleScanOptions)
	mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	mux.HandleFunc("PUT /api/settings", s.handlePutSettings)

	mux.HandleFunc("POST /api/scans", s.handleStartScan)
	mux.HandleFunc("GET /api/scans", s.handleListScans)
	mux.HandleFunc("GET /api/scans/{id}", s.handleGetScan)
	mux.HandleFunc("PATCH /api/scans/{id}", s.handleUpdateScan)
	mux.HandleFunc("DELETE /api/scans/{id}", s.handleDeleteScan)
	mux.HandleFunc("GET /api/scans/{id}/pdf", s.handleJobPDF)
	mux.HandleFunc("POST /api/scans/{id}/send", s.handleSend)
	mux.HandleFunc("POST /api/scans/{id}/sent/refresh", s.handleRefreshSent)
	mux.HandleFunc("GET /api/paperless", s.handlePaperlessStatus)
	mux.HandleFunc("GET /api/paperless/metadata", s.handlePaperlessMetadata)

	mux.HandleFunc("PUT /api/scans/{id}/pages/order", s.handleReorderPages)
	mux.HandleFunc("GET /api/scans/{id}/pages/{page}/image", s.handlePageImage)
	mux.HandleFunc("GET /api/scans/{id}/pages/{page}/original", s.handlePageOriginal)
	mux.HandleFunc("PATCH /api/scans/{id}/pages/{page}", s.handleEditPage)
	mux.HandleFunc("DELETE /api/scans/{id}/pages/{page}", s.handleDeletePage)

	if staticFS != nil {
		mux.Handle("/", http.FileServer(http.FS(staticFS)))
	}

	s.mux = mux
	return s
}

// Housekeeper follows uploads and removes old imported scans; main runs
// its sweep in the background.
func (s *Server) Housekeeper() *housekeeping.Keeper { return s.keeper }

// ServeHTTP makes Server itself an http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}
