package doxiedx400

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
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

func decodes(t *testing.T, s driver.Side) image.Point {
	t.Helper()
	img, err := jpeg.Decode(bytes.NewReader(s.JPEG))
	if err != nil {
		t.Fatalf("side does not decode: %v", err)
	}
	return img.Bounds().Size()
}

func TestScanPage_DuplexReturnsBothSidesEvenWhenBlank(t *testing.T) {
	dev := &fakeDevice{t: t}
	dev.scriptSheet(true,
		scriptedSide{fixture(t, "ink-300.scan"), 32},
		scriptedSide{fixture(t, "blank-300.scan"), 32})

	page, err := openFake(t, dev).ScanPage(context.Background(), driver.ScanOptions{Duplex: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := decodes(t, page.Front); got != (image.Point{2550, 32}) {
		t.Errorf("front: %v", got)
	}
	if page.Back == nil {
		t.Fatal("a blank back must still be returned; dropping it is the caller's decision")
	}
	decodes(t, *page.Back)
	if page.Front.XDPI != 300 || page.Front.YDPI != 300 {
		t.Errorf("dpi: %dx%d", page.Front.XDPI, page.Front.YDPI)
	}
}

func TestScanPage_200dpiReportsNonSquareSampling(t *testing.T) {
	dev := &fakeDevice{t: t}
	dev.scriptSheet(false, scriptedSide{fixture(t, "ink-300.scan"), 32})

	page, err := openFake(t, dev).ScanPage(context.Background(), driver.ScanOptions{Resolution: 200})
	if err != nil {
		t.Fatal(err)
	}
	if page.Front.XDPI != 300 || page.Front.YDPI != 200 {
		t.Errorf("200 dpi is read at 300 across: got %dx%d", page.Front.XDPI, page.Front.YDPI)
	}
}

func TestScanPage_SimplexDefaultsTo300(t *testing.T) {
	dev := &fakeDevice{t: t}
	dev.scriptSheet(false, scriptedSide{fixture(t, "ink-300.scan"), 32})

	page, err := openFake(t, dev).ScanPage(context.Background(), driver.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Back != nil {
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
