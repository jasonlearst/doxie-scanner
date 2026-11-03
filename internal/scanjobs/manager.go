package scanjobs

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/andiapps-dev/doxie-scanner/internal/driver"
	"github.com/andiapps-dev/doxie-scanner/internal/pageimage"
	"github.com/andiapps-dev/doxie-scanner/internal/storage"
)

// ErrAlreadyRunning is returned by StartScan when a scan is already in
// progress — there's exactly one physical scanner, so there's no queue,
// just a single "busy" rejection.
var ErrAlreadyRunning = errors.New("scanjobs: a scan is already in progress")

const (
	SidesSingle = "single"
	SidesDouble = "double"
	SidesAuto   = "auto"
	ColorAuto   = "auto"
)

var (
	Resolutions = []int{200, 300, 600}
	sides       = []string{SidesSingle, SidesDouble, SidesAuto}
	colors      = []string{string(pageimage.ColorColor), string(pageimage.ColorGray), string(pageimage.ColorBW), ColorAuto}
)

// DefaultSettings mirror the Doxie app's own defaults.
var DefaultSettings = storage.ScanSettings{Resolution: 300, Sides: SidesAuto, Color: ColorAuto}

// WithDefaults fills any unset field from DefaultSettings.
func WithDefaults(s storage.ScanSettings) storage.ScanSettings {
	if s.Resolution == 0 {
		s.Resolution = DefaultSettings.Resolution
	}
	if s.Sides == "" {
		s.Sides = DefaultSettings.Sides
	}
	if s.Color == "" {
		s.Color = DefaultSettings.Color
	}
	return s
}

// Validate rejects settings the scanner or this app can't honor.
func Validate(s storage.ScanSettings) error {
	if !slices.Contains(Resolutions, s.Resolution) {
		return fmt.Errorf("resolution must be one of %v dpi", Resolutions)
	}
	if !slices.Contains(sides, s.Sides) {
		return fmt.Errorf("sides must be one of %v", sides)
	}
	if !slices.Contains(colors, s.Color) {
		return fmt.Errorf("color must be one of %v", colors)
	}
	return nil
}

// Manager orchestrates scan jobs against a driver.Driver, persisting
// results via a storage.Store.
type Manager struct {
	drv   driver.Driver
	store *storage.Store
	now   func() time.Time

	mu      sync.Mutex
	running *JobState // most recently started job's live state (nil until the first scan)
}

// NewManager returns a Manager for the given driver and store.
func NewManager(drv driver.Driver, store *storage.Store) *Manager {
	return &Manager{drv: drv, store: store, now: time.Now}
}

// CurrentJob returns a snapshot of the most recently started job's live
// state, or nil if no scan has ever been started. Callers should treat
// this as authoritative only while Status == storage.StatusRunning;
// once finished, storage.Store holds the authoritative persisted record.
func (m *Manager) CurrentJob() *JobState {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running == nil {
		return nil
	}
	cp := *m.running
	return &cp
}

// Scanning reports whether a scan currently holds the scanner.
func (m *Manager) Scanning() bool {
	job := m.CurrentJob()
	return job != nil && job.Status == storage.StatusRunning
}

// StartScan begins a new scan job in the background and returns its job
// ID immediately. Settings must already have passed Validate.
func (m *Manager) StartScan(settings storage.ScanSettings) (string, error) {
	if err := Validate(settings); err != nil {
		return "", err
	}
	m.mu.Lock()
	if m.running != nil && m.running.Status == storage.StatusRunning {
		m.mu.Unlock()
		return "", ErrAlreadyRunning
	}
	now := m.now()
	id := storage.NewJobID(now)
	m.running = &JobState{ID: id, Status: storage.StatusRunning}
	m.mu.Unlock()

	meta := storage.JobMeta{
		ID:        id,
		Name:      storage.NewJobName(now),
		Driver:    m.drv.Info().Name,
		CreatedAt: now,
		Status:    storage.StatusRunning,
		Settings:  settings,
		Pages:     []storage.PageMeta{},
	}
	if err := m.store.CreateJob(meta); err != nil {
		m.mu.Lock()
		m.running.Status = storage.StatusFailed
		m.running.Err = err
		m.mu.Unlock()
		return "", fmt.Errorf("scanjobs: create job: %w", err)
	}

	// The scan must outlive whatever request triggered it.
	go m.run(context.Background(), id, meta)

	return id, nil
}

