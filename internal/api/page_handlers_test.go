package api

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/andiapps-dev/doxie-scanner/internal/pageimage"
	"github.com/andiapps-dev/doxie-scanner/internal/storage"
)

func TestPageImage_RendersCachesAndRevalidates(t *testing.T) {
	srv, store := newTestServer(t, &fakeDriver{})
	seedJob(t, store, "job1", seedPage{id: "001f", edits: pageimage.Edits{Rotation: 90}})

	rec := do(srv, http.MethodGet, "/api/scans/job1/pages/001f/image?size=thumb", nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
	img, err := jpeg.Decode(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 90 || b.Dy() != 60 {
		t.Errorf("a 60x90 page rotated 90° should render 90x60, got %v", b.Size())
	}
	key := pageimage.CacheKey(pageimage.Edits{Rotation: 90}, "thumb")
	if _, ok := store.LoadRender("job1", "001f", key); !ok {
		t.Error("the render should be cached")
	}

	etag := rec.Header().Get("ETag")
	req := httptest.NewRequest(http.MethodGet, "/api/scans/job1/pages/001f/image?size=thumb", nil)
	req.Header.Set("If-None-Match", etag)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotModified {
		t.Errorf("matching ETag: status %d", rec.Code)
	}

	if rec := do(srv, http.MethodGet, "/api/scans/job1/pages/001f/image", nil); rec.Code != http.StatusOK {
		t.Errorf("default (preview) size: status %d", rec.Code)
	}
}

func TestPageImage_Errors(t *testing.T) {
	srv, store := newTestServer(t, &fakeDriver{})
	seedJob(t, store, "job1", seedPage{id: "001f"})

	cases := map[string]int{
		"/api/scans/job1/pages/001f/image?size=poster": http.StatusBadRequest,
		"/api/scans/nope/pages/001f/image":             http.StatusNotFound,
		"/api/scans/job1/pages/999f/image":             http.StatusNotFound,
		"/api/scans/nope/pages/001f/original":          http.StatusNotFound,
	}
	for path, want := range cases {
		if rec := do(srv, http.MethodGet, path, nil); rec.Code != want {
			t.Errorf("%s: status %d, want %d", path, rec.Code, want)
		}
	}

	if err := store.DeletePageFile("job1", "001f.jpg"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/scans/job1/pages/001f/image", "/api/scans/job1/pages/001f/original"} {
		if rec := do(srv, http.MethodGet, path, nil); rec.Code != http.StatusInternalServerError {
			t.Errorf("missing original, %s: status %d", path, rec.Code)
		}
	}
}

func TestPageOriginal_IsTheStoredScan(t *testing.T) {
	srv, store := newTestServer(t, &fakeDriver{})
	seedJob(t, store, "job1", seedPage{id: "001f", edits: pageimage.Edits{Color: pageimage.ColorBW}})
	want, _ := store.LoadPageFile("job1", "001f.jpg")

	rec := do(srv, http.MethodGet, "/api/scans/job1/pages/001f/original", nil)
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), want) {
		t.Errorf("status %d, original must be served untouched", rec.Code)
	}
}

func editPage(t *testing.T, srv *Server, body any) (*httptest.ResponseRecorder, pageResponse) {
	t.Helper()
	rec := do(srv, http.MethodPatch, "/api/scans/job1/pages/001f", body)
	if rec.Code != http.StatusOK {
		return rec, pageResponse{}
	}
	return rec, decode[pageResponse](t, rec)
}

