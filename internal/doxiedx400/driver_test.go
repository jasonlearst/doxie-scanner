package doxiedx400

import (
	"context"
	"errors"
	"image"
	"testing"

	"github.com/andiapps-dev/doxie-scanner/internal/driver"
	"github.com/andiapps-dev/doxie-scanner/internal/scsiusb"
)

func withFakeOpenDevice(t *testing.T, dev scsiusb.BulkDevice, err error) {
	t.Helper()
	original := openDeviceFunc
	openDeviceFunc = func() (scsiusb.BulkDevice, error) { return dev, err }
	t.Cleanup(func() { openDeviceFunc = original })
}

func openFake(t *testing.T, dev *fakeDevice) driver.Session {
	t.Helper()
	withFakeOpenDevice(t, dev, nil)
	sess, err := New().Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return sess
}

func TestDriverInfo(t *testing.T) {
	info := New().Info()
	if info.Name != "doxie-dx400" || info.VID != VID || info.PID != PID {
		t.Errorf("got %+v", info)
	}
}

func TestDetect(t *testing.T) {
	dev := &fakeDevice{t: t}
	withFakeOpenDevice(t, dev, nil)
	if err := New().Detect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !dev.closed {
		t.Error("Detect should close the device after probing")
	}

	withFakeOpenDevice(t, nil, errors.New("no device"))
	if err := New().Detect(context.Background()); err == nil {
		t.Error("expected the open error")
	}
	if _, err := New().Open(context.Background()); err == nil {
		t.Error("expected the open error from Open too")
	}

	withFakeOpenDevice(t, &fakeDevice{t: t, closeErr: errors.New("close failed")}, nil)
	if err := New().Detect(context.Background()); err == nil {
		t.Error("expected the close error")
	}
}

func TestHasNextPage(t *testing.T) {
	dev := &fakeDevice{t: t}
	dev.ok([]byte{0x00, 0x00, 0x02, 0x05, 0x01})
	ok, err := openFake(t, dev).HasNextPage(context.Background())
	if err != nil || !ok {
		t.Fatalf("got %v, %v", ok, err)
	}
}

func TestScanPage_DuplexDropsBlankBack(t *testing.T) {
	dev := &fakeDevice{t: t}
	dev.scriptSheet(true,
		scriptedSide{fixture(t, "ink-300.scan"), 32},
		scriptedSide{fixture(t, "blank-300.scan"), 32})

	page, err := openFake(t, dev).ScanPage(context.Background(), driver.ScanOptions{Duplex: true})
	if err != nil {
		t.Fatal(err)
	}
	if page.Front == nil || page.Front.Bounds().Size() != (image.Point{2550, 32}) {
		t.Errorf("front: %v", page.Front)
	}
	if !page.BackBlank || page.Back != nil {
		t.Errorf("blank back should be dropped: BackBlank=%v Back=%v", page.BackBlank, page.Back)
	}
}

func TestScanPage_DuplexKeepsInkedBack(t *testing.T) {
	dev := &fakeDevice{t: t}
	ink := fixture(t, "ink-300.scan")
	dev.scriptSheet(true, scriptedSide{ink, 32}, scriptedSide{ink, 32})

	page, err := openFake(t, dev).ScanPage(context.Background(), driver.ScanOptions{Duplex: true, Resolution: 300})
	if err != nil {
		t.Fatal(err)
	}
	if page.Back == nil || page.BackBlank {
		t.Error("an inked back should be kept")
	}
}

func TestScanPage_SimplexDefaultsTo300(t *testing.T) {
	dev := &fakeDevice{t: t}
	dev.scriptSheet(false, scriptedSide{fixture(t, "ink-300.scan"), 32})

	page, err := openFake(t, dev).ScanPage(context.Background(), driver.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Back != nil || page.BackBlank {
		t.Error("simplex scan should have no back")
	}
	window := dev.payloads[0]
	if window[offYRes] != 0x01 || window[offYRes+1] != 0x2c {
		t.Errorf("default resolution should be 300 dpi, window yres % x", window[offYRes:offYRes+2])
	}
}

func TestScanPage_RejectsUnsupportedResolution(t *testing.T) {
	dev := &fakeDevice{t: t}
	if _, err := openFake(t, dev).ScanPage(context.Background(), driver.ScanOptions{Resolution: 150}); err == nil {
		t.Fatal("expected 150 dpi to be rejected")
	}
	if len(dev.commands) != 0 {
		t.Errorf("nothing should be sent for an invalid resolution, sent %d commands", len(dev.commands))
	}
}

func TestScanPage_SurfacesScanErrors(t *testing.T) {
	dev := &fakeDevice{t: t}
	dev.checkCondition(nil, senseBlock(ascADF, ascqChuteEmpty, 0, 0, 0))
	if _, err := openFake(t, dev).ScanPage(context.Background(), driver.ScanOptions{}); !errors.Is(err, ErrNoPaper) {
		t.Fatalf("got %v, want ErrNoPaper", err)
	}
}

func TestSessionClose(t *testing.T) {
	dev := &fakeDevice{t: t}
	if err := openFake(t, dev).Close(); err != nil || !dev.closed {
		t.Fatalf("close: %v, closed=%v", err, dev.closed)
	}
}

func TestScanPage_UndecodableSideFails(t *testing.T) {
	ink := fixture(t, "ink-300.scan")
	garbage := scriptedSide{[]byte{0x01, 0x02, 0x03}, 32}
	for name, sides := range map[string][]scriptedSide{
		"front": {garbage, {ink, 32}},
		"back":  {{ink, 32}, garbage},
	} {
		dev := &fakeDevice{t: t}
		dev.scriptSheet(true, sides...)
		if _, err := openFake(t, dev).ScanPage(context.Background(), driver.ScanOptions{Duplex: true}); err == nil {
			t.Errorf("%s: expected a decode error", name)
		}
	}
}
