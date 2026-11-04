package scanjobs

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/andiapps-dev/doxie-scanner/internal/driver"
)

// fakeSession is an in-memory driver.Session. Paper is present while
// scripted sheets remain.
type fakeSession struct {
	mu sync.Mutex

	pages []driver.Page
	idx   int

	hasNextErr error
	scanErr    error
	scanErrAt  int // 1-based sheet at which ScanPage returns scanErr; 0 = never
	buttonErr  error
	presses    int // pending presses of the scanner's button

	// pressDuringScan queues a press while a sheet is being scanned.
	pressDuringScan bool

	closed int
	opts   []driver.ScanOptions
}

func (s *fakeSession) HasNextPage(ctx context.Context) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hasNextErr != nil {
		return false, s.hasNextErr
	}
	return s.idx < len(s.pages), nil
}

func (s *fakeSession) ScanPage(ctx context.Context, opts driver.ScanOptions) (driver.Page, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.idx++
	s.opts = append(s.opts, opts)
	if s.pressDuringScan {
		s.presses++
	}
	if s.scanErr != nil && s.idx == s.scanErrAt {
		return driver.Page{}, s.scanErr
	}
	if s.idx-1 >= len(s.pages) {
		return driver.Page{}, errors.New("fakeSession: ScanPage called with no page scripted")
	}
	return s.pages[s.idx-1], nil
}

// fed is how many sheets ScanPage has been asked for.
func (s *fakeSession) fed() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.idx
}

func (s *fakeSession) WaitForButton(ctx context.Context, timeout time.Duration) (bool, error) {
	s.mu.Lock()
	if s.buttonErr != nil {
		err := s.buttonErr
		s.mu.Unlock()
		return false, err
	}
	if s.presses > 0 {
		s.presses--
		s.mu.Unlock()
		return true, nil
	}
	s.mu.Unlock()
	select {
	case <-time.After(timeout):
	case <-ctx.Done():
	}
	return false, nil
}

func (s *fakeSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed++
	return nil
}

func (s *fakeSession) press() {
	s.mu.Lock()
	s.presses++
	s.mu.Unlock()
}

func (s *fakeSession) setPages(pages ...driver.Page) {
	s.mu.Lock()
	s.pages, s.idx = pages, 0
	s.mu.Unlock()
}

// fakeDriver hands out its one session; while offline is set, Open fails
// the way a sleeping scanner does.
type fakeDriver struct {
	info    driver.Info
	session *fakeSession

	mu      sync.Mutex
	offline error
	opens   int
}

func (d *fakeDriver) Info() driver.Info { return d.info }

func (d *fakeDriver) Open(ctx context.Context) (driver.Session, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.opens++
	if d.offline != nil {
		return nil, d.offline
	}
	return d.session, nil
}

func (d *fakeDriver) setOffline(err error) {
	d.mu.Lock()
	d.offline = err
	d.mu.Unlock()
}

func (d *fakeDriver) openCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.opens
}