func TestEditPage(t *testing.T) {
	srv, store := newTestServer(t, &fakeDriver{})
	seedJob(t, store, "job1", seedPage{id: "001f"})
	if err := store.SaveRender("job1", "001f", "stale", []byte("x")); err != nil {
		t.Fatal(err)
	}

	_, page := editPage(t, srv, map[string]any{"rotation": 90, "brightness": 20, "contrast": -10, "color": "gray", "hidden": true})
	e := page.Edits
	if e.Rotation != 90 || e.Brightness != 20 || e.Contrast != -10 || e.Color != pageimage.ColorGray || !page.Hidden {
		t.Errorf("edits not applied: %+v hidden=%v", e, page.Hidden)
	}
	if page.ThumbKey != pageimage.CacheKey(e, "thumb") {
		t.Error("the response should carry the new thumbnail key")
	}
	if _, ok := store.LoadRender("job1", "001f", "stale"); ok {
		t.Error("old renders should be dropped after an edit")
	}

	_, page = editPage(t, srv, map[string]any{"crop": map[string]int{"x": 0, "y": 0, "w": 90, "h": 30}})
	if page.Edits.Crop == nil || page.Edits.Rotation != 90 || page.Edits.Brightness != 20 {
		t.Errorf("a partial update must keep the other edits: %+v", page.Edits)
	}

	_, page = editPage(t, srv, map[string]any{"rotation": 0})
	if c := page.Edits.Crop; c == nil || *c != (pageimage.Crop{X: 0, Y: 0, W: 30, H: 90}) {
		t.Errorf("rotating back should carry the crop to the upright frame, got %+v", c)
	}

	_, page = editPage(t, srv, `{"crop": null, "hidden": false}`)
	if page.Edits.Crop != nil || page.Hidden {
		t.Errorf("crop null clears it and hidden can be undone: %+v", page)
	}

	meta, _ := store.LoadMeta("job1")
	if meta.Pages[0].Edits != page.Edits {
		t.Error("edits must be persisted")
	}
}

func TestEditPage_Rejections(t *testing.T) {
	srv, store := newTestServer(t, &fakeDriver{})
	seedJob(t, store, "job1", seedPage{id: "001f"})

	for _, body := range []any{"{", map[string]any{"rotation": 45}, `{"crop": "corner"}`, map[string]any{"crop": map[string]int{"x": 0, "y": 0, "w": 61, "h": 10}}} {
		if rec, _ := editPage(t, srv, body); rec.Code != http.StatusBadRequest {
			t.Errorf("%v: status %d", body, rec.Code)
		}
	}
	if rec := do(srv, http.MethodPatch, "/api/scans/job1/pages/999f", map[string]any{"rotation": 90}); rec.Code != http.StatusNotFound {
		t.Errorf("unknown page: %d", rec.Code)
	}
}

func TestDeletePage(t *testing.T) {
	srv, store := newTestServer(t, &fakeDriver{})
	seedJob(t, store, "job1", seedPage{id: "001f"}, seedPage{id: "001b"})

	if rec := do(srv, http.MethodDelete, "/api/scans/job1/pages/001f", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("status %d", rec.Code)
	}
	meta, _ := store.LoadMeta("job1")
	if len(meta.Pages) != 1 || meta.Pages[0].ID != "001b" {
		t.Errorf("pages %+v", meta.Pages)
	}
	if _, err := store.LoadPageFile("job1", "001f.jpg"); err == nil {
		t.Error("the original should be deleted")
	}
	if rec := do(srv, http.MethodDelete, "/api/scans/job1/pages/001f", nil); rec.Code != http.StatusNotFound {
		t.Errorf("second delete: %d", rec.Code)
	}
}

