package api

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

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
	metaPath := filepath.Join(store.Root(), "jobs", "job1", "meta.json")
	if err := os.Chmod(metaPath, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(metaPath, 0o644) })
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
