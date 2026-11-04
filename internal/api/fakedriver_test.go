package api

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/andiapps-dev/doxie-scanner/internal/driver"
)

// fakeSession has paper while scripted sheets remain. If block is set,
// ScanPage waits on it, holding the job "running" until the test lets go.
type fakeSession struct {
	mu    sync.Mutex
	pages []driver.Page
	idx   int
	block chan struct{}
}

func (s *fakeSession) HasNextPage(ctx context.Context) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.idx < len(s.pages), nil
}

func (s *fakeSession) ScanPage(ctx context.Context, opts driver.ScanOptions) (driver.Page, error) {
	if s.block != nil {
		<-s.block
		return driver.Page{}, errors.New("released")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.pages[s.idx]
	s.idx++
	return p, nil
}

func (s *fakeSession) WaitForButton(ctx context.Context, timeout time.Duration) (bool, error) {
	select {
	case <-time.After(timeout):
	case <-ctx.Done():
	}
	return false, nil
}

func (s *fakeSession) EndBatch(ctx context.Context) error { return nil }

func (s *fakeSession) Close() error { return nil }

// fakeDriver hands out its one session, or openErr while the scanner is
// "asleep".
type fakeDriver struct {
	info    driver.Info
	session *fakeSession
	openErr error
}

func (d *fakeDriver) Info() driver.Info { return d.info }

func (d *fakeDriver) Open(ctx context.Context) (driver.Session, error) {
	if d.openErr != nil {
		return nil, d.openErr
	}
	if d.session == nil {
		return &fakeSession{}, nil
	}
	return d.session, nil
}