func TestReorderPages(t *testing.T) {
	srv, store := newTestServer(t, &fakeDriver{})
	seedJob(t, store, "job1", seedPage{id: "001f"}, seedPage{id: "001b", hidden: true}, seedPage{id: "002f"})

	rec := do(srv, http.MethodPut, "/api/scans/job1/pages/order", map[string][]string{"order": {"002f", "001f", "001b"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
	got := decode[jobDetailResponse](t, rec)
	if got.Pages[0].ID != "002f" || got.Pages[2].ID != "001b" || !got.Pages[2].Hidden {
		t.Errorf("order %+v", got.Pages)
	}

	for _, body := range []any{"{", map[string][]string{"order": {"001f"}}, map[string][]string{"order": {"001f", "001f", "002f"}}, map[string][]string{"order": {"001f", "001b", "zzz"}}} {
		if rec := do(srv, http.MethodPut, "/api/scans/job1/pages/order", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%v: status %d", body, rec.Code)
		}
	}
}

func TestPageImage_RenderFailure(t *testing.T) {
	srv, store := newTestServer(t, &fakeDriver{})
	seedJob(t, store, "job1", seedPage{id: "001f"})
	if err := store.SavePageFile("job1", "001f.jpg", []byte("not a jpeg")); err != nil {
		t.Fatal(err)
	}
	if rec := do(srv, http.MethodGet, "/api/scans/job1/pages/001f/image", nil); rec.Code != http.StatusInternalServerError {
		t.Errorf("status %d", rec.Code)
	}
}

func TestPageImage_CacheWriteFailureStillServes(t *testing.T) {
	srv, store := newTestServer(t, &fakeDriver{})
	seedJob(t, store, "job1", seedPage{id: "001f"})
	pages := filepath.Join(store.Root(), "jobs", "job1", "pages")
	if err := os.Chmod(pages, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(pages, 0o755) })
	if rec := do(srv, http.MethodGet, "/api/scans/job1/pages/001f/image?size=thumb", nil); rec.Code != http.StatusOK {
		t.Errorf("status %d", rec.Code)
	}
}

func TestPageImage_UncroppedForTheCropTool(t *testing.T) {
	srv, store := newTestServer(t, &fakeDriver{})
	seedJob(t, store, "job1", seedPage{id: "001f", edits: pageimage.Edits{Crop: &pageimage.Crop{X: 0, Y: 0, W: 30, H: 30}}})

	size := func(path string) image.Point {
		rec := do(srv, http.MethodGet, path, nil)
		img, err := jpeg.Decode(bytes.NewReader(rec.Body.Bytes()))
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		return img.Bounds().Size()
	}
	if got := size("/api/scans/job1/pages/001f/image?size=thumb"); got != (image.Point{30, 30}) {
		t.Errorf("cropped: %v", got)
	}
	if got := size("/api/scans/job1/pages/001f/image?size=thumb&crop=none"); got != (image.Point{60, 90}) {
		t.Errorf("crop=none should show the whole page: %v", got)
	}
}

func TestPageImage_WithoutToneForTheAdjustPanel(t *testing.T) {
	srv, store := newTestServer(t, &fakeDriver{})
	seedJob(t, store, "job1", seedPage{id: "001f", edits: pageimage.Edits{Brightness: 60, Contrast: 40}})

	mean := func(path string) float64 {
		rec := do(srv, http.MethodGet, path, nil)
		img, err := jpeg.Decode(bytes.NewReader(rec.Body.Bytes()))
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		var sum float64
		b := img.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				r, _, _, _ := img.At(x, y).RGBA()
				sum += float64(r >> 8)
			}
		}
		return sum / float64(b.Dx()*b.Dy())
	}
	toned := mean("/api/scans/job1/pages/001f/image?size=thumb")
	untoned := mean("/api/scans/job1/pages/001f/image?size=thumb&tone=none")
	if toned-untoned < 20 {
		t.Errorf("tone=none should drop the brightening: toned %.0f, untoned %.0f", toned, untoned)
	}
}

func TestEditPage_Straighten(t *testing.T) {
	srv, store := newTestServer(t, &fakeDriver{})
	seedJob(t, store, "job1", seedPage{id: "001f"})

	if _, page := editPage(t, srv, map[string]any{"straighten": -1.5}); page.Edits.Straighten != -1.5 {
		t.Errorf("straighten %v, want -1.5", page.Edits.Straighten)
	}
	if rec, _ := editPage(t, srv, map[string]any{"straighten": 20}); rec.Code != http.StatusBadRequest {
		t.Errorf("20°: status %d, want 400", rec.Code)
	}
}

