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

// ErrScannerUnavailable means the scanner isn't reachable: unplugged, or
// switched off (it turns itself off after a few idle hours).
var ErrScannerUnavailable = errors.New("the scanner is off: press its power button")

// ErrNoPaper means a scan was asked for with nothing in any feeder.
var ErrNoPaper = errors.New("load paper into the scanner first")

// DeviceStatus is what the UI shows about the scanner itself.
type DeviceStatus struct {
	// Checked is false until Run has first tried the scanner.
	Checked   bool `json:"checked"`
	Connected bool `json:"connected"`
	Scanning  bool `json:"scanning"`
	Paper     bool `json:"paper"`
	// Err is why the scanner can't be reached, while it can't.
	Err error `json:"-"`
}

type scanRequest struct {
	settings storage.ScanSettings
	reply    chan scanReply // nil for button presses
}

type scanReply struct {
	id  string
	err error
}

// Manager owns the scanner. Run keeps a session open whenever the scanner
// is reachable, listens for its scan button, reconnects after it sleeps,
// and runs every scan, whether asked for by the button or by StartScan.
// Only Run's goroutine ever touches the device.
type Manager struct {
	drv     driver.Driver
	store   *storage.Store
	now     func() time.Time
	process func(meta *storage.JobMeta, s scannedSheet) error

	Timings Timings

	requests chan scanRequest

	mu      sync.Mutex
	running *JobState // most recently started job's live state (nil until the first scan)
	device  DeviceStatus
}

// Timings control how Run paces itself; tests shorten them.
type Timings struct {
	// Reconnect is how often to look for a scanner that's asleep or off.
	Reconnect time.Duration
	// ButtonPoll is how long each wait for the scan button lasts, and so
	// how quickly a request from the web app is picked up.
	ButtonPoll time.Duration
	// Reply is how long StartScan waits for Run to take a request.
	Reply time.Duration
}

var DefaultTimings = Timings{Reconnect: 2 * time.Second, ButtonPoll: 500 * time.Millisecond, Reply: 10 * time.Second}

// NewManager returns a Manager for the given driver and store. Call Run
// to start using the scanner.
func NewManager(drv driver.Driver, store *storage.Store) *Manager {
	m := &Manager{
		drv:      drv,
		store:    store,
		now:      time.Now,
		Timings:  DefaultTimings,
		requests: make(chan scanRequest),
	}
	m.process = m.addSheet
	return m
}

// CurrentJob returns a snapshot of the most recently started job's live
// state, or nil if no scan has ever been started.
func (m *Manager) CurrentJob() *JobState {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running == nil {
		return nil
	}
	cp := *m.running
	return &cp
}

// Scanning reports whether a scan is in progress.
func (m *Manager) Scanning() bool {
	job := m.CurrentJob()
	return job != nil && job.Status == storage.StatusRunning
}

// Device reports the scanner's last known state.
func (m *Manager) Device() DeviceStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.device
	d.Scanning = m.running != nil && m.running.Status == storage.StatusRunning
	return d
}

func (m *Manager) setDevice(update func(*DeviceStatus)) {
	m.mu.Lock()
	update(&m.device)
	m.mu.Unlock()
}

// StartScan asks Run to scan with the given settings and returns the new
// job's ID once the scan has started.
func (m *Manager) StartScan(settings storage.ScanSettings) (string, error) {
	if err := Validate(settings); err != nil {
		return "", err
	}
	if m.Scanning() {
		return "", ErrAlreadyRunning
	}
	reply := make(chan scanReply, 1)
	select {
	case m.requests <- scanRequest{settings: settings, reply: reply}:
	case <-time.After(m.Timings.Reply):
		if m.Scanning() {
			return "", ErrAlreadyRunning
		}
		return "", ErrScannerUnavailable
	}
	r := <-reply
	return r.id, r.err
}

// ButtonSettings are what a press of the scanner's own button scans with.
func (m *Manager) ButtonSettings() storage.ScanSettings {
	saved, err := m.store.LoadSettings()
	if err != nil {
		return DefaultSettings
	}
	settings := WithDefaults(saved.ButtonScan)
	if Validate(settings) != nil {
		return DefaultSettings
	}
	return settings
}

