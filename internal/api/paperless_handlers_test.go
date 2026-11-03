package api

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/andiapps-dev/doxie-scanner/internal/pageimage"
	"github.com/andiapps-dev/doxie-scanner/internal/paperless"
	"github.com/andiapps-dev/doxie-scanner/internal/storage"
)

type upload struct {
	title, filename string
	tags            []string
	correspondent   string
	pdf             []byte
}

// fakePaperless records uploads and answers task lookups from tasks.
type fakePaperless struct {
	mu           sync.Mutex
	uploads      []upload
	tasks        map[string]string
	failUploads  bool
	unauthorized bool
	queried      []string
}

func (f *fakePaperless) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.unauthorized {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	switch r.URL.Path {
	case "/api/tags/":
		fmt.Fprint(w, `{"next":null,"results":[{"id":1,"name":"Bills"}]}`)
	case "/api/correspondents/", "/api/document_types/":
		fmt.Fprint(w, `{"next":null,"results":[]}`)
	case "/api/documents/post_document/":
		if f.failUploads {
			http.Error(w, "disk full", http.StatusInternalServerError)
			return
		}
		r.ParseMultipartForm(32 << 20)
		file, hdr, _ := r.FormFile("document")
		pdf, _ := io.ReadAll(file)
		f.uploads = append(f.uploads, upload{r.FormValue("title"), hdr.Filename, r.MultipartForm.Value["tags"], r.FormValue("correspondent"), pdf})
		fmt.Fprintf(w, `"task-%d"`, len(f.uploads))
	case "/api/tasks/":
		id := r.URL.Query().Get("task_id")
		f.queried = append(f.queried, id)
		fmt.Fprint(w, f.tasks[id])
	}
}

func paperlessServer(t *testing.T, store *storage.Store) (*Server, *fakePaperless) {
	t.Helper()
	fake := &fakePaperless{tasks: map[string]string{}}
	pl := httptest.NewServer(http.HandlerFunc(fake.serve))
	t.Cleanup(pl.Close)
	if store == nil {
		store = storage.New(t.TempDir())
	}
	srv, _ := newTestServerAt(t, &fakeDriver{}, store, WithPaperless(paperless.New(pl.URL, "token")))
	return srv, fake
}

func TestPaperlessStatus(t *testing.T) {
	plain, _ := newTestServer(t, &fakeDriver{})
	if got := decode[paperlessStatusResponse](t, do(plain, http.MethodGet, "/api/paperless", nil)); got.Configured {
		t.Errorf("not configured: %+v", got)
	}

	srv, fake := paperlessServer(t, nil)
	if got := decode[paperlessStatusResponse](t, do(srv, http.MethodGet, "/api/paperless", nil)); !got.Configured || !got.Connected || got.URL == "" {
		t.Errorf("connected: %+v", got)
	}
	fake.unauthorized = true
	if got := decode[paperlessStatusResponse](t, do(srv, http.MethodGet, "/api/paperless", nil)); got.Connected || got.Error == "" {
		t.Errorf("bad token: %+v", got)
	}
}

func TestPaperlessMetadata(t *testing.T) {
	plain, _ := newTestServer(t, &fakeDriver{})
	if rec := do(plain, http.MethodGet, "/api/paperless/metadata", nil); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("not configured: %d", rec.Code)
	}

	srv, fake := paperlessServer(t, nil)
	got := decode[paperless.Metadata](t, do(srv, http.MethodGet, "/api/paperless/metadata", nil))
	if len(got.Tags) != 1 || got.Tags[0].Name != "Bills" {
		t.Errorf("got %+v", got)
	}
	fake.unauthorized = true
	rec := do(srv, http.MethodGet, "/api/paperless/metadata", nil)
	if rec.Code != http.StatusBadGateway || decode[errorEnvelope](t, rec).Error.Code != "paperless_unauthorized" {
		t.Errorf("bad token: %d %s", rec.Code, rec.Body)
	}
}

