package api

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/andiapps-dev/doxie-scanner/internal/driver"
	"github.com/andiapps-dev/doxie-scanner/internal/scanjobs"
	"github.com/andiapps-dev/doxie-scanner/internal/scsiusb"
	"github.com/andiapps-dev/doxie-scanner/internal/storage"
)

func status(t *testing.T, srv *Server) scannerStatusResponse {
	t.Helper()
	return decode[scannerStatusResponse](t, do(srv, http.MethodGet, "/api/scanner/status", nil))
}

func TestHandleVersion(t *testing.T) {
	srv, _ := newTestServer(t, &fakeDriver{})
	if got := decode[versionResponse](t, do(srv, http.MethodGet, "/api/version", nil)); got.Version != "test" {
		t.Errorf("version %q", got.Version)
	}
}

func TestScannerStatus_Connected(t *testing.T) {
	srv, _ := newTestServer(t, &fakeDriver{info: driver.Info{Name: "doxie-dx400", VID: 0x2740, PID: 0x000c}})
	waitFor(t, "connected", func() bool { return status(t, srv).Connected })
	resp := status(t, srv)
	if resp.VID != "0x2740" || resp.PID != "0x000c" || resp.Driver != "doxie-dx400" || resp.Error != nil || resp.Paper {
		t.Errorf("got %+v", resp)
	}
}

func TestScannerStatus_ReportsPaper(t *testing.T) {
	srv, _ := newTestServer(t, &fakeDriver{session: paperWith(t, textImage(60, 90))})
	waitFor(t, "paper", func() bool { return status(t, srv).Paper })
}

func TestScannerStatus_Unreachable(t *testing.T) {
	cases := map[string]struct {
		err  error
		code string
	}{
		"asleep":       {&scsiusb.ErrDeviceNotFound{VID: 0x2740, PID: 0x000c}, "device_not_found"},
		"held by app":  {&scsiusb.ErrClaimInterface{Cause: errors.New("busy")}, "claim_interface_failed"},
		"other errors": {&scsiusb.ScsiError{Message: "boom"}, "scsi_error"},
	}
	for name, c := range cases {
		srv, _ := newTestServer(t, &fakeDriver{openErr: c.err})
		waitFor(t, name, func() bool { return status(t, srv).Error != nil })
		resp := status(t, srv)
		if resp.Connected || resp.Error.Code != c.code {
			t.Errorf("%s: got %+v", name, resp)
		}
		if c.code == "device_not_found" && resp.Error.Message != asleepMessage {
			t.Errorf("an absent scanner should read as asleep, got %q", resp.Error.Message)
		}
	}
}

func TestScannerStatus_BusyWhileScanning(t *testing.T) {
	srv, _, _, _ := runningServer(t)
	resp := status(t, srv)
	if !resp.Connected || !resp.Busy || resp.Error != nil {
		t.Errorf("a scanner in use by our own scan is connected and busy, got %+v", resp)
	}
}

func TestSettings(t *testing.T) {
	srv, store := newTestServer(t, &fakeDriver{})
	if got := decode[storage.AppSettings](t, do(srv, http.MethodGet, "/api/settings", nil)); got.ButtonScan != scanjobs.DefaultSettings {
		t.Errorf("unsaved settings should read as the defaults, got %+v", got)
	}

	rec := do(srv, http.MethodPut, "/api/settings", map[string]any{"buttonScan": map[string]any{"resolution": 600, "sides": "double"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
	want := storage.ScanSettings{Resolution: 600, Sides: "double", Color: "auto"}
	if saved, _ := store.LoadSettings(); saved.ButtonScan != want {
		t.Errorf("saved %+v", saved.ButtonScan)
	}

	for _, body := range []any{"{", map[string]any{"buttonScan": map[string]any{"resolution": 150}}} {
		if rec := do(srv, http.MethodPut, "/api/settings", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%v: status %d", body, rec.Code)
		}
	}
}

func TestSettings_StorageFailures(t *testing.T) {
	srv, store := newTestServer(t, &fakeDriver{})
	path := filepath.Join(store.Root(), "settings.json")
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if rec := do(srv, http.MethodGet, "/api/settings", nil); rec.Code != http.StatusInternalServerError {
		t.Errorf("corrupt settings: status %d", rec.Code)
	}
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, 0o644) })
	if rec := do(srv, http.MethodPut, "/api/settings", map[string]any{}); rec.Code != http.StatusInternalServerError {
		t.Errorf("unwritable settings: status %d", rec.Code)
	}
}
