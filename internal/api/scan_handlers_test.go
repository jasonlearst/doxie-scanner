package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/andiapps-dev/doxie-scanner/internal/pageimage"
	"github.com/andiapps-dev/doxie-scanner/internal/scanjobs"
	"github.com/andiapps-dev/doxie-scanner/internal/scsiusb"
	"github.com/andiapps-dev/doxie-scanner/internal/storage"
)

func waitDone(t *testing.T, srv *Server) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for srv.jobs.Scanning() {
		if time.Now().After(deadline) {
			t.Fatal("scan never finished")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestScanOptions(t *testing.T) {
	srv, _ := newTestServer(t, &fakeDriver{})
	resp := decode[scanOptionsResponse](t, do(srv, http.MethodGet, "/api/scan-options", nil))
	if resp.Defaults != scanjobs.DefaultSettings || len(resp.Resolutions) != 3 || resp.Sides[0] != "auto" || resp.Colors[0] != "auto" {
		t.Errorf("got %+v", resp)
	}
}

func TestStartScan_EmptyBodyUsesDefaults(t *testing.T) {
	srv, store := newTestServer(t, &fakeDriver{session: paperWith(t, textImage(60, 90))})
	rec := do(srv, http.MethodPost, "/api/scans", nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	id := decode[startScanResponse](t, rec).JobID
	waitDone(t, srv)
	meta, err := store.LoadMeta(id)
	if err != nil || meta.Settings != scanjobs.DefaultSettings {
		t.Errorf("settings %+v (%v)", meta.Settings, err)
	}
}

func TestStartScan_PartialSettingsKeepTheRestDefault(t *testing.T) {
	srv, store := newTestServer(t, &fakeDriver{session: paperWith(t, textImage(60, 90))})
	id := decode[startScanResponse](t, do(srv, http.MethodPost, "/api/scans", map[string]any{"resolution": 600, "color": "gray"})).JobID
	waitDone(t, srv)
	meta, _ := store.LoadMeta(id)
	if meta.Settings != (storage.ScanSettings{Resolution: 600, Sides: "auto", Color: "gray"}) {
		t.Errorf("settings %+v", meta.Settings)
	}
}

func TestStartScan_Rejections(t *testing.T) {
	srv, _ := newTestServer(t, &fakeDriver{})
	for _, body := range []any{"{not json", map[string]any{"resolution": 150}, map[string]any{"sides": "both"}} {
		if rec := do(srv, http.MethodPost, "/api/scans", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%v: status %d", body, rec.Code)
		}
	}
}

func TestStartScan_ConflictWhileRunning(t *testing.T) {
	srv, _, id, _ := runningServer(t)
	rec := do(srv, http.MethodPost, "/api/scans", nil)
	if rec.Code != http.StatusConflict || decode[startScanResponse](t, rec).JobID != id {
		t.Errorf("status %d body %s", rec.Code, rec.Body)
	}
}

func TestStartScan_StorageFailure(t *testing.T) {
	root := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(root, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv, _ := newTestServerAt(t, &fakeDriver{session: paperWith(t, textImage(60, 90))}, storage.New(root))
	if rec := do(srv, http.MethodPost, "/api/scans", nil); rec.Code != http.StatusInternalServerError {
		t.Errorf("status %d", rec.Code)
	}
}

func TestStartScan_NoPaper(t *testing.T) {
	srv, store := newTestServer(t, &fakeDriver{})
	waitFor(t, "connected", func() bool { return srv.jobs.Device().Connected })
	rec := do(srv, http.MethodPost, "/api/scans", nil)
	if rec.Code != http.StatusConflict || decode[errorEnvelope](t, rec).Error.Code != "no_paper" {
		t.Errorf("status %d %s", rec.Code, rec.Body)
	}
	if jobs, _ := store.ListJobs(); len(jobs) != 0 {
		t.Error("no job should be created without paper")
	}
}

func TestStartScan_ScannerAsleep(t *testing.T) {
	srv, _ := newTestServer(t, &fakeDriver{openErr: &scsiusb.ErrDeviceNotFound{}})
	waitFor(t, "the loop to notice", func() bool { return srv.jobs.Device().Err != nil })
	rec := do(srv, http.MethodPost, "/api/scans", nil)
	if rec.Code != http.StatusServiceUnavailable || decode[errorEnvelope](t, rec).Error.Message != asleepMessage {
		t.Errorf("status %d %s", rec.Code, rec.Body)
	}
}

func TestListScans(t *testing.T) {
	srv, store := newTestServer(t, &fakeDriver{})
	seedJob(t, store, "job1", seedPage{id: "001f", hidden: true}, seedPage{id: "001b"}, seedPage{id: "002f"})
	seedJob(t, store, "empty")

	list := decode[[]jobSummary](t, do(srv, http.MethodGet, "/api/scans", nil))
	if len(list) != 2 {
		t.Fatalf("got %d jobs", len(list))
	}
	for _, j := range list {
		switch j.ID {
		case "job1":
			if j.PageCount != 2 || j.CoverPage != "001b" || j.CoverKey == "" {
				t.Errorf("hidden pages don't count or make the cover: %+v", j)
			}
		case "empty":
			if j.PageCount != 0 || j.CoverPage != "" {
				t.Errorf("empty job: %+v", j)
			}
		}
	}
}

func TestListScans_StorageFailure(t *testing.T) {
	srv, store := newTestServer(t, &fakeDriver{})
	if err := os.WriteFile(filepath.Join(store.Root(), "jobs"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if rec := do(srv, http.MethodGet, "/api/scans", nil); rec.Code != http.StatusInternalServerError {
		t.Errorf("status %d", rec.Code)
	}
}

func TestGetScan(t *testing.T) {
	srv, store := newTestServer(t, &fakeDriver{})
	seedJob(t, store, "job1", seedPage{id: "001f"})

	got := decode[jobDetailResponse](t, do(srv, http.MethodGet, "/api/scans/job1", nil))
	if len(got.Pages) != 1 || got.Pages[0].ThumbKey == "" || got.Pages[0].PreviewKey == got.Pages[0].ThumbKey || got.PagesScanned != nil {
		t.Errorf("got %+v", got)
	}
	if rec := do(srv, http.MethodGet, "/api/scans/nope", nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown job: %d", rec.Code)
	}
}

func TestGetScan_ReturnsABacksInk(t *testing.T) {
	srv, store := newTestServer(t, &fakeDriver{})
	meta := seedJob(t, store, "job1", seedPage{id: "001f"}, seedPage{id: "001b"})
	meta.Pages[1].Ink = &pageimage.Ink{Sheet: 0.0179, Densest: 0.193}
	if err := store.SaveMeta(meta); err != nil {
		t.Fatal(err)
	}

	var got struct {
		Pages []map[string]json.RawMessage `json:"pages"`
	}
	if err := json.Unmarshal(do(srv, http.MethodGet, "/api/scans/job1", nil).Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if ink, ok := got.Pages[0]["ink"]; ok {
		t.Errorf("a page without measurements should have no ink, got %s", ink)
	}
	if ink := string(got.Pages[1]["ink"]); ink != `{"sheet":0.0179,"densest":0.193}` {
		t.Errorf("ink: %s", ink)
	}
}

func TestGetScan_ReportsLiveProgress(t *testing.T) {
	srv, _, id, _ := runningServer(t)
	got := decode[jobDetailResponse](t, do(srv, http.MethodGet, "/api/scans/"+id, nil))
	if got.PagesScanned == nil || *got.PagesScanned != 0 {
		t.Errorf("a running job should report pagesScanned, got %+v", got.PagesScanned)
	}
}

func TestRenameScan(t *testing.T) {
	srv, store := newTestServer(t, &fakeDriver{})
	seedJob(t, store, "job1")

	if rec := do(srv, http.MethodPatch, "/api/scans/job1", map[string]string{"name": "  Taxes  "}); rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if meta, _ := store.LoadMeta("job1"); meta.Name != "Taxes" {
		t.Errorf("name %q", meta.Name)
	}
	for body, want := range map[any]int{"{": 400, `{"name":"  "}`: 400} {
		if rec := do(srv, http.MethodPatch, "/api/scans/job1", body); rec.Code != want {
			t.Errorf("%v: status %d", body, rec.Code)
		}
	}
	if rec := do(srv, http.MethodPatch, "/api/scans/nope", map[string]string{"name": "x"}); rec.Code != http.StatusNotFound {
		t.Errorf("unknown job: %d", rec.Code)
	}
}

func TestMutateJob_SaveFailure(t *testing.T) {
	srv, store := newTestServer(t, &fakeDriver{})
	seedJob(t, store, "job1")
	jobDir := filepath.Join(store.Root(), "jobs", "job1")
	if err := os.Chmod(jobDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(jobDir, 0o755) })
	if rec := do(srv, http.MethodPatch, "/api/scans/job1", map[string]string{"name": "x"}); rec.Code != http.StatusInternalServerError {
		t.Errorf("status %d", rec.Code)
	}
}

func TestRunningJobIsLocked(t *testing.T) {
	srv, _, id, _ := runningServer(t)
	for _, req := range []struct{ method, path string }{
		{http.MethodPatch, "/api/scans/" + id},
		{http.MethodDelete, "/api/scans/" + id},
		{http.MethodPatch, "/api/scans/" + id + "/pages/001f"},
	} {
		if rec := do(srv, req.method, req.path, map[string]string{"name": "x"}); rec.Code != http.StatusConflict {
			t.Errorf("%s %s: status %d", req.method, req.path, rec.Code)
		}
	}
}

func TestDeleteScan(t *testing.T) {
	srv, store := newTestServer(t, &fakeDriver{})
	seedJob(t, store, "job1", seedPage{id: "001f"})

	if rec := do(srv, http.MethodDelete, "/api/scans/job1", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("status %d", rec.Code)
	}
	if _, err := store.LoadMeta("job1"); err == nil {
		t.Error("job should be gone")
	}
	if rec := do(srv, http.MethodDelete, "/api/scans/job1", nil); rec.Code != http.StatusNotFound {
		t.Errorf("second delete: %d", rec.Code)
	}
}

func TestDeleteScan_StorageFailure(t *testing.T) {
	srv, store := newTestServer(t, &fakeDriver{})
	seedJob(t, store, "job1")
	jobsDir := filepath.Join(store.Root(), "jobs")
	if err := os.Chmod(jobsDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(jobsDir, 0o755) })
	if rec := do(srv, http.MethodDelete, "/api/scans/job1", nil); rec.Code != http.StatusInternalServerError {
		t.Errorf("status %d", rec.Code)
	}
}

func TestErrorCode(t *testing.T) {
	for status, want := range map[int]string{404: "not_found", 400: "bad_request", 500: "internal_error"} {
		if got := errorCode(status); got != want {
			t.Errorf("%d: got %q", status, got)
		}
	}
}

func TestUpdateScan_Splits(t *testing.T) {
	srv, store := newTestServer(t, &fakeDriver{})
	seedJob(t, store, "job1", seedPage{id: "001f"}, seedPage{id: "001b"}, seedPage{id: "002f"})

	rec := do(srv, http.MethodPatch, "/api/scans/job1", map[string]any{"splits": []string{"002f", "001b", "002f"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
	meta, _ := store.LoadMeta("job1")
	if fmt.Sprint(meta.Splits) != "[002f 001b]" || meta.Name != "Scan job1" {
		t.Errorf("splits are deduplicated and the name left alone: %v %q", meta.Splits, meta.Name)
	}
	if rec := do(srv, http.MethodPatch, "/api/scans/job1", map[string]any{"splits": []string{"zzz"}}); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown page: %d", rec.Code)
	}

	if rec := do(srv, http.MethodDelete, "/api/scans/job1/pages/001b", nil); rec.Code != http.StatusNoContent {
		t.Fatal(rec.Code)
	}
	meta, _ = store.LoadMeta("job1")
	if fmt.Sprint(meta.Splits) != "[002f]" {
		t.Errorf("deleting a page drops its split: %v", meta.Splits)
	}

	do(srv, http.MethodPatch, "/api/scans/job1", map[string]any{"splits": []string{}})
	if meta, _ = store.LoadMeta("job1"); len(meta.Splits) != 0 {
		t.Errorf("an empty list clears splits: %v", meta.Splits)
	}
}

func TestListScans_SentCountsAndError(t *testing.T) {
	srv, store := newTestServer(t, &fakeDriver{})
	meta := seedJob(t, store, "job1", seedPage{id: "001f"})
	meta.Error = "paper jam"
	meta.Sent = []storage.SentDocument{{Status: "done"}, {Status: "failed"}, {Status: "queued"}, {Status: "sending"}}
	if err := store.SaveMeta(meta); err != nil {
		t.Fatal(err)
	}
	got := decode[[]jobSummary](t, do(srv, http.MethodGet, "/api/scans", nil))[0]
	if got.Sent != (sentCounts{Total: 4, Sending: 1, Done: 1, Failed: 1}) || got.Error != "paper jam" {
		t.Errorf("got %+v", got)
	}
}

func TestGetScan_ReportsWhenItWillBeRemoved(t *testing.T) {
	srv, store := newTestServer(t, &fakeDriver{})
	meta := seedJob(t, store, "job1", seedPage{id: "001f"})
	filed := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	meta.Sent = []storage.SentDocument{{Status: "done", Pages: []string{"001f"}, SentAt: filed, FiledAt: &filed}}
	if err := store.SaveMeta(meta); err != nil {
		t.Fatal(err)
	}
	got := decode[jobDetailResponse](t, do(srv, http.MethodGet, "/api/scans/job1", nil))
	if got.RemoveAt == nil || !got.RemoveAt.Equal(filed.Add(7*24*time.Hour)) {
		t.Errorf("removeAt %v", got.RemoveAt)
	}
	seedJob(t, store, "job2", seedPage{id: "001f"})
	if got := decode[jobDetailResponse](t, do(srv, http.MethodGet, "/api/scans/job2", nil)); got.RemoveAt != nil {
		t.Error("a scan not yet filed has no removal date")
	}
}

func scanAndWait(t *testing.T, srv *Server) string {
	t.Helper()
	rec := do(srv, http.MethodPost, "/api/scans", nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("start scan: %d %s", rec.Code, rec.Body)
	}
	waitDone(t, srv)
	return decode[startScanResponse](t, rec).JobID
}

func combine(srv *Server, into, from any) *httptest.ResponseRecorder {
	return do(srv, http.MethodPost, fmt.Sprintf("/api/scans/%v/combine", into), map[string]any{"from": from})
}

func TestCombine_TwoScansFromTheScanner(t *testing.T) {
	sheets := []image.Image{stripes(6), stripes(9), stripes(12), stripes(15), stripes(18)}
	sess := paperWith(t, sheets[:2]...)
	srv, store := newTestServer(t, &fakeDriver{session: sess})
	first := scanAndWait(t, srv)
	sess.mu.Lock()
	sess.pages = append(sess.pages, paperWith(t, sheets[2:]...).pages...)
	sess.mu.Unlock()
	second := scanAndWait(t, srv)
	for _, id := range []string{first, second} {
		if rec := do(srv, http.MethodPatch, "/api/scans/"+id, map[string]any{"splits": []string{"002f"}}); rec.Code != http.StatusOK {
			t.Fatal(rec.Code)
		}
	}

	// Combining into the newer scan still puts the older scan's pages first.
	rec := combine(srv, second, first)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
	got := decode[jobDetailResponse](t, rec)
	if l := layoutOf(got); l != "004f |005f 001f |002f 003f" {
		t.Errorf("got %q: the older scan's pages first, each scan's split kept, none between the two", l)
	}
	if got.ID != second || got.Status != storage.StatusCompleted {
		t.Errorf("the combined scan keeps the open scan's ID: %s %s", got.ID, got.Status)
	}
	for i, p := range got.Pages {
		original := do(srv, http.MethodGet, "/api/scans/"+second+"/pages/"+p.ID+"/original", nil).Body.Bytes()
		if want := sess.pages[i].Front.JPEG; !bytes.Equal(original, want) {
			t.Errorf("page %d (%s) holds the wrong sheet", i+1, p.ID)
		}
	}
	if list := decode[[]jobSummary](t, do(srv, http.MethodGet, "/api/scans", nil)); len(list) != 1 || list[0].PageCount != 5 {
		t.Errorf("list %+v: the other scan should be gone", list)
	}
	if _, err := store.LoadMeta(first); err == nil {
		t.Error("the combined-away scan is still on disk")
	}
	if pdf := do(srv, http.MethodGet, "/api/scans/"+second+"/pdf", nil); !bytes.Contains(pdf.Body.Bytes(), []byte("/Count 5")) {
		t.Errorf("status %d: the PDF should hold all 5 pages", pdf.Code)
	}
}

func TestCombine_Rejections(t *testing.T) {
	srv, store := newTestServer(t, &fakeDriver{})
	seedJob(t, store, "job1", seedPage{id: "001f"})
	seedJob(t, store, "job2", seedPage{id: "001f"})
	sent := seedJob(t, store, "sent", seedPage{id: "001f"})
	sent.Sent = []storage.SentDocument{{Pages: []string{"001f"}, Status: "done"}}
	if err := store.SaveMeta(sent); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		into string
		body any
		want int
		code string
	}{
		{"job1", "{", http.StatusBadRequest, "bad_request"},
		{"job1", map[string]any{}, http.StatusBadRequest, "bad_request"},
		{"job1", map[string]any{"from": "job1"}, http.StatusBadRequest, "bad_request"},
		{"job1", map[string]any{"from": "nope"}, http.StatusNotFound, "not_found"},
		{"nope", map[string]any{"from": "job1"}, http.StatusNotFound, "not_found"},
		{"job1", map[string]any{"from": "sent"}, http.StatusConflict, "already_sent"},
		{"sent", map[string]any{"from": "job1"}, http.StatusConflict, "already_sent"},
	}
	for _, c := range cases {
		rec := do(srv, http.MethodPost, "/api/scans/"+c.into+"/combine", c.body)
		if rec.Code != c.want || decode[errorEnvelope](t, rec).Error.Code != c.code {
			t.Errorf("combine %v into %s: %d %s, want %d %s", c.body, c.into, rec.Code, rec.Body, c.want, c.code)
		}
	}
	for _, id := range []string{"job1", "job2", "sent"} {
		if meta, err := store.LoadMeta(id); err != nil || len(meta.Pages) != 1 {
			t.Errorf("%s changed: %+v, %v", id, meta.Pages, err)
		}
	}

	running, runStore, id, _ := runningServer(t)
	seedJob(t, runStore, "job1", seedPage{id: "001f"})
	for _, pair := range [][2]string{{id, "job1"}, {"job1", id}} {
		if rec := combine(running, pair[0], pair[1]); rec.Code != http.StatusConflict || decode[errorEnvelope](t, rec).Error.Code != "scan_running" {
			t.Errorf("combine %s into %s while scanning: %d %s", pair[1], pair[0], rec.Code, rec.Body)
		}
	}
}

func TestCombine_NeverShowsADeletedPagesImage(t *testing.T) {
	srv, store := newTestServer(t, &fakeDriver{})
	seedJob(t, store, "into", seedPage{id: "001f"}, seedPage{id: "002f"})
	seedJob(t, store, "from", seedPage{id: "001f"})
	etag := func(page string) string {
		return do(srv, http.MethodGet, "/api/scans/into/pages/"+page+"/image?size=thumb", nil).Header().Get("ETag")
	}
	deleted := etag("002f")
	if rec := do(srv, http.MethodDelete, "/api/scans/into/pages/002f", nil); rec.Code != http.StatusNoContent {
		t.Fatal(rec.Code)
	}

	got := decode[jobDetailResponse](t, combine(srv, "into", "from"))
	if l := layoutOf(got); l != "001f 003f" {
		t.Fatalf("got %q: the incoming page must not take the deleted 002f", l)
	}
	if e := etag("003f"); e == "" || e == deleted {
		t.Errorf("incoming page's ETag %s matches the deleted page's %s", e, deleted)
	}
}

func TestCombine_SaveFailureChangesNothing(t *testing.T) {
	srv, store := newTestServer(t, &fakeDriver{})
	seedJob(t, store, "into", seedPage{id: "001f"})
	seedJob(t, store, "from", seedPage{id: "001f"})
	dir := filepath.Join(store.Root(), "jobs", "into")
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	if rec := combine(srv, "into", "from"); rec.Code != http.StatusInternalServerError {
		t.Errorf("status %d", rec.Code)
	}
	if meta, _ := store.LoadMeta("into"); len(meta.Pages) != 1 {
		t.Errorf("into changed: %+v", meta.Pages)
	}
	if _, err := store.LoadMeta("from"); err != nil {
		t.Errorf("from must survive a failed combine: %v", err)
	}
}

func TestCombine_OtherScanLeftBehindStillCombines(t *testing.T) {
	srv, store := newTestServer(t, &fakeDriver{})
	seedJob(t, store, "into", seedPage{id: "001f"})
	seedJob(t, store, "from", seedPage{id: "001f"})
	dir := filepath.Join(store.Root(), "jobs", "from")
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	rec := combine(srv, "into", "from")
	if rec.Code != http.StatusOK || layoutOf(decode[jobDetailResponse](t, rec)) != "001f 002f" {
		t.Errorf("status %d %s: the combine itself went through", rec.Code, rec.Body)
	}
}

func TestCombine_SendsAsOneDocument(t *testing.T) {
	store := storage.New(t.TempDir())
	into := seedJob(t, store, "into", seedPage{id: "001f"}, seedPage{id: "001b", hidden: true}, seedPage{id: "002f"})
	from := seedJob(t, store, "from", seedPage{id: "001f"}, seedPage{id: "002f"})
	from.CreatedAt = into.CreatedAt.Add(time.Minute)
	if err := store.SaveMeta(from); err != nil {
		t.Fatal(err)
	}
	srv, fake := paperlessServer(t, store)
	if rec := combine(srv, "into", "from"); rec.Code != http.StatusOK {
		t.Fatalf("combine: %d %s", rec.Code, rec.Body)
	}

	sendAsShown(t, srv, store, "into")
	if len(fake.uploads) != 1 || !bytes.Contains(fake.uploads[0].pdf, []byte("/Count 4")) {
		t.Errorf("%d uploads: want one document of the 4 visible pages", len(fake.uploads))
	}
}
