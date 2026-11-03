// Package driver defines the minimal, deliberately small interface a
// scanner implementation must satisfy to be usable by this application.
// It exists so a second scanner model could be added later (reusing
// internal/scsiusb for the generic USB-bulk/SCSI-CDB transport) without
// touching any of the job/storage/API/frontend code, which only ever
// talks to a Driver/Session, never to a concrete scanner package
// directly.
package driver

import (
	"context"
	"time"
)

// Info describes a driver for display/diagnostic purposes.
type Info struct {
	Name   string // registry key, e.g. "doxie-dx400"
	Vendor string
	Model  string
	VID    uint16
	PID    uint16
}

// ScanOptions controls how a single page is scanned.
type ScanOptions struct {
	// Duplex requests both sides of the page. Not all drivers support
	// this; drivers that don't should simply ignore it and always scan
	// simplex.
	Duplex bool
	// Resolution in dpi; zero means the driver's default.
	Resolution int
}

// Side is one side of a sheet exactly as the scanner delivered it.
type Side struct {
	// JPEG is a complete JPEG file.
	JPEG []byte
	// XDPI and YDPI are the sampling resolution. They differ when the
	// scanner reads fewer lines per inch than columns (200 dpi is read at
	// 300 across), so consumers must square up the pixels.
	XDPI, YDPI int
}

// Page is the result of scanning one physical sheet. A driver never
// drops a side: deciding what to do with a blank back is the caller's
// call, not the hardware layer's.
type Page struct {
	Front Side
	// Back is nil only for a simplex scan.
	Back *Side
}

// Session represents one open connection to a scanner, spanning
// potentially many pages (e.g. a whole ADF batch).
type Session interface {
	// HasNextPage reports whether the feeder currently has a sheet ready
	// to scan. Callers should stop looping once this returns false.
	HasNextPage(ctx context.Context) (bool, error)
	// ScanPage scans the next sheet. Callers must have confirmed
	// HasNextPage first.
	ScanPage(ctx context.Context, opts ScanOptions) (Page, error)
	// WaitForButton waits up to timeout for the scanner's own scan button
	// and reports whether it was pressed.
	WaitForButton(ctx context.Context, timeout time.Duration) (bool, error)
	// Close releases the underlying device.
	Close() error
}

// Driver is a scanner model implementation.
type Driver interface {
	Info() Info
	// Open opens a session; it fails while the scanner is unplugged, off,
	// or asleep.
	Open(ctx context.Context) (Session, error)
}