func TestEditPage_AutoCropGoesBackToTheStoredPaper(t *testing.T) {
	srv, store := newTestServer(t, &fakeDriver{})
	seedJob(t, store, "job1", seedPage{id: "001f", edits: pageimage.Edits{Rotation: 90, Brightness: 15}})
	meta, _ := store.LoadMeta("job1")
	meta.Pages[0].Paper = &pageimage.Paper{Straighten: 0.5, Crop: pageimage.Crop{X: 2, Y: 3, W: 50, H: 80}}
	if err := store.SaveMeta(meta); err != nil {
		t.Fatal(err)
	}

	_, page := editPage(t, srv, map[string]any{"autoCrop": true})
	e := page.Edits
	if e.Straighten != 0.5 || e.Brightness != 15 || e.Rotation != 90 {
		t.Errorf("edits %+v: want the stored straightening, other edits kept", e)
	}
	if want := (pageimage.Crop{X: 7, Y: 2, W: 80, H: 50}); e.Crop == nil || *e.Crop != want {
		t.Errorf("crop %+v, want %+v in the page's turned frame", e.Crop, want)
	}
}

// narrowScan is a sheet narrower than the scanner on its gray backing.
func narrowScan() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 600, 800))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.NRGBA{176, 178, 177, 255}}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(60, 0, 540, 800), &image.Uniform{color.White}, image.Point{}, draw.Src)
	return img
}

func TestEditPage_AutoCropFindsTheSheetInAnOlderScan(t *testing.T) {
	srv, store := newTestServer(t, &fakeDriver{})
	seedJob(t, store, "job1", seedPage{id: "001f"})
	meta, _ := store.LoadMeta("job1")
	meta.Pages[0].WidthPx, meta.Pages[0].HeightPx = 600, 800
	if err := store.SaveMeta(meta); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePageFile("job1", "001f.jpg", jpegBytes(t, narrowScan())); err != nil {
		t.Fatal(err)
	}

	rec, page := editPage(t, srv, map[string]any{"autoCrop": true})
	if c := page.Edits.Crop; rec.Code != http.StatusOK || c == nil || c.X < 58 || c.X > 66 || c.X+c.W < 534 || c.X+c.W > 542 {
		t.Fatalf("status %d, crop %+v: want the sheet at x 60..540", rec.Code, c)
	}
	if meta, _ := store.LoadMeta("job1"); meta.Pages[0].Paper == nil {
		t.Error("the sheet found should be kept for next time")
	}
}

func TestEditPage_AutoCropWithNoBackingShowing(t *testing.T) {
	srv, store := newTestServer(t, &fakeDriver{})
	seedJob(t, store, "job1", seedPage{id: "001f"})

	rec, _ := editPage(t, srv, map[string]any{"autoCrop": true})
	if rec.Code != http.StatusUnprocessableEntity || !bytes.Contains(rec.Body.Bytes(), []byte("nothing to crop")) {
		t.Errorf("status %d %s, want 422 saying there's nothing to crop", rec.Code, rec.Body)
	}
}

// Documents: 001f 002f (001b is 001f's hidden back) | 003f | 004f.
func movingServer(t *testing.T) (*Server, *storage.Store) {
	t.Helper()
	srv, store := newTestServer(t, &fakeDriver{})
	seedJob(t, store, "job1", seedPage{id: "001f"}, seedPage{id: "001b", hidden: true}, seedPage{id: "002f"}, seedPage{id: "003f"}, seedPage{id: "004f"})
	if rec := do(srv, http.MethodPatch, "/api/scans/job1", map[string]any{"splits": []string{"003f", "004f"}}); rec.Code != http.StatusOK {
		t.Fatal(rec.Code)
	}
	return srv, store
}

func movePage(srv *Server, page string, body any) *httptest.ResponseRecorder {
	return do(srv, http.MethodPost, "/api/scans/job1/pages/"+page+"/move", body)
}

