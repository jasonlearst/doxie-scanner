package scanjobs

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/andiapps-dev/doxie-scanner/internal/driver"
	"github.com/andiapps-dev/doxie-scanner/internal/pageimage"
	"github.com/andiapps-dev/doxie-scanner/internal/storage"
)

func page(c color.Color, w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{c}, image.Point{}, draw.Src)
	return img
}

// textPage is white with black bars: black & white, not blank.
func textPage() *image.NRGBA {
	img := page(color.White, 300, 400)
	for y := 10; y < 400; y += 20 {
		draw.Draw(img, image.Rect(10, y, 290, y+6), image.Black, image.Point{}, draw.Src)
	}
	return img
}

func side(t *testing.T, img image.Image) driver.Side {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return driver.Side{JPEG: buf.Bytes(), XDPI: 300, YDPI: 300}
}

func sheet(front driver.Side, back *driver.Side) driver.Page {
	return driver.Page{Front: front, Back: back}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	// Generous margin: a throttled CI runner can starve the manager's
	// goroutine well past what these fakes need locally.
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// startManager runs a Manager with fast timings until the test ends.
func startManager(t *testing.T, drv *fakeDriver) (*Manager, *storage.Store) {
	t.Helper()
	store := storage.New(t.TempDir())
	m := NewManager(drv, store)
	m.Timings = Timings{Reconnect: 20 * time.Millisecond, ButtonPoll: 5 * time.Millisecond, Reply: 2 * time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { m.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return m, store
}

func finished(t *testing.T, m *Manager) *JobState {
	t.Helper()
	waitFor(t, "the scan to finish", func() bool { return m.CurrentJob() != nil && !m.Scanning() })
	return m.CurrentJob()
}

func scan(t *testing.T, settings storage.ScanSettings, sheets ...driver.Page) (*fakeSession, storage.JobMeta, *storage.Store) {
	t.Helper()
	sess := &fakeSession{pages: sheets}
	m, store := startManager(t, &fakeDriver{info: driver.Info{Name: "doxie-dx400"}, session: sess})
	id, err := m.StartScan(WithDefaults(settings))
	if err != nil {
		t.Fatalf("StartScan: %v", err)
	}
	if state := finished(t, m); state.Status != storage.StatusCompleted {
		t.Fatalf("status %v: %v", state.Status, state.Err)
	}
	meta, err := store.LoadMeta(id)
	if err != nil {
		t.Fatal(err)
	}
	return sess, meta, store
}

func TestValidateAndDefaults(t *testing.T) {
	if got := WithDefaults(storage.ScanSettings{}); got != DefaultSettings {
		t.Errorf("WithDefaults(empty): got %+v", got)
	}
	if got := WithDefaults(storage.ScanSettings{Resolution: 600, Sides: SidesSingle, Color: "gray"}); got.Resolution != 600 || got.Sides != SidesSingle || got.Color != "gray" {
		t.Errorf("WithDefaults must keep explicit values, got %+v", got)
	}
	for _, bad := range []storage.ScanSettings{
		{Resolution: 150, Sides: SidesAuto, Color: ColorAuto},
		{Resolution: 300, Sides: "triple", Color: ColorAuto},
		{Resolution: 300, Sides: SidesAuto, Color: "sepia"},
	} {
		if err := Validate(bad); err == nil {
			t.Errorf("%+v should be rejected", bad)
		}
	}
}

func TestStartScan_RejectsInvalidSettingsWithoutCreatingAJob(t *testing.T) {
	m, store := startManager(t, &fakeDriver{session: &fakeSession{}})
	if _, err := m.StartScan(storage.ScanSettings{Resolution: 150, Sides: SidesAuto, Color: ColorAuto}); err == nil {
		t.Fatal("expected an error")
	}
	if jobs, _ := store.ListJobs(); len(jobs) != 0 {
		t.Errorf("no job should be created, got %d", len(jobs))
	}
}

func TestStartScan_NoPaperCreatesNoJob(t *testing.T) {
	m, store := startManager(t, &fakeDriver{session: &fakeSession{}})
	if _, err := m.StartScan(DefaultSettings); !errors.Is(err, ErrNoPaper) {
		t.Fatalf("got %v, want ErrNoPaper", err)
	}
	if jobs, _ := store.ListJobs(); len(jobs) != 0 {
		t.Errorf("an empty feeder must not leave an empty job behind, got %d", len(jobs))
	}
}

func TestScan_SingleSidedStoresOriginalJPEGsAndThumbnails(t *testing.T) {
	first, second := side(t, textPage()), side(t, textPage())
	sess, meta, store := scan(t, storage.ScanSettings{Sides: SidesSingle}, sheet(first, nil), sheet(second, nil))

	if len(meta.Pages) != 2 || meta.Pages[0].ID != "001f" || meta.Pages[1].ID != "002f" {
		t.Fatalf("pages: %+v", meta.Pages)
	}
	stored, err := store.LoadPageFile(meta.ID, meta.Pages[0].File)
	if err != nil || !bytes.Equal(stored, first.JPEG) {
		t.Error("the scanner's JPEG must be stored byte for byte")
	}
	p := meta.Pages[0]
	if p.WidthPx != 300 || p.HeightPx != 400 || p.Side != "front" || p.Sheet != 1 {
		t.Errorf("page meta: %+v", p)
	}
	if _, ok := store.LoadRender(meta.ID, p.ID, pageimage.CacheKey(p.Edits, "thumb")); !ok {
		t.Error("a thumbnail should be pre-rendered at scan time")
	}
	if sess.opts[0].Duplex || sess.opts[0].Resolution != 300 {
		t.Errorf("single-sided default scan sent %+v", sess.opts[0])
	}
}

func TestScan_DoubleSidedKeepsBlankBacksVisible(t *testing.T) {
	blank := side(t, page(color.White, 300, 400))
	sess, meta, _ := scan(t, storage.ScanSettings{Sides: SidesDouble}, sheet(side(t, textPage()), &blank))

	if !sess.opts[0].Duplex {
		t.Error("double-sided must scan duplex")
	}
	if len(meta.Pages) != 2 {
		t.Fatalf("pages: %+v", meta.Pages)
	}
	back := meta.Pages[1]
	if back.ID != "001b" || !back.Blank || back.Hidden {
		t.Errorf("a blank back is flagged but never hidden in double-sided mode: %+v", back)
	}
}

func TestScan_AutoSidesHidesBlankBacksButKeepsThem(t *testing.T) {
	blank := side(t, page(color.White, 300, 400))
	inked := side(t, textPage())
	_, meta, store := scan(t, storage.ScanSettings{Sides: SidesAuto},
		sheet(side(t, textPage()), &blank),
		sheet(side(t, textPage()), &inked))

	if len(meta.Pages) != 4 {
		t.Fatalf("every side must be stored, got %d pages", len(meta.Pages))
	}
	if !meta.Pages[1].Hidden || meta.Pages[3].Hidden {
		t.Errorf("only the blank back should be hidden: %+v", meta.Pages)
	}
	if _, err := store.LoadPageFile(meta.ID, meta.Pages[1].File); err != nil {
		t.Error("a hidden page's original must still be on disk so it can be restored")
	}
	if len(meta.VisiblePages()) != 3 {
		t.Errorf("visible pages: %d", len(meta.VisiblePages()))
	}
}

// sparseBack is a short 300 dpi sheet on slightly tinted stock with one
// small block of print in a corner, too little to notice across the whole
// sheet.
func sparseBack() *image.NRGBA {
	img := page(color.Gray{Y: 225}, 2550, 1100)
	for y := 120; y < 340; y += 24 {
		draw.Draw(img, image.Rect(1800, y, 2350, y+5), image.Black, image.Point{}, draw.Src)
	}
	return img
}

func TestScan_AutoSidesKeepsABackWithPrintInOneCorner(t *testing.T) {
	sparse, white := side(t, sparseBack()), side(t, page(color.White, 2550, 1100))
	_, meta, _ := scan(t, storage.ScanSettings{Sides: SidesAuto},
		sheet(side(t, textPage()), &sparse),
		sheet(side(t, textPage()), &white))

	if b := meta.Pages[1]; b.Blank || b.Hidden || b.Ink == nil || b.Ink.Densest < 0.15 {
		t.Errorf("the printed back should be shown, with its ink: blank=%v hidden=%v ink=%+v", b.Blank, b.Hidden, b.Ink)
	}
	if b := meta.Pages[3]; !b.Blank || !b.Hidden || b.Ink == nil || b.Ink.Densest != 0 {
		t.Errorf("the white back should still be hidden: blank=%v hidden=%v ink=%+v", b.Blank, b.Hidden, b.Ink)
	}
	if f := meta.Pages[0]; f.Ink != nil {
		t.Errorf("fronts aren't judged, so carry no ink: %+v", f.Ink)
	}
}

// narrowSheet is a sheet narrower than the scanner, on its gray backing.
func narrowSheet(printed bool) *image.NRGBA {
	img := page(color.NRGBA{176, 178, 177, 255}, 600, 800)
	draw.Draw(img, image.Rect(60, 0, 540, 800), &image.Uniform{color.White}, image.Point{}, draw.Src)
	if printed {
		for y := 60; y < 740; y += 20 {
			draw.Draw(img, image.Rect(100, y, 500, y+6), image.Black, image.Point{}, draw.Src)
		}
	}
	return img
}

func TestScan_CropsToTheSheetAndHidesItsBlankBack(t *testing.T) {
	back := side(t, narrowSheet(false))
	_, meta, _ := scan(t, storage.ScanSettings{Sides: SidesAuto}, sheet(side(t, narrowSheet(true)), &back))

	for _, p := range meta.Pages {
		c := p.Edits.Crop
		if p.Paper == nil || c == nil || c.X < 58 || c.X > 66 || c.X+c.W < 534 || c.X+c.W > 542 {
			t.Errorf("%s: paper %+v crop %+v, want the sheet at x 60..540", p.ID, p.Paper, c)
		}
	}
	if b := meta.Pages[1]; !b.Blank || !b.Hidden {
		t.Errorf("a blank back beside the backing should be hidden: blank=%v hidden=%v", b.Blank, b.Hidden)
	}
	if f := meta.Pages[0]; f.Blank {
		t.Error("the printed front is not blank")
	}
}

func TestScan_ColorModes(t *testing.T) {
	red := page(color.White, 300, 400)
	draw.Draw(red, image.Rect(50, 50, 250, 250), &image.Uniform{color.RGBA{220, 20, 20, 255}}, image.Point{}, draw.Src)

	_, meta, _ := scan(t, storage.ScanSettings{Sides: SidesSingle, Color: ColorAuto},
		sheet(side(t, red), nil), sheet(side(t, textPage()), nil))
	if meta.Pages[0].Edits.Color != pageimage.ColorColor || meta.Pages[1].Edits.Color != pageimage.ColorBW {
		t.Errorf("auto color: got %q and %q", meta.Pages[0].Edits.Color, meta.Pages[1].Edits.Color)
	}

	_, meta, _ = scan(t, storage.ScanSettings{Sides: SidesSingle, Color: "gray"}, sheet(side(t, red), nil))
	if meta.Pages[0].Edits.Color != pageimage.ColorGray {
		t.Errorf("a fixed color setting applies to every page, got %q", meta.Pages[0].Edits.Color)
	}
}

func TestScan_NonSquare200dpiIsSquaredUp(t *testing.T) {
	s := side(t, textPage())
	s.XDPI, s.YDPI = 300, 200
	sess, meta, _ := scan(t, storage.ScanSettings{Resolution: 200, Sides: SidesSingle}, sheet(s, nil))

	if sess.opts[0].Resolution != 200 {
		t.Errorf("resolution not forwarded: %+v", sess.opts[0])
	}
	if p := meta.Pages[0]; p.WidthPx != 200 || p.XDPI != 300 || p.YDPI != 200 {
		t.Errorf("page: %+v", p)
	}
}

func TestStartScan_AlreadyRunning(t *testing.T) {
	m, _ := startManager(t, &fakeDriver{session: &fakeSession{}})
	m.mu.Lock()
	m.running = &JobState{ID: "existing-job", Status: storage.StatusRunning}
	m.mu.Unlock()
	if !m.Scanning() || !m.Device().Scanning {
		t.Error("Scanning should report the running job")
	}
	if _, err := m.StartScan(DefaultSettings); err != ErrAlreadyRunning {
		t.Fatalf("got %v, want ErrAlreadyRunning", err)
	}
}

func TestStartScan_TimesOutWithoutRun(t *testing.T) {
	m := NewManager(&fakeDriver{session: &fakeSession{}}, storage.New(t.TempDir()))
	m.Timings.Reply = 10 * time.Millisecond
	if _, err := m.StartScan(DefaultSettings); !errors.Is(err, ErrScannerUnavailable) {
		t.Errorf("nobody owns the scanner: got %v", err)
	}
}

func TestCurrentJob_NilBeforeAnyScan(t *testing.T) {
	m, _ := startManager(t, &fakeDriver{session: &fakeSession{}})
	if m.CurrentJob() != nil || m.Scanning() {
		t.Error("expected no job before any scan has started")
	}
}

func TestRun_SleepingScannerIsReportedAndRecovered(t *testing.T) {
	drv := &fakeDriver{session: &fakeSession{}, offline: errTest("no USB device")}
	m, _ := startManager(t, drv)

	waitFor(t, "an offline status", func() bool { return m.Device().Err == errTest("no USB device") })
	if m.Device().Connected {
		t.Error("a sleeping scanner is not connected")
	}
	if _, err := m.StartScan(DefaultSettings); !errors.Is(err, ErrScannerUnavailable) {
		t.Errorf("scanning while asleep: got %v", err)
	}

	before := drv.openCount()
	waitFor(t, "a retry", func() bool { return drv.openCount() > before })
	drv.setOffline(nil)
	waitFor(t, "the scanner to reconnect", func() bool { return m.Device().Connected })
}

func TestRun_TracksPaper(t *testing.T) {
	sess := &fakeSession{}
	m, _ := startManager(t, &fakeDriver{session: sess})
	waitFor(t, "the scanner to connect", func() bool { return m.Device().Connected })
	sess.setPages(sheet(side(t, textPage()), nil))
	waitFor(t, "paper to be noticed", func() bool { return m.Device().Paper })
}

func TestRun_ButtonScansWithSavedSettings(t *testing.T) {
	sess := &fakeSession{pages: []driver.Page{sheet(side(t, textPage()), nil)}, pressDuringScan: true}
	m, store := startManager(t, &fakeDriver{session: sess})
	if err := store.SaveSettings(storage.AppSettings{ButtonScan: storage.ScanSettings{Resolution: 600, Sides: SidesSingle}}); err != nil {
		t.Fatal(err)
	}

	sess.press()
	state := finished(t, m)
	meta, err := store.LoadMeta(state.ID)
	if err != nil || len(meta.Pages) != 1 {
		t.Fatalf("button scan: %+v %v", meta, err)
	}
	if meta.Settings != (storage.ScanSettings{Resolution: 600, Sides: SidesSingle, Color: ColorAuto}) {
		t.Errorf("button scans use the saved settings, got %+v", meta.Settings)
	}

	time.Sleep(50 * time.Millisecond)
	if jobs, _ := store.ListJobs(); len(jobs) != 1 {
		t.Errorf("a press made during the scan must not start another one, got %d jobs", len(jobs))
	}
}

func TestRun_ButtonWithoutPaperDoesNothing(t *testing.T) {
	sess := &fakeSession{}
	m, store := startManager(t, &fakeDriver{session: sess})
	waitFor(t, "the scanner to connect", func() bool { return m.Device().Connected })
	sess.press()
	time.Sleep(50 * time.Millisecond)
	if jobs, _ := store.ListJobs(); len(jobs) != 0 || m.CurrentJob() != nil {
		t.Errorf("a press with an empty feeder must not create a job, got %d", len(jobs))
	}
}

func TestButtonSettings_FallBackToDefaults(t *testing.T) {
	store := storage.New(t.TempDir())
	m := NewManager(&fakeDriver{}, store)
	if m.ButtonSettings() != DefaultSettings {
		t.Error("no saved settings means the defaults")
	}
	if err := store.SaveSettings(storage.AppSettings{ButtonScan: storage.ScanSettings{Resolution: 150}}); err != nil {
		t.Fatal(err)
	}
	if m.ButtonSettings() != DefaultSettings {
		t.Error("invalid saved settings fall back to the defaults")
	}
	if err := os.WriteFile(store.Root()+"/settings.json", []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if m.ButtonSettings() != DefaultSettings {
		t.Error("unreadable settings fall back to the defaults")
	}
}

func TestRun_SessionErrorsReconnect(t *testing.T) {
	for name, sess := range map[string]*fakeSession{
		"button": {buttonErr: errTest("pipe stalled")},
		"sensor": {hasNextErr: errTest("sensor failed")},
	} {
		drv := &fakeDriver{session: sess}
		m, _ := startManager(t, drv)
		waitFor(t, name+" reconnects", func() bool { return drv.openCount() >= 3 })
		if _, err := m.StartScan(DefaultSettings); err == nil {
			t.Errorf("%s: a scan on a failing session should not succeed", name)
		}
	}
}

func failedScan(t *testing.T, sess *fakeSession) (*JobState, storage.JobMeta) {
	t.Helper()
	drv := &fakeDriver{session: sess}
	m, store := startManager(t, drv)
	id, err := m.StartScan(DefaultSettings)
	if err != nil {
		t.Fatalf("StartScan: %v", err)
	}
	state := finished(t, m)
	if state.Status != storage.StatusFailed || state.Err == nil {
		t.Fatalf("expected a failed job, got %v (%v)", state.Status, state.Err)
	}
	meta, err := store.LoadMeta(id)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Status != storage.StatusFailed || meta.Error == "" {
		t.Errorf("meta: %+v", meta)
	}
	waitFor(t, "a fresh session after the failure", func() bool { return drv.openCount() >= 2 })
	return state, meta
}

func TestScan_Failures(t *testing.T) {
	undecodable := driver.Side{JPEG: []byte("not a jpeg"), XDPI: 300, YDPI: 300}
	good := side(t, textPage())
	failedScan(t, &fakeSession{pages: []driver.Page{sheet(undecodable, nil)}})
	failedScan(t, &fakeSession{pages: []driver.Page{sheet(good, &undecodable)}})
}

func TestScan_FailureMidLoopKeepsEarlierPages(t *testing.T) {
	good := side(t, textPage())
	state, meta := failedScan(t, &fakeSession{
		pages:     []driver.Page{sheet(good, nil), sheet(good, nil)},
		scanErr:   errTest("paper jam"),
		scanErrAt: 2,
	})
	if state.PagesScanned != 1 || len(meta.Pages) != 1 {
		t.Errorf("sheet 1 should be kept: scanned=%d pages=%d", state.PagesScanned, len(meta.Pages))
	}
}

// The DX400 pulls in the next sheet as soon as one ends and abandons it
// partway through if the host is slow to ask for it. On a Raspberry Pi 3
// processing a sheet takes seconds, so it must not hold up feeding.
func TestScan_FeedsTheNextSheetWithoutWaitingForProcessing(t *testing.T) {
	good := side(t, textPage())
	sess := &fakeSession{pages: []driver.Page{sheet(good, nil), sheet(good, nil), sheet(good, nil)}}
	m, store := startManager(t, &fakeDriver{session: sess})
	release := make(chan struct{})
	var once sync.Once
	finishProcessing := func() { once.Do(func() { close(release) }) }
	t.Cleanup(finishProcessing)
	process := m.process
	m.process = func(meta *storage.JobMeta, s scannedSheet) error {
		<-release
		return process(meta, s)
	}

	id, err := m.StartScan(WithDefaults(storage.ScanSettings{Sides: SidesSingle}))
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "every sheet to be fed while the first is still processing", func() bool { return sess.fed() == 3 })
	finishProcessing()

	if state := finished(t, m); state.Status != storage.StatusCompleted || state.PagesScanned != 3 {
		t.Fatalf("status %v, %d pages: %v", state.Status, state.PagesScanned, state.Err)
	}
	if meta, _ := store.LoadMeta(id); len(meta.Pages) != 3 {
		t.Errorf("stored %d pages, want 3", len(meta.Pages))
	}
}

func TestScan_ProcessingFailureStopsFeeding(t *testing.T) {
	good := side(t, textPage())
	pages := make([]driver.Page, 200)
	for i := range pages {
		pages[i] = sheet(good, nil)
	}
	sess := &fakeSession{pages: pages}
	m, _ := startManager(t, &fakeDriver{session: sess})
	process := m.process
	m.process = func(meta *storage.JobMeta, s scannedSheet) error {
		if s.number == 1 {
			return errTest("unreadable sheet")
		}
		return process(meta, s)
	}

	if _, err := m.StartScan(DefaultSettings); err != nil {
		t.Fatal(err)
	}
	state := finished(t, m)
	if state.Status != storage.StatusFailed || !errors.Is(state.Err, errTest("unreadable sheet")) {
		t.Fatalf("status %v: %v", state.Status, state.Err)
	}
	if fed := sess.fed(); fed >= len(pages) {
		t.Errorf("fed all %d sheets after the first could not be processed", fed)
	}
}

// Once processing has failed, the sheet already moving is read to the end
// so it leaves the scanner, and no further sheet is asked for.
func TestFeed_FinishesTheMovingSheetThenAsksForNoMore(t *testing.T) {
	good := side(t, textPage())
	stack := func() []driver.Page { return []driver.Page{sheet(good, nil), sheet(good, nil), sheet(good, nil)} }
	cases := map[string]struct {
		failDuring func(sess *fakeSession, fail func())
		wantFed    int
	}{
		"while a sheet is moving": {func(sess *fakeSession, fail func()) {
			sess.onScan = func(n int) {
				if n == 2 {
					fail()
				}
			}
		}, 2},
		"while checking for paper": {func(sess *fakeSession, fail func()) {
			sess.onHasNext = func(n int) {
				if n == 2 {
					fail()
				}
			}
		}, 1},
	}
	for name, c := range cases {
		sess := &fakeSession{pages: stack()}
		stop := make(chan struct{})
		c.failDuring(sess, func() { close(stop) })
		sheets := make(chan scannedSheet, sheetQueue)

		if err := feed(context.Background(), sess, driver.ScanOptions{}, sheets, stop); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if sess.fed() != c.wantFed || len(sheets) != c.wantFed {
			t.Errorf("%s: fed %d sheets and kept %d, want %d", name, sess.fed(), len(sheets), c.wantFed)
		}
	}
}

func TestScan_EndsTheScannersBatchOnceFeedingStops(t *testing.T) {
	good := side(t, textPage())
	sess, _, _ := scan(t, storage.ScanSettings{Sides: SidesSingle}, sheet(good, nil), sheet(good, nil))
	if n := sess.batchesEnded(); n != 1 {
		t.Errorf("after the feeder emptied: ended the batch %d times, want 1", n)
	}

	jammed := &fakeSession{pages: []driver.Page{sheet(good, nil), sheet(good, nil)}, scanErr: errTest("paper jam"), scanErrAt: 2}
	failedScan(t, jammed)
	if n := jammed.batchesEnded(); n != 1 {
		t.Errorf("after a failed sheet: ended the batch %d times, want 1", n)
	}

	refused := &fakeSession{pages: []driver.Page{sheet(good, nil)}, endBatchErr: errTest("release refused")}
	if state, _ := failedScan(t, refused); !errors.Is(state.Err, errTest("release refused")) {
		t.Errorf("a failed release should fail the scan, got %v", state.Err)
	}
}

func TestStartScan_CreateJobError(t *testing.T) {
	badRoot := t.TempDir() + "/blocker"
	if err := os.WriteFile(badRoot, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	drv := &fakeDriver{session: &fakeSession{pages: []driver.Page{sheet(side(t, textPage()), nil)}}}
	m := NewManager(drv, storage.New(badRoot+"/data"))
	m.Timings = Timings{Reconnect: 20 * time.Millisecond, ButtonPoll: 5 * time.Millisecond, Reply: 2 * time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Run(ctx)
	if _, err := m.StartScan(DefaultSettings); err == nil {
		t.Fatal("expected an error")
	}
}

func TestScan_SavePageError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission-based test is unreliable")
	}
	sess := &fakeSession{pages: []driver.Page{sheet(side(t, textPage()), nil)}}
	store := storage.New(t.TempDir())
	m := NewManager(&fakeDriver{session: sess}, store)

	meta, err := m.createJob(DefaultSettings)
	if err != nil {
		t.Fatal(err)
	}
	pagesDir := store.Root() + "/jobs/" + meta.ID + "/pages"
	if err := os.Chmod(pagesDir, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(pagesDir, 0o755)

	if err := m.run(context.Background(), sess, meta); err == nil {
		t.Fatal("expected the save failure to surface")
	}
	if state := m.CurrentJob(); state.Status != storage.StatusFailed {
		t.Fatalf("status: got %v (err=%v)", state.Status, state.Err)
	}
}

type errTest string

func (e errTest) Error() string { return string(e) }
