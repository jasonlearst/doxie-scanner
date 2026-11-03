package api

import (
	"io/fs"
	"net/http"
	"sync"

	"github.com/andiapps-dev/doxie-scanner/internal/driver"
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

	// metaMu serializes read-modify-write of meta.json across requests.
	metaMu sync.Mutex
	// renderSlot limits full-size decodes to one at a time: a single
	// 600 dpi page is ~135 MB decoded, which a 1 GB Raspberry Pi can only
	// afford once.
	renderSlot chan struct{}
}

// NewServer builds a Server and wires its full route table. staticFS
// serves the embedded frontend at "/"; it may be nil in tests.
func NewServer(drv driver.Driver, jobs *scanjobs.Manager, store *storage.Store, staticFS fs.FS, version string) *Server {
	s := &Server{drv: drv, jobs: jobs, store: store, version: version, renderSlot: make(chan struct{}, 1)}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/version", s.handleVersion)
	mux.HandleFunc("GET /api/scanner/status", s.handleScannerStatus)
	mux.HandleFunc("GET /api/scan-options", s.handleScanOptions)

	mux.HandleFunc("POST /api/scans", s.handleStartScan)
	mux.HandleFunc("GET /api/scans", s.handleListScans)
	mux.HandleFunc("GET /api/scans/{id}", s.handleGetScan)
	mux.HandleFunc("PATCH /api/scans/{id}", s.handleRenameScan)
	mux.HandleFunc("DELETE /api/scans/{id}", s.handleDeleteScan)
	mux.HandleFunc("GET /api/scans/{id}/pdf", s.handleJobPDF)

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

// ServeHTTP makes Server itself an http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}