// Run owns the scanner until ctx ends.
func (m *Manager) Run(ctx context.Context) {
	for ctx.Err() == nil {
		sess, err := m.drv.Open(ctx)
		if err != nil {
			m.setDevice(func(d *DeviceStatus) { *d = DeviceStatus{Checked: true, Err: err} })
			m.waitOffline(ctx)
			continue
		}
		m.setDevice(func(d *DeviceStatus) { *d = DeviceStatus{Checked: true, Connected: true} })
		m.serve(ctx, sess)
		sess.Close()
		// A session that fails straight away (say, a jam the user hasn't
		// cleared) mustn't turn into a tight reopen loop.
		select {
		case <-time.After(m.Timings.Reconnect / 4):
		case <-ctx.Done():
		}
	}
}

// waitOffline turns away scan requests until it's time to try the
// scanner again.
func (m *Manager) waitOffline(ctx context.Context) {
	retry := time.After(m.Timings.Reconnect)
	for {
		select {
		case req := <-m.requests:
			req.respond("", ErrScannerUnavailable)
		case <-retry:
			return
		case <-ctx.Done():
			return
		}
	}
}

// serve handles requests and button presses on an open session until the
// session fails; the caller then reconnects. A failed scan also ends it,
// so a jam or a scanner that went to sleep mid-scan gets a fresh session.
func (m *Manager) serve(ctx context.Context, sess driver.Session) {
	for ctx.Err() == nil {
		select {
		case req := <-m.requests:
			if m.scanOnRequest(ctx, sess, req) != nil {
				return
			}
			continue
		default:
		}

		pressed, err := sess.WaitForButton(ctx, m.Timings.ButtonPoll)
		if err != nil {
			return
		}
		if pressed {
			if m.scanOnRequest(ctx, sess, scanRequest{settings: m.ButtonSettings()}) != nil {
				return
			}
			continue
		}

		paper, err := sess.HasNextPage(ctx)
		if err != nil {
			return
		}
		m.setDevice(func(d *DeviceStatus) { d.Paper = paper })
	}
}

func (req scanRequest) respond(id string, err error) {
	if req.reply != nil {
		req.reply <- scanReply{id: id, err: err}
	}
}

// scanOnRequest starts a job if there's paper and runs it to the end. It
// returns an error only when the session should be dropped.
func (m *Manager) scanOnRequest(ctx context.Context, sess driver.Session, req scanRequest) error {
	paper, err := sess.HasNextPage(ctx)
	if err != nil {
		req.respond("", ErrScannerUnavailable)
		return err
	}
	if !paper {
		req.respond("", ErrNoPaper)
		return nil
	}

	meta, err := m.createJob(req.settings)
	if err != nil {
		req.respond("", err)
		return nil
	}
	req.respond(meta.ID, nil)

	err = m.run(ctx, sess, meta)
	m.drainButton(ctx, sess)
	return err
}

// drainButton discards presses made while a scan was running, so a
// queued press doesn't start a second scan the moment the first ends.
func (m *Manager) drainButton(ctx context.Context, sess driver.Session) {
	for i := 0; i < 3; i++ {
		if pressed, err := sess.WaitForButton(ctx, 20*time.Millisecond); err != nil || !pressed {
			return
		}
	}
}

func (m *Manager) createJob(settings storage.ScanSettings) (storage.JobMeta, error) {
	now := m.now()
	meta := storage.JobMeta{
		ID:        storage.NewJobID(now),
		Name:      storage.NewJobName(now),
		Driver:    m.drv.Info().Name,
		CreatedAt: now,
		Status:    storage.StatusRunning,
		Settings:  settings,
		Pages:     []storage.PageMeta{},
	}
	if err := m.store.CreateJob(meta); err != nil {
		return meta, fmt.Errorf("scanjobs: create job: %w", err)
	}
	m.mu.Lock()
	m.running = &JobState{ID: meta.ID, Status: storage.StatusRunning}
	m.mu.Unlock()
	return meta, nil
}

