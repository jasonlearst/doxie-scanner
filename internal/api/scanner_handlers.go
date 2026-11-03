package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/andiapps-dev/doxie-scanner/internal/housekeeping"
	"github.com/andiapps-dev/doxie-scanner/internal/scanjobs"
	"github.com/andiapps-dev/doxie-scanner/internal/storage"
)

type versionResponse struct {
	Version string `json:"version"`
}

// handleVersion reports the build-time version identifier — a git tag in
// release builds, "dev" for a local/untagged build.
func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, versionResponse{Version: s.version})
}

type scannerStatusResponse struct {
	// Checking is true until the scanner has been looked for once.
	Checking  bool      `json:"checking"`
	Connected bool      `json:"connected"`
	Busy      bool      `json:"busy"`
	Paper     bool      `json:"paper"`
	VID       string    `json:"vid"`
	PID       string    `json:"pid"`
	Driver    string    `json:"driver"`
	Error     *apiError `json:"error,omitempty"`
}

// The scanner's own sleep (display and power light off) keeps it on USB;
// it only drops off when it switches itself off or is unplugged.
const asleepMessage = "The scanner is off. Press its power button to turn it on."

const maxRetentionDays = 3650

// handleScannerStatus reports what the scanner loop last saw. It never
// touches USB itself, so the UI can poll it freely, even mid-scan. Not
// connected is a normal state (the scanner sleeps when idle), so this
// always answers 200.
func (s *Server) handleScannerStatus(w http.ResponseWriter, r *http.Request) {
	info := s.drv.Info()
	d := s.jobs.Device()
	resp := scannerStatusResponse{
		Checking:  !d.Checked,
		Connected: d.Connected,
		Busy:      d.Scanning,
		Paper:     d.Paper,
		VID:       fmt.Sprintf("0x%04x", info.VID),
		PID:       fmt.Sprintf("0x%04x", info.PID),
		Driver:    info.Name,
	}
	if !d.Connected && d.Err != nil {
		_, code, message := classifyDeviceError(d.Err)
		if code == "device_not_found" {
			message = asleepMessage
		}
		resp.Error = &apiError{Code: code, Message: message}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.store.LoadSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, withDefaults(settings))
}

func withDefaults(settings storage.AppSettings) storage.AppSettings {
	settings.ButtonScan = scanjobs.WithDefaults(settings.ButtonScan)
	if settings.RetentionDays == nil {
		days := housekeeping.DefaultRetentionDays
		settings.RetentionDays = &days
	}
	return settings
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var settings storage.AppSettings
	if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	settings = withDefaults(settings)
	if err := scanjobs.Validate(settings.ButtonScan); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if *settings.RetentionDays < 0 || *settings.RetentionDays > maxRetentionDays {
		writeError(w, http.StatusBadRequest, "bad_request", fmt.Sprintf("retentionDays must be between 0 and %d", maxRetentionDays))
		return
	}
	if err := s.store.SaveSettings(settings); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, settings)
}
