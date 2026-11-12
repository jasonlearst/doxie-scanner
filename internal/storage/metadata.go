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
	"errors"
	"fmt"
	"slices"
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
	// Ink is what judged a back blank or printed. Fronts aren't judged, and
	// scans from before it was recorded don't have it.
	Ink *pageimage.Ink `json:"ink,omitempty"`
	// Paper is where the sheet was found, kept so the crop tool can go back
	// to it after the person changes the crop.
	Paper *pageimage.Paper `json:"paper,omitempty"`
}

// JobMeta is the full persisted record for one scan job, stored as
// meta.json inside that job's directory.
type JobMeta struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Driver      string       `json:"driver"`
	CreatedAt   time.Time    `json:"createdAt"`
	CompletedAt *time.Time   `json:"completedAt,omitempty"`
	Status      JobStatus    `json:"status"`
	Settings    ScanSettings `json:"settings"`
	Pages       []PageMeta   `json:"pages"`
	// Splits are the pages that start a new document when the job is
	// sent. A split moves with its page when the whole order is set, but
	// stays where it is when one page is moved (see MovePage).
	Splits []string       `json:"splits,omitempty"`
	Sent   []SentDocument `json:"sent,omitempty"`
	Error  string         `json:"error,omitempty"`
	// LastSheet is the highest sheet number a deleted or combined page
	// has had. Pages added later are numbered above it, so none takes the
	// ID, and with it the cached image, of a page that's gone.
	LastSheet int `json:"lastSheet,omitempty"`
}

// SentDocument records one upload to Paperless and how its consumption
// went, so the job shows what has already been filed.
// SentSending marks a document the app is still building and handing to
// Paperless. It has no Paperless task yet.
const SentSending = "sending"

type SentDocument struct {
	Title      string     `json:"title"`
	Pages      []string   `json:"pages"`
	TaskID     string     `json:"taskId,omitempty"`
	Status     string     `json:"status"` // sending, queued, processing, done or failed
	DocumentID int        `json:"documentId,omitempty"`
	Error      string     `json:"error,omitempty"`
	SentAt     time.Time  `json:"sentAt"`
	FiledAt    *time.Time `json:"filedAt,omitempty"`
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
	if i := pageIndex(j.Pages, id); i >= 0 {
		return j.Pages[i], i, true
	}
	return PageMeta{}, -1, false
}

func pageIndex(pages []PageMeta, id string) int {
	return slices.IndexFunc(pages, func(p PageMeta) bool { return p.ID == id })
}

// ErrPageNotFound means no page of the job has the ID given.
var ErrPageNotFound = errors.New("page not found")

// highestSheet is the highest sheet number any page of the job has had.
func (j JobMeta) highestSheet() int {
	n := j.LastSheet
	for _, p := range j.Pages {
		n = max(n, p.Sheet)
	}
	return n
}

// DeletePage removes a page and its split, remembering its sheet so the
// page's ID is never handed out again.
func (j *JobMeta) DeletePage(id string) (PageMeta, error) {
	page, i, ok := j.FindPage(id)
	if !ok {
		return page, ErrPageNotFound
	}
	j.LastSheet = j.highestSheet()
	j.Pages = slices.Delete(j.Pages, i, i+1)
	j.Splits = slices.DeleteFunc(j.Splits, func(s string) bool { return s == id })
	return page, nil
}

// Documents are the visible pages' IDs, cut where Splits say.
func (j JobMeta) Documents() [][]string {
	var docs [][]string
	for _, p := range j.Pages {
		if p.Hidden {
			continue
		}
		if len(docs) == 0 || slices.Contains(j.Splits, p.ID) {
			docs = append(docs, nil)
		}
		docs[len(docs)-1] = append(docs[len(docs)-1], p.ID)
	}
	return docs
}

// attached is the run of pages around pages[i] that travel with it: the
// hidden pages of its sheet right beside it, such as its blank back.
func attached(pages []PageMeta, i int) (start, end int) {
	ofSheet := func(k int) bool { return pages[k].Hidden && pages[k].Sheet == pages[i].Sheet }
	start, end = i, i+1
	for start > 0 && ofSheet(start-1) {
		start--
	}
	for end < len(pages) && ofSheet(end) {
		end++
	}
	return start, end
}