func (m *Manager) run(ctx context.Context, id string, meta storage.JobMeta) {
	sess, err := m.drv.Open(ctx)
	if err != nil {
		m.finish(id, &meta, err)
		return
	}
	defer sess.Close()

	opts := driver.ScanOptions{
		Duplex:     meta.Settings.Sides != SidesSingle,
		Resolution: meta.Settings.Resolution,
	}
	for sheet := 1; ; sheet++ {
		hasNext, err := sess.HasNextPage(ctx)
		if err != nil {
			m.finish(id, &meta, err)
			return
		}
		if !hasNext {
			break
		}

		scanned, err := sess.ScanPage(ctx, opts)
		if err != nil {
			m.finish(id, &meta, err)
			return
		}

		front, err := m.addSide(id, sheet, "front", scanned.Front, meta.Settings)
		if err != nil {
			m.finish(id, &meta, err)
			return
		}
		meta.Pages = append(meta.Pages, front)
		if scanned.Back != nil {
			back, err := m.addSide(id, sheet, "back", *scanned.Back, meta.Settings)
			if err != nil {
				m.finish(id, &meta, err)
				return
			}
			meta.Pages = append(meta.Pages, back)
		}

		_ = m.store.SaveMeta(meta) // best-effort checkpoint; the final save happens regardless

		m.mu.Lock()
		if m.running != nil && m.running.ID == id {
			m.running.PagesScanned = len(meta.VisiblePages())
		}
		m.mu.Unlock()
	}

	m.finish(id, &meta, nil)
}

// addSide stores the scanner's JPEG untouched, then decodes it once to
// judge blankness and color and to pre-render the thumbnail the UI shows
// first. A blank back is hidden, never discarded, and only when the user
// chose Auto sides: a back with only a signature can look blank.
func (m *Manager) addSide(jobID string, sheet int, side string, s driver.Side, settings storage.ScanSettings) (storage.PageMeta, error) {
	pageID := storage.PageID(sheet, side)
	page := storage.PageMeta{
		ID:    pageID,
		File:  storage.PageFilename(pageID),
		Sheet: sheet,
		Side:  side,
		XDPI:  s.XDPI,
		YDPI:  s.YDPI,
	}
	if err := m.store.SavePageFile(jobID, page.File, s.JPEG); err != nil {
		return page, err
	}

	img, err := pageimage.Decode(s.JPEG, s.XDPI, s.YDPI)
	if err != nil {
		return page, fmt.Errorf("scanjobs: sheet %d %s: %w", sheet, side, err)
	}
	page.WidthPx, page.HeightPx = img.Bounds().Dx(), img.Bounds().Dy()
	page.Blank = side == "back" && pageimage.IsBlank(img)
	page.Hidden = page.Blank && settings.Sides == SidesAuto

	page.Edits.Color = pageimage.ColorMode(settings.Color)
	if settings.Color == ColorAuto {
		page.Edits.Color = pageimage.DetectColorMode(img)
	}

	thumb, err := pageimage.RenderView(img, page.Edits, "thumb")
	if err != nil {
		return page, err
	}
	_ = m.store.SaveRender(jobID, pageID, pageimage.CacheKey(page.Edits, "thumb"), thumb) // a cache miss later just re-renders
	return page, nil
}

// finish records a job's terminal state, both in the persisted metadata
// and the in-memory live state.
func (m *Manager) finish(id string, meta *storage.JobMeta, runErr error) {
	now := m.now()
	meta.CompletedAt = &now
	if runErr != nil {
		meta.Status = storage.StatusFailed
		meta.Error = runErr.Error()
	} else {
		meta.Status = storage.StatusCompleted
	}
	_ = m.store.SaveMeta(*meta)

	m.mu.Lock()
	if m.running != nil && m.running.ID == id {
		m.running.Status = meta.Status
		m.running.Err = runErr
	}
	m.mu.Unlock()
}
