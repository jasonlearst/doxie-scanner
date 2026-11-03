package doxiedx400

import (
	"context"

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
	return scsiusb.Open(VID, PID, usbInterfaceNum, epBulkIn&0x0f, epBulkOut&0x0f)
}

func (d *dxDriver) Detect(ctx context.Context) error {
	dev, err := openDeviceFunc()
	if err != nil {
		return err
	}
	return dev.Close()
}

func (d *dxDriver) Open(ctx context.Context) (driver.Session, error) {
	dev, err := openDeviceFunc()
	if err != nil {
		return nil, err
	}
	return &session{dev: dev}, nil
}

type session struct {
	dev scsiusb.BulkDevice
}

func (s *session) HasNextPage(ctx context.Context) (bool, error) {
	return hasPaper(s.dev)
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
	sides, err := scanSheet(s.dev, w, quantTables[res])
	if err != nil {
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

func (s *session) Close() error {
	return s.dev.Close()
}