// MovePage puts a visible page right after the visible page target, or
// right before it, in target's document. Only the moved page changes
// document: the boundaries stay where they are, and a document left
// with no pages goes. The page's attached hidden pages move with it.
// Other hidden pages, and splits on hidden pages, stay as they are.
func (j *JobMeta) MovePage(id, target string, after bool) error {
	page, i, ok := j.FindPage(id)
	if !ok {
		return ErrPageNotFound
	}
	to, _, found := j.FindPage(target)
	switch {
	case page.Hidden:
		return errors.New("a hidden page moves with its sheet's visible page")
	case !found:
		return fmt.Errorf("no page %q to move next to", target)
	case to.Hidden:
		return errors.New("a page can't be moved next to a hidden page")
	case target == id:
		return errors.New("a page can't be moved next to itself")
	}

	docs := j.Documents()
	for d := range docs {
		docs[d] = slices.DeleteFunc(docs[d], func(p string) bool { return p == id })
	}
	docs = slices.DeleteFunc(docs, func(doc []string) bool { return len(doc) == 0 })
	for d, doc := range docs {
		if k := slices.Index(doc, target); k >= 0 {
			if after {
				k++
			}
			docs[d] = slices.Insert(doc, k, id)
		}
	}

	start, end := attached(j.Pages, i)
	moving := slices.Clone(j.Pages[start:end])
	rest := slices.Delete(slices.Clone(j.Pages), start, end)
	at, tEnd := attached(rest, pageIndex(rest, target))
	if after {
		at = tEnd
	}
	j.Pages = slices.Insert(rest, at, moving...)

	starts := map[string]bool{}
	for _, doc := range docs[1:] {
		starts[doc[0]] = true
	}
	splits := make([]string, 0, len(docs)-1)
	for _, p := range j.Pages {
		if starts[p.ID] || (p.Hidden && slices.Contains(j.Splits, p.ID)) {
			splits = append(splits, p.ID)
		}
	}
	j.Splits = splits
	return nil
}

// Combine adds the pages of another job, in the order they were scanned:
// the older job's pages come first. Incoming pages are renumbered above
// every sheet this job has had, keep their edits and hidden state, and
// bring their splits; the two parts are one document. The result keeps
// this job's ID and name and counts as finished. files maps each
// incoming page's file in from to its file here.
func (j JobMeta) Combine(from JobMeta) (combined JobMeta, files map[string]string) {
	used := map[string]bool{}
	for _, p := range j.Pages {
		used[p.ID] = true
	}
	next := j.highestSheet()
	sheets := map[int]int{}
	ids := map[string]string{}
	files = map[string]string{}
	incoming := make([]PageMeta, 0, len(from.Pages))
	for _, p := range from.Pages {
		side := p.Side
		if side == "" {
			side = "front"
		}
		sheet, ok := sheets[p.Sheet]
		for !ok || used[PageID(sheet, side)] {
			next++
			sheet, ok = next, true
		}
		sheets[p.Sheet] = sheet
		id := PageID(sheet, side)
		used[id] = true
		ids[p.ID] = id
		files[p.File] = PageFilename(id)
		p.ID, p.File, p.Sheet = id, PageFilename(id), sheet
		incoming = append(incoming, p)
	}

	combined = j
	combined.LastSheet = next
	combined.Status, combined.Error = StatusCompleted, ""
	if from.CompletedAt != nil && (j.CompletedAt == nil || from.CompletedAt.After(*j.CompletedAt)) {
		combined.CompletedAt = from.CompletedAt
	}
	splits := slices.Clone(j.Splits)
	for _, s := range from.Splits {
		if id, ok := ids[s]; ok {
			splits = append(splits, id)
		}
	}
	later := JobMeta{Pages: incoming}
	if from.CreatedAt.Before(j.CreatedAt) {
		later = j
		combined.Pages = append(incoming, j.Pages...)
	} else {
		combined.Pages = append(slices.Clone(j.Pages), incoming...)
	}
	if visible := later.VisiblePages(); len(visible) > 0 {
		splits = slices.DeleteFunc(splits, func(s string) bool { return s == visible[0].ID })
	}
	combined.Splits = splits
	return combined, files
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
