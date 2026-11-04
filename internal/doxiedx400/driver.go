package doxiedx400

import (
	"context"
	"errors"
	"time"

	"github.com/andiapps-dev/doxie-scanner/internal/driver"
	"github.com/andiapps-dev/doxie-scanner/internal/scsiusb"
)

const defaultResolution = 300

func init() {
	driver.Register("doxie-dx400", New)
}

// New constructs a Doxie Pro DX400 driver.Driver.
func New() driver.Driver { return &dxDriver{} }

type dxDriver struct{}

func (d *dxDriver) Info() driver.Info {
	return driver.Info{
		Name:   "doxie-dx400",
		Vendor: "Avision (Doxie)",
		Model:  "Doxie Pro DX400",
		VID:    VID,
		PID:    PID,
	}
}

// openDeviceFunc is swapped out by tests for a fake device.
var openDeviceFunc = func() (scsiusb.BulkDevice, error) {
	return scsiusb.Open(VID, PID, usbInterfaceNum, epBulkIn&0x0f, epBulkOut&0x0f, epInterrupt&0x0f)
}

func (d *dxDriver) Open(ctx context.Context) (driver.Session, error) {
	dev, err := openDeviceFunc()
	if err != nil {
		return nil, err
	}
	return &session{dev: dev, sleep: time.Sleep}, nil
}

// Within a stack, the next sheet can take a moment to reach the paper
// sensor after the last one leaves.
const (
	nextSheetWait = time.Second
	nextSheetPoll = 100 * time.Millisecond
)

// session follows Doxie's driver, which keeps the scan job reserved for a
// whole stack and only releases the scan unit between sheets. Releasing
// the job after every sheet left the next sheet half fed.
type session struct {
	dev   scsiusb.BulkDevice
	batch *window // the open stack's settings; nil between stacks
	sleep func(time.Duration)
}

// HasNextPage reports whether paper is waiting. Inside a stack, "no
// paper" only counts once it has lasted nextSheetWait.
func (s *session) HasNextPage(ctx context.Context) (bool, error) {
	paper, err := hasPaper(s.dev)
	for waited := time.Duration(0); err == nil && !paper && s.batch != nil && waited < nextSheetWait; waited += nextSheetPoll {
		s.sleep(nextSheetPoll)
		paper, err = hasPaper(s.dev)
	}
	return paper, err
}

func (s *session) ScanPage(ctx context.Context, opts driver.ScanOptions) (driver.Page, error) {
	res := opts.Resolution
	if res == 0 {
		res = defaultResolution
	}
	w, err := newWindow(res, opts.Duplex)
	if err != nil {
		return driver.Page{}, err
	}
	sides, err := s.scanSheet(w, quantTables[res])
	if err != nil {
		_ = s.EndBatch(ctx) // the scan error is what the person needs to see
		return driver.Page{}, err
	}

	side := func(jpg []byte) driver.Side { return driver.Side{JPEG: jpg, XDPI: w.xRes, YDPI: w.yRes} }
	page := driver.Page{Front: side(sides[0])}
	if opts.Duplex {
		back := side(sides[1])
		page.Back = &back
	}
	return page, nil
}

// WaitForButton listens on the interrupt pipe, where a press arrives as
// an 8-byte packet whose first byte has the 0x80 "pressed" flag set.
func (s *session) WaitForButton(ctx context.Context, timeout time.Duration) (bool, error) {
	ir, ok := s.dev.(scsiusb.InterruptReader)
	if !ok {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	buf := make([]byte, buttonPacketLen)
	n, err := ir.ReadInterrupt(ctx, buf)
	if err != nil {
		if ctx.Err() != nil {
			return false, nil
		}
		return false, err
	}
	return n > 0 && buf[0]&buttonPressed != 0, nil
}

// scanSheet starts the batch with the full setup on its first sheet and
// runs the shorter one before every later sheet.
func (s *session) scanSheet(w window, quant []byte) ([][]byte, error) {
	if s.batch != nil && *s.batch != w {
		if err := s.EndBatch(context.Background()); err != nil {
			return nil, err
		}
	}
	if s.batch == nil {
		if err := control(s.dev, opReserve, flagJob); err != nil {
			return nil, err
		}
		s.batch = &w
		if err := prepare(s.dev, w, quant); err != nil {
			return nil, err
		}
	} else if err := prepareNext(s.dev, w); err != nil {
		return nil, err
	}
	return scanSheet(s.dev, w, quant)
}

// EndBatch releases the scan job held for the stack.
func (s *session) EndBatch(ctx context.Context) error {
	if s.batch == nil {
		return nil
	}
	s.batch = nil
	return control(s.dev, opRelease, flagJob)
}

func (s *session) Close() error {
	return errors.Join(s.EndBatch(context.Background()), s.dev.Close())
}
