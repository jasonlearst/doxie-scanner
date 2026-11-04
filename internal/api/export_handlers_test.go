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

// Auto crop and straighten nearly every page, so those edits must not
// cost a decode and a second round of JPEG compression.
func TestJobPDF_CroppedAndStraightenedPagesKeepTheScan(t *testing.T) {
	srv, store := newTestServer(t, &fakeDriver{})
	seedJob(t, store, "job1", seedPage{id: "001f", edits: pageimage.Edits{Straighten: 1.5, Rotation: 90, Crop: &pageimage.Crop{X: 3, Y: 6, W: 80, H: 50}}})
	original, _ := store.LoadPageFile("job1", "001f.jpg")

	pdf := do(srv, http.MethodGet, "/api/scans/job1/pdf", nil).Body.Bytes()
	if !bytes.Contains(pdf, original) {
		t.Error("a cropped, straightened, turned page must still carry the scan byte for byte")
	}
	if !bytes.Contains(pdf, []byte("/MediaBox [0 0 19.20 12.00]")) {
		t.Error("the page should be the 80x50 px crop at 300 dpi")
	}
}

func TestJobPDF_RedrawsOnlyForToneAndColor(t *testing.T) {
	srv, store := newTestServer(t, &fakeDriver{})
	seedJob(t, store, "job1", seedPage{id: "001f", edits: pageimage.Edits{Straighten: 2, Color: pageimage.ColorBW, Crop: &pageimage.Crop{X: 5, Y: 5, W: 40, H: 60}}})
	original, _ := store.LoadPageFile("job1", "001f.jpg")

	pdf := do(srv, http.MethodGet, "/api/scans/job1/pdf", nil).Body.Bytes()
	if bytes.Contains(pdf, original) {
		t.Error("a black & white page has to be redrawn")
	}
	if !bytes.Contains(pdf, []byte("/BitsPerComponent 1")) || !bytes.Contains(pdf, []byte("/MediaBox [0 0 9.60 14.40]")) {
		t.Error("want a 1-bit image on a page the size of the crop")
	}
}

func TestJobPDF_NonSquare200dpiScanIsPlacedToSize(t *testing.T) {
	srv, store := newTestServer(t, &fakeDriver{})
	seedJob(t, store, "job1", seedPage{id: "001f"})
	meta, _ := store.LoadMeta("job1")
	// The scanner reads 300 dpi across and 200 down: a 90x90 JPEG is a
	// 60x90 page of square pixels.
	meta.Pages[0].XDPI, meta.Pages[0].YDPI = 300, 200
	meta.Pages[0].Edits.Crop = &pageimage.Crop{X: 0, Y: 0, W: 40, H: 80}
	if err := store.SaveMeta(meta); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePageFile("job1", "001f.jpg", jpegBytes(t, textImage(90, 90))); err != nil {
		t.Fatal(err)
	}

	rec := do(srv, http.MethodGet, "/api/scans/job1/pdf", nil)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte("/MediaBox [0 0 14.40 28.80]")) {
		t.Errorf("status %d: want the 40x80 crop at 200 dpi, 0.2x0.4 in", rec.Code)
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
