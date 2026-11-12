package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/andiapps-dev/doxie-scanner/internal/driver"
	"github.com/andiapps-dev/doxie-scanner/internal/pageimage"
	"github.com/andiapps-dev/doxie-scanner/internal/scanjobs"
	"github.com/andiapps-dev/doxie-scanner/internal/storage"
)

// newTestServer builds a Server against a real storage.Store in a temp
// directory, with a running scanjobs.Manager wrapping drv.
func newTestServer(t *testing.T, drv driver.Driver) (*Server, *storage.Store) {
	t.Helper()
	return newTestServerAt(t, drv, storage.New(t.TempDir()))
}

func newTestServerAt(t *testing.T, drv driver.Driver, store *storage.Store, opts ...Option) (*Server, *storage.Store) {
	t.Helper()
	mgr := scanjobs.NewManager(drv, store)
	mgr.Timings = scanjobs.Timings{Reconnect: 20 * time.Millisecond, ButtonPoll: 5 * time.Millisecond, Reply: 2 * time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { mgr.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return NewServer(drv, mgr, store, nil, "test", opts...), store
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// paperWith is a session with one sheet per image waiting in the feeder.
func paperWith(t *testing.T, images ...image.Image) *fakeSession {
	t.Helper()
	sess := &fakeSession{}
	for _, img := range images {
		sess.pages = append(sess.pages, driver.Page{Front: driver.Side{JPEG: jpegBytes(t, img), XDPI: 300, YDPI: 300}})
	}
	return sess
}

func do(srv *Server, method, path string, body any) *httptest.ResponseRecorder {
	var r io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		r = strings.NewReader(b)
	default:
		data, _ := json.Marshal(b)
		r = bytes.NewReader(data)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(method, path, r))
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return v
}

// textImage is white with black bars, sized like a small scan.
func textImage(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.White, image.Point{}, draw.Src)
	for y := 5; y < h; y += 15 {
		draw.Draw(img, image.Rect(5, y, w-5, y+4), image.Black, image.Point{}, draw.Src)
	}
	return img
}

func jpegBytes(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// seedPage describes one page to seed; the original is a 60x90 JPEG.
type seedPage struct {
	id     string
	hidden bool
	edits  pageimage.Edits
}

// seedJob writes a completed job with real JPEG originals straight into
// the store, bypassing the scanner.
func seedJob(t *testing.T, store *storage.Store, id string, pages ...seedPage) storage.JobMeta {
	t.Helper()
	meta := storage.JobMeta{
		ID:        id,
		Name:      "Scan " + id,
		Driver:    "fake-driver",
		CreatedAt: time.Now(),
		Status:    storage.StatusCompleted,
		Settings:  scanjobs.DefaultSettings,
		Pages:     []storage.PageMeta{},
	}
	if err := store.CreateJob(meta); err != nil {
		t.Fatal(err)
	}
	for _, p := range pages {
		page := storage.PageMeta{
			ID: p.id, File: storage.PageFilename(p.id), Side: "front",
			XDPI: 300, YDPI: 300, WidthPx: 60, HeightPx: 90,
			Hidden: p.hidden, Edits: p.edits,
		}
		var side byte
		fmt.Sscanf(p.id, "%03d%c", &page.Sheet, &side)
		if side == 'b' {
			page.Side = "back"
		}
		if err := store.SavePageFile(id, page.File, jpegBytes(t, textImage(60, 90))); err != nil {
			t.Fatal(err)
		}
		meta.Pages = append(meta.Pages, page)
	}
	if err := store.SaveMeta(meta); err != nil {
		t.Fatal(err)
	}
	return meta
}

// runningServer starts a scan whose session blocks until release is
// closed, so tests can observe a job that's still scanning.
func runningServer(t *testing.T) (*Server, *storage.Store, string, chan struct{}) {
	t.Helper()
	release := make(chan struct{})
	sess := paperWith(t, textImage(60, 90))
	sess.block = release
	srv, store := newTestServer(t, &fakeDriver{session: sess})
	rec := do(srv, http.MethodPost, "/api/scans", nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("start scan: %d %s", rec.Code, rec.Body)
	}
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	return srv, store, decode[startScanResponse](t, rec).JobID, release
}

// stripes is a page told apart from others by how far apart its bars are.
func stripes(every int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 60, 90))
	draw.Draw(img, img.Bounds(), image.White, image.Point{}, draw.Src)
	for y := 3; y < 90; y += every {
		draw.Draw(img, image.Rect(5, y, 55, y+2), image.Black, image.Point{}, draw.Src)
	}
	return img
}

// layoutOf lists a scan's pages in order: a trailing - marks a hidden
// page, and a leading | a split.
func layoutOf(got jobDetailResponse) string {
	var out []string
	for _, p := range got.Pages {
		tok := p.ID
		if p.Hidden {
			tok += "-"
		}
		if slices.Contains(got.Splits, p.ID) {
			tok = "|" + tok
		}
		out = append(out, tok)
	}
	return strings.Join(out, " ")
}

// sendAsShown sends a scan the way the app does: one Paperless document
// per document the splits cut.
func sendAsShown(t *testing.T, srv *Server, store *storage.Store, id string) {
	t.Helper()
	meta, err := store.LoadMeta(id)
	if err != nil {
		t.Fatal(err)
	}
	var docs []map[string]any
	for _, pages := range meta.Documents() {
		docs = append(docs, map[string]any{"pages": pages})
	}
	if rec := do(srv, http.MethodPost, "/api/scans/"+id+"/send", map[string]any{"documents": docs}); rec.Code != http.StatusAccepted {
		t.Fatalf("send: %d %s", rec.Code, rec.Body)
	}
	srv.background.Wait()
}
