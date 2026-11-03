package api

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/andiapps-dev/doxie-scanner/internal/pageimage"
)

func TestJobPDF_VisiblePagesWithOriginalsUntouched(t *testing.T) {
	srv, store := newTestServer(t, &fakeDriver{})
	seedJob(t, store, "job1",
		seedPage{id: "001f"},
		seedPage{id: "001b", hidden: true},
		seedPage{id: "002f", edits: pageimage.Edits{Color: pageimage.ColorBW}})
	original, _ := store.LoadPageFile("job1", "001f.jpg")

	rec := do(srv, http.MethodGet, "/api/scans/job1/pdf", nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/pdf" {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
	pdf := rec.Body.Bytes()
	if !bytes.Contains(pdf, []byte("/Count 2")) {
		t.Error("the hidden page must be left out")
	}
	if !bytes.Contains(pdf, original) {
		t.Error("an unedited page must be embedded byte for byte")
	}
	if !bytes.Contains(pdf, []byte("/FlateDecode")) {
		t.Error("the black & white page should be re-encoded losslessly")
	}
	if !bytes.Contains(pdf, []byte("/MediaBox [0 0 14.40 21.60]")) {
		t.Error("a 60x90 px page at 300 dpi is 0.2x0.3 in")
	}
	if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename="Scan job1.pdf"` {
		t.Errorf("Content-Disposition %q", got)
	}
}

func TestJobPDF_ExplicitSelectionIncludesHiddenPages(t *testing.T) {
	srv, store := newTestServer(t, &fakeDriver{})
	seedJob(t, store, "job1", seedPage{id: "001f"}, seedPage{id: "001b", hidden: true})

	rec := do(srv, http.MethodGet, "/api/scans/job1/pdf?pages=001b,001f,001b", nil)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte("/Count 3")) {
		t.Errorf("status %d: an explicit list is honored as given", rec.Code)
	}
}

func TestJobPDF_Errors(t *testing.T) {
	srv, store := newTestServer(t, &fakeDriver{})
	seedJob(t, store, "job1", seedPage{id: "001f"})
	seedJob(t, store, "hidden-only", seedPage{id: "001f", hidden: true})
	seedJob(t, store, "broken", seedPage{id: "001f"}, seedPage{id: "002f", edits: pageimage.Edits{Rotation: 90}})
	if err := store.SavePageFile("broken", "002f.jpg", []byte("not a jpeg")); err != nil {
		t.Fatal(err)
	}
	seedJob(t, store, "missing", seedPage{id: "001f"})
	if err := store.DeletePageFile("missing", "001f.jpg"); err != nil {
		t.Fatal(err)
	}

	cases := map[string]int{
		"/api/scans/nope/pdf":           http.StatusNotFound,
		"/api/scans/job1/pdf?pages=zzz": http.StatusBadRequest,
		"/api/scans/hidden-only/pdf":    http.StatusBadRequest,
		"/api/scans/broken/pdf":         http.StatusInternalServerError,
		"/api/scans/missing/pdf":        http.StatusInternalServerError,
	}
	for path, want := range cases {
		if rec := do(srv, http.MethodGet, path, nil); rec.Code != want {
			t.Errorf("%s: status %d, want %d", path, rec.Code, want)
		}
	}
}

func TestPDFFilename(t *testing.T) {
	for in, want := range map[string]string{
		"Scan 2026-11-02 23:10": "Scan 2026-11-02 23-10.pdf",
		"../../etc/passwd":      "..-..-etc-passwd.pdf",
		"  ":                    "scan.pdf",
		"Taxes/2026":            "Taxes-2026.pdf",
	} {
		if got := pdfFilename(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}
