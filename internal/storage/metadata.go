// Package storage persists scan jobs to the filesystem under a single
// required root directory (DOXIE_DATA_DIR in production — see main.go).
// There is deliberately no database: one directory per job containing a
// meta.json sidecar and a pages/ subdirectory of the scanner's JPEGs is enough for
// a tool with this usage pattern, and it keeps the whole thing
// inspectable with plain filesystem tools.
package storage

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/andiapps-dev/doxie-scanner/internal/pageimage"
)

// JobStatus is the lifecycle state of a scan job.
type JobStatus string

const (
	StatusRunning   JobStatus = "running"
	StatusCompleted JobStatus = "completed"
	StatusFailed    JobStatus = "failed"
)

// ScanSettings records what was asked for when a job started.
type ScanSettings struct {
	Resolution int    `json:"resolution"`
	Sides      string `json:"sides"` // "single", "double" or "auto"
	Color      string `json:"color"` // "color", "gray", "bw" or "auto"
}

// PageMeta describes one scanned side. Its File is the scanner's JPEG and
// is never rewritten: rotation, crop, tone and color are kept in Edits and
// applied whenever the page is rendered, so every change can be undone.
// ID never changes either; a page's position is its place in
// JobMeta.Pages, so reordering or deleting pages renames nothing.
type PageMeta struct {
	ID       string          `json:"id"`
	File     string          `json:"file"`
	Sheet    int             `json:"sheet"`
	Side     string          `json:"side"` // "front" or "back"
	XDPI     int             `json:"xdpi"`
	YDPI     int             `json:"ydpi"`
	WidthPx  int             `json:"widthPx"` // decoded, square pixels at YDPI
	HeightPx int             `json:"heightPx"`
	Blank    bool            `json:"blank,omitempty"`
	Hidden   bool            `json:"hidden,omitempty"`
	Edits    pageimage.Edits `json:"edits"`
}

// JobMeta is the full persisted record for one scan job, stored as
// meta.json inside that job's directory.
type JobMeta struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Driver      string         `json:"driver"`
	CreatedAt   time.Time      `json:"createdAt"`
	CompletedAt *time.Time     `json:"completedAt,omitempty"`
	Status      JobStatus      `json:"status"`
	Settings    ScanSettings   `json:"settings"`
	Pages       []PageMeta     `json:"pages"`
	Sent        []SentDocument `json:"sent,omitempty"`
	Error       string         `json:"error,omitempty"`
}

// SentDocument records one upload to Paperless and how its consumption
// went, so the job shows what has already been filed.
type SentDocument struct {
	Title      string    `json:"title"`
	Pages      []string  `json:"pages"`
	TaskID     string    `json:"taskId,omitempty"`
	Status     string    `json:"status"` // queued, processing, done or failed
	DocumentID int       `json:"documentId,omitempty"`
	Error      string    `json:"error,omitempty"`
	SentAt     time.Time `json:"sentAt"`
}

// VisiblePages are the pages that export and upload by default.
func (j JobMeta) VisiblePages() []PageMeta {
	out := make([]PageMeta, 0, len(j.Pages))
	for _, p := range j.Pages {
		if !p.Hidden {
			out = append(out, p)
		}
	}
	return out
}

// FindPage looks a page up by ID, returning its position too.
func (j JobMeta) FindPage(id string) (PageMeta, int, bool) {
	for i, p := range j.Pages {
		if p.ID == id {
			return p, i, true
		}
	}
	return PageMeta{}, -1, false
}

// NewJobID returns a human-sortable, collision-resistant job identifier:
// a UTC timestamp (to the second) plus 4 random hex characters, so two
// jobs started within the same second still get distinct IDs.
func NewJobID(now time.Time) string {
	return fmt.Sprintf("%s-%s", now.UTC().Format("20060102-150405"), randHex(4))
}

// NewJobName returns the auto-generated display name assigned to a job
// at creation time (e.g. "Scan 2026-07-14 15:30"). Callers can rename a
// job afterward via the API; this is only ever the initial default.
func NewJobName(now time.Time) string {
	return "Scan " + now.Format("2006-01-02 15:04")
}

// cryptoRandRead is a package-level indirection purely so tests can
// exercise the (effectively unreachable in practice) failure path
// without needing to actually break the host's entropy source.
var cryptoRandRead = rand.Read

func randHex(n int) string {
	buf := make([]byte, (n+1)/2)
	if _, err := cryptoRandRead(buf); err != nil {
		// crypto/rand failing is effectively unrecoverable and would
		// indicate a broken host; panicking here matches how the
		// standard library itself treats a failing rand.Read in
		// contexts where there is no sane fallback.
		panic("storage: crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(buf)[:n]
}

// PageID names the side of a sheet, e.g. "003b" for the back of sheet 3.
func PageID(sheet int, side string) string {
	return fmt.Sprintf("%03d%c", sheet, side[0])
}

// PageFilename is where a page's original scanner JPEG is stored.
func PageFilename(id string) string {
	return id + ".jpg"
}