func TestMovePage(t *testing.T) {
	cases := []struct {
		name, page string
		body       map[string]string
		want       string
	}{
		{"after", "001f", map[string]string{"after": "002f"}, "002f 001f 001b- |003f |004f"},
		{"before", "002f", map[string]string{"before": "001f"}, "002f 001f 001b- |003f |004f"},
		{"to the start", "004f", map[string]string{"before": "001f"}, "004f 001f 001b- 002f |003f"},
		{"to the end", "001f", map[string]string{"after": "004f"}, "002f |003f |004f 001f 001b-"},
		{"just below a divider starts that document", "002f", map[string]string{"before": "003f"}, "001f 001b- |002f 003f |004f"},
		{"just above a divider ends that document", "004f", map[string]string{"after": "002f"}, "001f 001b- 002f 004f |003f"},
		{"emptying a document removes it", "004f", map[string]string{"before": "003f"}, "001f 001b- 002f |004f 003f"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv, _ := movingServer(t)
			rec := movePage(srv, c.page, c.body)
			if rec.Code != http.StatusOK {
				t.Fatalf("status %d %s", rec.Code, rec.Body)
			}
			if got := layoutOf(decode[jobDetailResponse](t, rec)); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
			if got := layoutOf(decode[jobDetailResponse](t, do(srv, http.MethodGet, "/api/scans/job1", nil))); got != c.want {
				t.Errorf("saved %q, want %q", got, c.want)
			}
		})
	}
}

func TestMovePage_Rejections(t *testing.T) {
	srv, _ := movingServer(t)
	const before = "001f 001b- 002f |003f |004f"
	cases := []struct {
		page string
		body any
		want int
	}{
		{"001f", "{", http.StatusBadRequest},
		{"001f", map[string]string{}, http.StatusBadRequest},
		{"001f", map[string]string{"after": "002f", "before": "003f"}, http.StatusBadRequest},
		{"001f", map[string]string{"after": "zzz"}, http.StatusBadRequest},
		{"002f", map[string]string{"after": "001b"}, http.StatusBadRequest},
		{"002f", map[string]string{"before": "002f"}, http.StatusBadRequest},
		{"001b", map[string]string{"after": "004f"}, http.StatusBadRequest},
		{"999f", map[string]string{"after": "001f"}, http.StatusNotFound},
	}
	for _, c := range cases {
		if rec := movePage(srv, c.page, c.body); rec.Code != c.want {
			t.Errorf("move %s %v: status %d, want %d", c.page, c.body, rec.Code, c.want)
		}
	}
	if rec := do(srv, http.MethodPost, "/api/scans/nope/pages/001f/move", map[string]string{"after": "002f"}); rec.Code != http.StatusNotFound {
		t.Errorf("unknown scan: %d", rec.Code)
	}
	if got := layoutOf(decode[jobDetailResponse](t, do(srv, http.MethodGet, "/api/scans/job1", nil))); got != before {
		t.Errorf("a refused move changed the scan: %q", got)
	}

	running, _, id, _ := runningServer(t)
	if rec := do(running, http.MethodPost, "/api/scans/"+id+"/pages/001f/move", map[string]string{"after": "002f"}); rec.Code != http.StatusConflict {
		t.Errorf("still scanning: %d", rec.Code)
	}
}

// The PDF writes its images in no fixed order, so pages are told apart by
// height: at 300 dpi, 90, 96 and 102 px are 21.60, 23.04 and 24.48 pt.
func TestMovePage_SendsInTheNewOrder(t *testing.T) {
	store := storage.New(t.TempDir())
	meta := seedJob(t, store, "job1", seedPage{id: "001f"}, seedPage{id: "002f"}, seedPage{id: "003f"})
	for i := range meta.Pages {
		meta.Pages[i].HeightPx = 90 + 6*i
	}
	if err := store.SaveMeta(meta); err != nil {
		t.Fatal(err)
	}
	srv, fake := paperlessServer(t, store)
	if rec := movePage(srv, "003f", map[string]string{"before": "001f"}); rec.Code != http.StatusOK {
		t.Fatalf("move: %d %s", rec.Code, rec.Body)
	}

	sendAsShown(t, srv, store, "job1")
	if len(fake.uploads) != 1 {
		t.Fatalf("uploads: %d", len(fake.uploads))
	}
	pdf := fake.uploads[0].pdf
	at := func(height string) int { return bytes.Index(pdf, []byte("/MediaBox [0 0 14.40 "+height+"]")) }
	if !(at("24.48") >= 0 && at("24.48") < at("21.60") && at("21.60") < at("23.04")) {
		t.Errorf("pages at %d, %d, %d: want 003f, 001f, 002f in that order", at("24.48"), at("21.60"), at("23.04"))
	}
}