// run scans sheets until the feeders are empty and records the outcome.
// sheetQueue is how many scanned sheets may wait to be processed before
// feeding has to wait too.
const sheetQueue = 64

// run feeds sheets back to back and processes them on another goroutine.
// The DX400 pulls in the next sheet as soon as one ends and abandons it
// partway through if the host is slow to ask for it, and on a Raspberry
// Pi 3 processing a sheet takes several seconds.
func (m *Manager) run(ctx context.Context, sess driver.Session, meta storage.JobMeta) error {
	opts := driver.ScanOptions{
		Duplex:     meta.Settings.Sides != SidesSingle,
		Resolution: meta.Settings.Resolution,
	}
	sheets := make(chan scannedSheet, sheetQueue)
	stop := make(chan struct{})
	processed := make(chan error, 1)
	go func() { processed <- m.processSheets(&meta, sheets, stop) }()

	err := feed(ctx, sess, opts, sheets, stop)
	if endErr := sess.EndBatch(ctx); err == nil {
		err = endErr
	}
	close(sheets)
	if procErr := <-processed; err == nil {
		err = procErr
	}
	return m.finish(&meta, err)
}

// feed scans sheets until the feeder is empty or stop is closed. A sheet
// already moving is always read to the end so it leaves the scanner.
func feed(ctx context.Context, sess driver.Session, opts driver.ScanOptions, sheets chan<- scannedSheet, stop <-chan struct{}) error {
	for n := 1; ; n++ {
		hasNext, err := sess.HasNextPage(ctx)
		if err != nil || !hasNext {
			return err
		}
		select {
		case <-stop:
			return nil
		default:
		}
		page, err := sess.ScanPage(ctx, opts)
		if err != nil {
			return err
		}
		sheets <- scannedSheet{number: n, page: page}
	}
}

// processSheets processes sheets in order. After a failure it closes stop
// so feeding ends, and drops whatever is still queued.
func (m *Manager) processSheets(meta *storage.JobMeta, sheets <-chan scannedSheet, stop chan<- struct{}) error {
	var failed error
	for s := range sheets {
		if failed != nil {
			continue
		}
		if failed = m.process(meta, s); failed != nil {
			close(stop)
		}
	}
	return failed
}

type scannedSheet struct {
	number int
	page   driver.Page
}

// addSheet adds a scanned sheet's sides to the job and checkpoints it.
func (m *Manager) addSheet(meta *storage.JobMeta, s scannedSheet) error {
	front, err := m.addSide(meta.ID, s.number, "front", s.page.Front, meta.Settings)
	if err != nil {
		return err
	}
	meta.Pages = append(meta.Pages, front)
	if s.page.Back != nil {
		back, err := m.addSide(meta.ID, s.number, "back", *s.page.Back, meta.Settings)
		if err != nil {
			return err
		}
		meta.Pages = append(meta.Pages, back)
	}

	_ = m.store.SaveMeta(*meta) // best-effort checkpoint; the final save happens regardless

	m.mu.Lock()
	m.running.PagesScanned = len(meta.VisiblePages())
	m.mu.Unlock()
	return nil
}

// addSide stores the scanner's JPEG untouched, then decodes it once to
// find the sheet in it (crop and straighten), judge blankness and color,
// and pre-render the thumbnail the UI shows first. A blank back is hidden, never discarded, and only when the user
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
	if paper, found := pageimage.DetectPaper(img); found {
		page.Paper = &paper
		page.Edits = paper.Apply(page.Edits, page.WidthPx, page.HeightPx)
	}
	if side == "back" {
		ink := pageimage.MeasureInk(img, page.Edits)
		page.Ink = &ink
		page.Blank = ink.Blank()
	}
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
// and the in-memory live state, and passes runErr through.
func (m *Manager) finish(meta *storage.JobMeta, runErr error) error {
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
	m.running.Status = meta.Status
	m.running.Err = runErr
	m.mu.Unlock()
	return runErr
}
