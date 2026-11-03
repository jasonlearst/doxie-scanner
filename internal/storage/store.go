package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Store persists scan jobs under a single root directory. Every method
// is a thin, direct filesystem operation — no caching, no database — by
// design, since this is meant to be trivially inspectable and robust
// rather than fast at a scale this tool will never see.
type Store struct {
	root string
	// mu serializes UpdateMeta's read-modify-write across the web app and
	// background housekeeping.
	mu sync.Mutex
}

// ErrJobNotFound means a job's metadata is missing or unreadable.
var ErrJobNotFound = errors.New("storage: job not found")

// New returns a Store rooted at root. It does not create or validate the
// directory — call CheckWritable for that (main.go does this once at
// startup, since the data directory is a hard requirement, not an
// optional cache).
func New(root string) *Store {
	return &Store{root: root}
}

// Root returns the store's root directory.
func (s *Store) Root() string { return s.root }

func (s *Store) jobsDir() string         { return filepath.Join(s.root, "jobs") }
func (s *Store) jobDir(id string) string { return filepath.Join(s.jobsDir(), id) }
func (s *Store) pagesDir(id string) string {
	return filepath.Join(s.jobDir(id), "pages")
}
func (s *Store) metaPath(id string) string {
	return filepath.Join(s.jobDir(id), "meta.json")
}

// CheckWritable verifies the store's root directory exists and is
// writable, creating it if necessary. This is the mechanism behind the
// "data directory is required" guarantee: main.go calls this once at
// startup and refuses to serve requests if it fails, since a silently
// unwritable data directory is exactly how scans get lost.
func (s *Store) CheckWritable() error {
	if err := os.MkdirAll(s.root, 0o755); err != nil {
		return fmt.Errorf("data directory %q is not usable: %w", s.root, err)
	}
	probe := filepath.Join(s.root, ".write-test")
	if err := os.WriteFile(probe, []byte("ok"), 0o644); err != nil {
		return fmt.Errorf("data directory %q is not writable: %w", s.root, err)
	}
	return os.Remove(probe)
}

// CreateJob creates the on-disk directory structure for a new job and
// writes its initial metadata.
func (s *Store) CreateJob(meta JobMeta) error {
	if err := os.MkdirAll(s.pagesDir(meta.ID), 0o755); err != nil {
		return fmt.Errorf("storage: create job %q: %w", meta.ID, err)
	}
	return s.SaveMeta(meta)
}

// SaveMeta writes (or overwrites) a job's meta.json.
func (s *Store) SaveMeta(meta JobMeta) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("storage: marshal metadata for job %q: %w", meta.ID, err)
	}
	if err := os.WriteFile(s.metaPath(meta.ID), data, 0o644); err != nil {
		return fmt.Errorf("storage: write metadata for job %q: %w", meta.ID, err)
	}
	return nil
}

// UpdateMeta loads a job's metadata, lets change edit it, and saves the
// result, all under one lock. A non-nil error from change aborts the
// update and is returned as is.
func (s *Store) UpdateMeta(id string, change func(*JobMeta) error) (JobMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	meta, err := s.LoadMeta(id)
	if err != nil {
		return JobMeta{}, fmt.Errorf("%w: %v", ErrJobNotFound, err)
	}
	if err := change(&meta); err != nil {
		return JobMeta{}, err
	}
	if err := s.SaveMeta(meta); err != nil {
		return JobMeta{}, err
	}
	return meta, nil
}

// LoadMeta reads a job's meta.json.
func (s *Store) LoadMeta(id string) (JobMeta, error) {
	data, err := os.ReadFile(s.metaPath(id))
	if err != nil {
		return JobMeta{}, fmt.Errorf("storage: read metadata for job %q: %w", id, err)
	}
	var meta JobMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return JobMeta{}, fmt.Errorf("storage: parse metadata for job %q: %w", id, err)
	}
	return meta, nil
}

// ListJobs returns every job's metadata, newest first. Job directories
// that exist but whose metadata can't be read (e.g. a partially-written
// job from a crash mid-scan) are silently skipped rather than failing
// the whole listing.
func (s *Store) ListJobs() ([]JobMeta, error) {
	entries, err := os.ReadDir(s.jobsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("storage: list jobs: %w", err)
	}

	jobs := make([]JobMeta, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		meta, err := s.LoadMeta(e.Name())
		if err != nil {
			continue
		}
		jobs = append(jobs, meta)
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].CreatedAt.After(jobs[j].CreatedAt) })
	return jobs, nil
}

