package scanjobs

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"os"
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

func waitForJobDone(t *testing.T, m *Manager) *JobState {
	t.Helper()
	// Generous margin: a throttled CI runner can starve the background
	// goroutine well past what these fakes need locally.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		state := m.CurrentJob()
		if state != nil && state.Status != storage.StatusRunning {
			return state
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for job to finish")
	return nil
}

func newTestManager(t *testing.T, drv driver.Driver) (*Manager, *storage.Store) {
	t.Helper()
	store := storage.New(t.TempDir())
	return NewManager(drv, store), store
}

func scan(t *testing.T, settings storage.ScanSettings, sheets ...driver.Page) (*fakeSession, storage.JobMeta, *storage.Store) {
	t.Helper()
	sess := &fakeSession{pages: sheets}
	m, store := newTestManager(t, &fakeDriver{info: driver.Info{Name: "doxie-dx400"}, session: sess})
	id, err := m.StartScan(WithDefaults(settings))
	if err != nil {
		t.Fatalf("StartScan: %v", err)
	}
	if state := waitForJobDone(t, m); state.Status != storage.StatusCompleted {
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
	m, store := newTestManager(t, &fakeDriver{session: &fakeSession{}})
	if _, err := m.StartScan(storage.ScanSettings{Resolution: 150, Sides: SidesAuto, Color: ColorAuto}); err == nil {
		t.Fatal("expected an error")
	}
	if jobs, _ := store.ListJobs(); len(jobs) != 0 {
		t.Errorf("no job should be created, got %d", len(jobs))
	}
}

func TestScan_ZeroSheets(t *testing.T) {
	sess, meta, _ := scan(t, storage.ScanSettings{})
	if len(meta.Pages) != 0 || meta.Status != storage.StatusCompleted || !sess.closed {
		t.Errorf("meta %+v, session closed=%v", meta, sess.closed)
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
	m, _ := newTestManager(t, &fakeDriver{session: &fakeSession{}})
	m.running = &JobState{ID: "existing-job", Status: storage.StatusRunning}
	if !m.Scanning() {
		t.Error("Scanning should report the running job")
	}
	if _, err := m.StartScan(DefaultSettings); err != ErrAlreadyRunning {
		t.Fatalf("got %v, want ErrAlreadyRunning", err)
	}
}

func TestCurrentJob_NilBeforeAnyScan(t *testing.T) {
	m, _ := newTestManager(t, &fakeDriver{session: &fakeSession{}})
	if m.CurrentJob() != nil || m.Scanning() {
		t.Error("expected no job before any scan has started")
	}
}

func failedScan(t *testing.T, drv *fakeDriver) (*JobState, storage.JobMeta) {
	t.Helper()
	m, store := newTestManager(t, drv)
	id, err := m.StartScan(DefaultSettings)
	if err != nil {
		t.Fatalf("StartScan should succeed even though the background run fails: %v", err)
	}
	state := waitForJobDone(t, m)
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
	return state, meta
}

func TestScan_Failures(t *testing.T) {
	failedScan(t, &fakeDriver{openErr: errTest("no device")})
	failedScan(t, &fakeDriver{session: &fakeSession{hasNextErr: errTest("sensor failed")}})

	undecodable := driver.Side{JPEG: []byte("not a jpeg"), XDPI: 300, YDPI: 300}
	failedScan(t, &fakeDriver{session: &fakeSession{pages: []driver.Page{sheet(undecodable, nil)}}})

	good := side(t, textPage())
	failedScan(t, &fakeDriver{session: &fakeSession{pages: []driver.Page{sheet(good, &undecodable)}}})
}

func TestScan_FailureMidLoopKeepsEarlierPages(t *testing.T) {
	good := side(t, textPage())
	state, meta := failedScan(t, &fakeDriver{session: &fakeSession{
		pages:     []driver.Page{sheet(good, nil), sheet(good, nil)},
		scanErr:   errTest("paper jam"),
		scanErrAt: 2,
	}})
	if state.PagesScanned != 1 || len(meta.Pages) != 1 {
		t.Errorf("sheet 1 should be kept: scanned=%d pages=%d", state.PagesScanned, len(meta.Pages))
	}
}

func TestStartScan_CreateJobError(t *testing.T) {
	badRoot := t.TempDir() + "/blocker"
	if err := os.WriteFile(badRoot, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewManager(&fakeDriver{session: &fakeSession{}}, storage.New(badRoot+"/data"))
	if _, err := m.StartScan(DefaultSettings); err == nil {
		t.Fatal("expected an error")
	}
	if state := m.CurrentJob(); state == nil || state.Status != storage.StatusFailed {
		t.Errorf("state: %+v", state)
	}
}

func TestScan_SavePageError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission-based test is unreliable")
	}
	drv := &fakeDriver{session: &fakeSession{pages: []driver.Page{sheet(side(t, textPage()), nil)}}}
	m, store := newTestManager(t, drv)

	// Drive run() synchronously so the pages directory can be made
	// unwritable before the scan touches it.
	now := time.Now()
	meta := storage.JobMeta{ID: storage.NewJobID(now), CreatedAt: now, Status: storage.StatusRunning, Settings: DefaultSettings}
	if err := store.CreateJob(meta); err != nil {
		t.Fatal(err)
	}
	m.running = &JobState{ID: meta.ID, Status: storage.StatusRunning}
	pagesDir := store.Root() + "/jobs/" + meta.ID + "/pages"
	if err := os.Chmod(pagesDir, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(pagesDir, 0o755)

	m.run(context.Background(), meta.ID, meta)
	if state := m.CurrentJob(); state.Status != storage.StatusFailed {
		t.Fatalf("status: got %v (err=%v)", state.Status, state.Err)
	}
}

type errTest string

func (e errTest) Error() string { return string(e) }
