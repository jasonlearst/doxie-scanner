package paperless

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakePaperless serves just enough of the Paperless-ngx API.
func fakePaperless(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Token secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL+"/", "secret")
}

func TestPing(t *testing.T) {
	c := fakePaperless(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"count":0,"next":null,"results":[]}`)
	})
	if err := c.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	bad := New(c.BaseURL(), "wrong")
	if err := bad.Ping(context.Background()); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("a bad token: got %v", err)
	}
	if err := New("http://127.0.0.1:1", "x").Ping(context.Background()); err == nil {
		t.Error("an unreachable server should fail")
	}
}

func TestMetadataFollowsPages(t *testing.T) {
	var base string
	c := fakePaperless(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/tags/" && r.URL.Query().Get("page") == "":
			fmt.Fprintf(w, `{"next":%q,"results":[{"id":1,"name":"Bills"}]}`, base+"/api/tags/?page=2")
		case r.URL.Path == "/api/tags/":
			fmt.Fprint(w, `{"next":null,"results":[{"id":2,"name":"Taxes"}]}`)
		case r.URL.Path == "/api/correspondents/":
			fmt.Fprint(w, `{"next":null,"results":[{"id":7,"name":"Bank"}]}`)
		case r.URL.Path == "/api/document_types/":
			fmt.Fprint(w, `{"next":null,"results":[{"id":3,"name":"Invoice"}]}`)
		}
	})
	base = c.BaseURL()

	m, err := c.Metadata(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Tags) != 2 || m.Tags[1].Name != "Taxes" || m.Correspondents[0].ID != 7 || m.DocumentTypes[0].Name != "Invoice" {
		t.Errorf("got %+v", m)
	}
}

func TestMetadataErrors(t *testing.T) {
	for _, failing := range []string{"/api/tags/", "/api/correspondents/", "/api/document_types/"} {
		c := fakePaperless(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == failing {
				http.Error(w, "boom", http.StatusInternalServerError)
				return
			}
			fmt.Fprint(w, `{"next":null,"results":[]}`)
		})
		if _, err := c.Metadata(context.Background()); err == nil {
			t.Errorf("%s failing should fail Metadata", failing)
		}
	}
	c := fakePaperless(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "not json") })
	if _, err := c.Metadata(context.Background()); err == nil {
		t.Error("garbage should fail to decode")
	}
}

func TestUpload(t *testing.T) {
	c := fakePaperless(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/documents/post_document/" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		f, hdr, err := r.FormFile("document")
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(f)
		got := fmt.Sprintf("%s|%s|%s|%s|%s|%v|%v", hdr.Filename, body, r.FormValue("title"), r.FormValue("correspondent"),
			r.FormValue("document_type"), r.MultipartForm.Value["tags"], r.MultipartForm.Value["created"])
		if got != "scan.pdf|%PDF|Water bill|7||[1 2]|[]" {
			t.Errorf("form: %s", got)
		}
		fmt.Fprint(w, `"4b1f-uuid"`)
	})
	id, err := c.Upload(context.Background(), Document{PDF: []byte("%PDF"), Filename: "scan.pdf", Title: "Water bill", Correspondent: 7, Tags: []int{1, 2}})
	if err != nil || id != "4b1f-uuid" {
		t.Errorf("got %q, %v", id, err)
	}
}

func TestUpload_ResponseShapes(t *testing.T) {
	for body, want := range map[string]string{`"abc"`: "abc", "abc\n": "abc", `""`: ""} {
		c := fakePaperless(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
		id, err := c.Upload(context.Background(), Document{PDF: []byte("x"), Filename: "a.pdf"})
		if want == "" {
			if err == nil {
				t.Errorf("%q: an empty task ID should fail", body)
			}
			continue
		}
		if err != nil || id != want {
			t.Errorf("%q: got %q, %v", body, id, err)
		}
	}
	c := fakePaperless(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "too big", http.StatusRequestEntityTooLarge)
	})
	if _, err := c.Upload(context.Background(), Document{PDF: []byte("x"), Filename: "a.pdf"}); err == nil || !strings.Contains(err.Error(), "too big") {
		t.Errorf("server errors should surface with their message, got %v", err)
	}
}

func TestTask(t *testing.T) {
	cases := map[string]Task{
		`[]`:                                   {Status: TaskQueued},
		`[{"status":"PENDING","result":null}]`: {Status: TaskQueued},
		`[{"status":"STARTED"}]`:               {Status: TaskProcessing},
		`[{"status":"SUCCESS","result":"Success","related_document":"42"}]`:           {Status: TaskDone, DocumentID: 42, Message: "Success"},
		`[{"status":"SUCCESS","related_document":43}]`:                                {Status: TaskDone, DocumentID: 43},
		`[{"status":"FAILURE","result":"Not consuming scan.pdf: It is a duplicate"}]`: {Status: TaskFailed, Message: "Not consuming scan.pdf: It is a duplicate"},

		`{"count":0,"next":null,"results":[]}`:                                                                {Status: TaskQueued},
		`{"results":[{"status":"pending","result_data":null}]}`:                                               {Status: TaskQueued},
		`{"results":[{"status":"started"}]}`:                                                                  {Status: TaskProcessing},
		`{"results":[{"status":"success","result_data":{"document_id":1158},"related_document_ids":[1158]}]}`: {Status: TaskDone, DocumentID: 1158},
		`{"results":[{"status":"failure","result_data":{"duplicate_of":12}}]}`:                                {Status: TaskFailed, Message: "It is a duplicate of document #12"},
		`{"results":[{"status":"failure","result_data":{"reason":"Unsupported type"}}]}`:                      {Status: TaskFailed, Message: "Unsupported type"},
		`{"results":[{"status":"failure","result_data":{"error_message":"OCR failed"}}]}`:                     {Status: TaskFailed, Message: "OCR failed"},
		`{"results":[{"status":"revoked"}]}`:                                                                  {Status: TaskFailed},
	}
	for body, want := range cases {
		c := fakePaperless(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("task_id") != "abc" {
				t.Errorf("task_id %q", r.URL.Query().Get("task_id"))
			}
			fmt.Fprint(w, body)
		})
		got, err := c.Task(context.Background(), "abc")
		if err != nil || got != want {
			t.Errorf("%s: got %+v, %v", body, got, err)
		}
	}
	c := fakePaperless(t, func(w http.ResponseWriter, r *http.Request) { http.Error(w, "down", http.StatusBadGateway) })
	if _, err := c.Task(context.Background(), "abc"); err == nil {
		t.Error("expected an error")
	}
	for _, body := range []string{"not json", `"a string"`} {
		c := fakePaperless(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
		if _, err := c.Task(context.Background(), "abc"); err == nil {
			t.Errorf("%s: expected a decode error", body)
		}
	}
}

func TestDocumentURL(t *testing.T) {
	if got := New("https://paperless.lan/", "t").DocumentURL(42); got != "https://paperless.lan/documents/42/details" {
		t.Errorf("got %q", got)
	}
}

func TestBadBaseURL(t *testing.T) {
	if err := New("http://bad host", "t").Ping(context.Background()); err == nil {
		t.Error("an invalid URL should fail")
	}
}