func TestSend_SplitsIntoDocuments(t *testing.T) {
	store := storage.New(t.TempDir())
	seedJob(t, store, "job1", seedPage{id: "001f"}, seedPage{id: "001b"}, seedPage{id: "002f", edits: pageimage.Edits{Rotation: 90}})
	srv, fake := paperlessServer(t, store)

	rec := do(srv, http.MethodPost, "/api/scans/job1/send", map[string]any{"documents": []map[string]any{
		{"pages": []string{"001f", "001b"}, "title": "Water bill", "tags": []int{1}, "correspondent": 7},
		{"pages": []string{"002f"}},
	}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
	if len(fake.uploads) != 2 {
		t.Fatalf("uploads: %d", len(fake.uploads))
	}
	first, second := fake.uploads[0], fake.uploads[1]
	if first.title != "Water bill" || first.filename != "Water bill.pdf" || fmt.Sprint(first.tags) != "[1]" || first.correspondent != "7" {
		t.Errorf("first upload: %+v", first)
	}
	if !bytes.Contains(first.pdf, []byte("/Count 2")) || !bytes.Contains(second.pdf, []byte("/Count 1")) {
		t.Error("each document should carry only its own pages")
	}
	if second.title != "Scan job1" {
		t.Errorf("an untitled document takes the job's name, got %q", second.title)
	}

	meta, _ := store.LoadMeta("job1")
	if len(meta.Sent) != 2 || meta.Sent[0].TaskID != "task-1" || meta.Sent[1].Status != paperless.TaskQueued || meta.Sent[1].SentAt.IsZero() {
		t.Errorf("sent records: %+v", meta.Sent)
	}
}

func TestSend_FailuresAreRecordedPerDocument(t *testing.T) {
	store := storage.New(t.TempDir())
	seedJob(t, store, "job1", seedPage{id: "001f"}, seedPage{id: "002f", edits: pageimage.Edits{Rotation: 90}})
	if err := store.SavePageFile("job1", "002f.jpg", []byte("not a jpeg")); err != nil {
		t.Fatal(err)
	}
	srv, fake := paperlessServer(t, store)

	do(srv, http.MethodPost, "/api/scans/job1/send", map[string]any{"documents": []map[string]any{{"pages": []string{"002f"}}, {"pages": []string{"001f"}}}})
	fake.failUploads = true
	do(srv, http.MethodPost, "/api/scans/job1/send", map[string]any{"documents": []map[string]any{{"pages": []string{"001f"}}}})

	meta, _ := store.LoadMeta("job1")
	if len(meta.Sent) != 3 {
		t.Fatalf("sent: %+v", meta.Sent)
	}
	if meta.Sent[0].Status != paperless.TaskFailed || meta.Sent[0].Error == "" {
		t.Errorf("an unbuildable document is recorded as failed: %+v", meta.Sent[0])
	}
	if meta.Sent[1].TaskID == "" {
		t.Error("the other document should still go through")
	}
	if meta.Sent[2].Status != paperless.TaskFailed || meta.Sent[2].Error == "" {
		t.Errorf("a rejected upload is recorded as failed: %+v", meta.Sent[2])
	}
}

func TestSend_Rejections(t *testing.T) {
	plain, _ := newTestServer(t, &fakeDriver{})
	if rec := do(plain, http.MethodPost, "/api/scans/job1/send", map[string]any{}); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("not configured: %d", rec.Code)
	}

	store := storage.New(t.TempDir())
	seedJob(t, store, "job1", seedPage{id: "001f"})
	srv, fake := paperlessServer(t, store)
	cases := []struct {
		path string
		body any
		want int
	}{
		{"/api/scans/job1/send", "{", http.StatusBadRequest},
		{"/api/scans/nope/send", map[string]any{"documents": []map[string]any{{"pages": []string{"001f"}}}}, http.StatusNotFound},
		{"/api/scans/job1/send", map[string]any{"documents": []map[string]any{}}, http.StatusBadRequest},
		{"/api/scans/job1/send", map[string]any{"documents": []map[string]any{{"pages": []string{}}}}, http.StatusBadRequest},
		{"/api/scans/job1/send", map[string]any{"documents": []map[string]any{{"pages": []string{"zzz"}}}}, http.StatusBadRequest},
	}
	for _, c := range cases {
		if rec := do(srv, http.MethodPost, c.path, c.body); rec.Code != c.want {
			t.Errorf("%s %v: status %d, want %d", c.path, c.body, rec.Code, c.want)
		}
	}
	if len(fake.uploads) != 0 {
		t.Error("nothing should be uploaded for a rejected request")
	}
}

func TestSend_RunningJobIsLocked(t *testing.T) {
	srv, _, id, _ := runningServer(t)
	srv.paperless = paperless.New("http://127.0.0.1:1", "t")
	rec := do(srv, http.MethodPost, "/api/scans/"+id+"/send", map[string]any{"documents": []map[string]any{{"pages": []string{"001f"}}}})
	if rec.Code != http.StatusConflict {
		t.Errorf("status %d", rec.Code)
	}
}

func TestRefreshSent(t *testing.T) {
	store := storage.New(t.TempDir())
	meta := seedJob(t, store, "job1", seedPage{id: "001f"})
	meta.Sent = []storage.SentDocument{
		{Title: "a", TaskID: "t1", Status: paperless.TaskQueued},
		{Title: "b", TaskID: "t2", Status: paperless.TaskProcessing},
		{Title: "c", TaskID: "t3", Status: paperless.TaskDone, DocumentID: 9},
		{Title: "d", Status: paperless.TaskFailed, Error: "upload failed"},
	}
	if err := store.SaveMeta(meta); err != nil {
		t.Fatal(err)
	}
	srv, fake := paperlessServer(t, store)
	fake.tasks["t1"] = `[{"status":"SUCCESS","related_document":"42"}]`
	fake.tasks["t2"] = `[{"status":"FAILURE","result":"duplicate"}]`

	got := decode[jobDetailResponse](t, do(srv, http.MethodPost, "/api/scans/job1/sent/refresh", nil))
	if s := got.Sent; s[0].Status != paperless.TaskDone || s[0].DocumentID != 42 || s[1].Status != paperless.TaskFailed || s[1].Error != "duplicate" || s[2].DocumentID != 9 {
		t.Errorf("refreshed: %+v", s)
	}
	if fmt.Sprint(fake.queried) != "[t1 t2]" {
		t.Errorf("only pending uploads should be checked, queried %v", fake.queried)
	}

	fake.tasks["t1"] = ""
	meta.Sent[0].Status = paperless.TaskQueued
	store.SaveMeta(meta)
	fake.unauthorized = true
	if rec := do(srv, http.MethodPost, "/api/scans/job1/sent/refresh", nil); rec.Code != http.StatusBadGateway {
		t.Errorf("paperless failure: %d", rec.Code)
	}
	if rec := do(srv, http.MethodPost, "/api/scans/nope/sent/refresh", nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown job: %d", rec.Code)
	}
	plain, _ := newTestServer(t, &fakeDriver{})
	if rec := do(plain, http.MethodPost, "/api/scans/job1/sent/refresh", nil); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("not configured: %d", rec.Code)
	}
}
