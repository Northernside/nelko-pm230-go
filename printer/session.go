package printer

import (
	"context"
	"sync"
	"time"
)

const idleClose = 30 * time.Second

type Session struct {
	turn chan struct{}

	mu     sync.Mutex
	holder string
	dev    string
	idle   Link
	timer  *time.Timer
}

func NewSession() *Session { return &Session{turn: make(chan struct{}, 1)} }

type BusyError struct{ Holder string }

func (e *BusyError) Error() string {
	if e.Holder == "" {
		return "the printer is busy"
	}
	return "the printer is busy (" + e.Holder + ")"
}

// ctx ending before it is our turn -> *BusyError naming whoever has the printer
func (s *Session) acquire(ctx context.Context, who string) (release func(), err error) {
	if s == nil {
		return func() {}, nil
	}

	select {
	case s.turn <- struct{}{}:
	case <-ctx.Done():
		s.mu.Lock()
		defer s.mu.Unlock()
		return nil, &BusyError{Holder: s.holder}
	}

	s.mu.Lock()
	s.holder = who
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		s.holder = ""
		s.mu.Unlock()
		<-s.turn
	}, nil
}

// optional on a Link
type aliver interface{ Alive() bool }
type flusher interface{ Flush() }

// the kept link for dev with the last job's replies dropped, or nil
func (s *Session) take(dev string) Link {
	if s == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	l := s.idle
	if l == nil {
		return nil
	}

	s.idle = nil
	s.timer.Stop()

	if a, ok := l.(aliver); s.dev != dev || (ok && !a.Alive()) {
		l.Close()
		return nil
	}

	if f, ok := l.(flusher); ok {
		f.Flush()
	}

	return l
}

func (s *Session) keep(dev string, l Link) {
	if s == nil {
		l.Close()
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.idle != nil {
		s.timer.Stop()
		s.idle.Close()
	}

	s.dev, s.idle = dev, l
	s.timer = time.AfterFunc(idleClose, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.idle == l {
			s.idle = nil
			l.Close()
		}
	})
}

// closes the kept link, on macOS while RunMain's loop still runs
func (s *Session) Close() {
	if s == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.idle != nil {
		s.timer.Stop()
		s.idle.Close()
		s.idle = nil
	}
}