// SavePageFile writes one page image file (front or back) for a job.
func (s *Store) SavePageFile(jobID, filename string, data []byte) error {
	path := filepath.Join(s.pagesDir(jobID), filename)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("storage: write page file %q for job %q: %w", filename, jobID, err)
	}
	return nil
}

// LoadPageFile reads one page image file for a job.
func (s *Store) LoadPageFile(jobID, filename string) ([]byte, error) {
	path := filepath.Join(s.pagesDir(jobID), filename)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("storage: read page file %q for job %q: %w", filename, jobID, err)
	}
	return data, nil
}

// DeletePageFile removes one page's original and every cached rendering
// of it.
func (s *Store) DeletePageFile(jobID, filename string) error {
	path := filepath.Join(s.pagesDir(jobID), filename)
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("storage: delete page file %q for job %q: %w", filename, jobID, err)
	}
	return s.DeleteRenders(jobID, strings.TrimSuffix(filename, filepath.Ext(filename)))
}

func (s *Store) rendersDir(jobID string) string {
	return filepath.Join(s.pagesDir(jobID), ".render")
}

func (s *Store) renderPath(jobID, pageID, key string) string {
	return filepath.Join(s.rendersDir(jobID), pageID+"-"+key+".jpg")
}

// LoadRender returns a cached rendering of a page; key identifies the
// edits and size it was made with.
func (s *Store) LoadRender(jobID, pageID, key string) ([]byte, bool) {
	data, err := os.ReadFile(s.renderPath(jobID, pageID, key))
	return data, err == nil
}

// SaveRender caches a rendering of a page.
func (s *Store) SaveRender(jobID, pageID, key string, data []byte) error {
	if err := os.MkdirAll(s.rendersDir(jobID), 0o755); err != nil {
		return fmt.Errorf("storage: create render cache for job %q: %w", jobID, err)
	}
	if err := os.WriteFile(s.renderPath(jobID, pageID, key), data, 0o644); err != nil {
		return fmt.Errorf("storage: cache rendering of page %q in job %q: %w", pageID, jobID, err)
	}
	return nil
}

// DeleteRenders drops every cached rendering of a page, e.g. after its
// edits change.
func (s *Store) DeleteRenders(jobID, pageID string) error {
	matches, _ := filepath.Glob(filepath.Join(s.rendersDir(jobID), pageID+"-*.jpg"))
	for _, m := range matches {
		if err := os.Remove(m); err != nil {
			return fmt.Errorf("storage: drop cached rendering %q: %w", filepath.Base(m), err)
		}
	}
	return nil
}

// DeleteJob removes a job's entire directory (metadata and all pages).
func (s *Store) DeleteJob(id string) error {
	if err := os.RemoveAll(s.jobDir(id)); err != nil {
		return fmt.Errorf("storage: delete job %q: %w", id, err)
	}
	return nil
}

// AppSettings are the user's saved preferences.
type AppSettings struct {
	// ButtonScan is what the scanner's own scan button does.
	ButtonScan ScanSettings `json:"buttonScan"`
	// RetentionDays is how long a scan stays after Paperless has filed
	// every page of it; 0 keeps scans forever, nil means the default.
	RetentionDays *int `json:"retentionDays,omitempty"`
}

func (s *Store) settingsPath() string { return filepath.Join(s.root, "settings.json") }

// LoadSettings returns the saved settings, or the zero value if none have
// been saved yet.
func (s *Store) LoadSettings() (AppSettings, error) {
	var settings AppSettings
	data, err := os.ReadFile(s.settingsPath())
	if os.IsNotExist(err) {
		return settings, nil
	}
	if err != nil {
		return settings, fmt.Errorf("storage: read settings: %w", err)
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return settings, fmt.Errorf("storage: parse settings: %w", err)
	}
	return settings, nil
}

// SaveSettings writes the settings.
func (s *Store) SaveSettings(settings AppSettings) error {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("storage: marshal settings: %w", err)
	}
	if err := os.WriteFile(s.settingsPath(), data, 0o644); err != nil {
		return fmt.Errorf("storage: write settings: %w", err)
	}
	return nil
}
