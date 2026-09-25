package printer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Northernside/nelko-pm230-go/tspl"
)

// the <ESC>!? reply, bits per the TSC TSPL/TSPL2 Programming Manual
// only 0x00 is confirmed on a PM230
type Status byte

const (
	StatusHeadOpen Status = 1 << iota
	StatusPaperJam
	StatusNoPaper
	StatusNoRibbon
	StatusPaused
	StatusPrinting
	StatusCoverOpen
	StatusOther
)

var statusNames = [...]string{"head open", "paper jam", "out of paper", "out of ribbon", "paused", "printing", "cover open", "other error"}

func (s Status) String() string {
	if s == 0 {
		return "ready"
	}
	var parts []string
	for i, name := range statusNames {
		if s&(1<<i) != 0 {
			parts = append(parts, name)
		}
	}
	return strings.Join(parts, ", ") + fmt.Sprintf(" (0x%02x)", byte(s))
}

func (s Status) Fault() bool { return s&^StatusPrinting != 0 }

const readyTimeout = 10 * time.Second

var ErrNoAnswer = errors.New("the printer did not answer the status query, whatever we send would be lost")

type StatusError struct{ Status Status }

func (e *StatusError) Error() string { return "the printer reports " + e.Status.String() }

// bytes sent before the RFCOMM channel is up vanish without an error (/dev/cu.PM230, 2026-09-24)
// -> nothing goes out until <ESC>!? got its status byte back
func (p *Printer) waitReady(ctx context.Context, link Link, dev string) (Status, error) {
	start := time.Now()
	var busySince time.Time
	for attempt := 1; ; attempt++ {
		s, answered, err := queryStatus(ctx, link)
		switch {
		case err != nil:
			return 0, err
		case !answered && time.Since(start) > readyTimeout:
			return 0, ErrNoAnswer
		case !answered:
			p.log("no answer to the status query yet", "device", dev, "attempt", attempt)
			continue
		case s.Fault():
			return s, &StatusError{s}
		case s&StatusPrinting == 0:
			p.log("printer ready", "device", dev, "after", time.Since(start).Round(time.Millisecond))
			return s, nil
		}

		if busySince.IsZero() {
			busySince = time.Now()
		}
		if time.Since(busySince) > time.Minute {
			return s, &StatusError{s}
		}
		if err := sleep(ctx, 500*time.Millisecond); err != nil {
			return 0, err
		}
	}
}

// one <ESC>!?, a second for the reply
func queryStatus(ctx context.Context, link Link) (Status, bool, error) {
	if err := ctx.Err(); err != nil {
		return 0, false, err
	}
	if _, err := link.Write(tspl.QueryStatus); err != nil {
		return 0, false, err
	}
	buf := make([]byte, 64)
	for until := time.Now().Add(time.Second); time.Now().Before(until); {
		n, err := link.ReadTimeout(buf, 100*time.Millisecond)
		if err != nil {
			return 0, false, err
		}
		if n > 0 {
			return Status(buf[n-1]), true, nil // older bytes may answer an earlier query
		}
	}
	return 0, false, nil
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// holds the Session until done, done(true) hands the link back to it
// a kept link that went silent gets one fresh open, a fresh one that stays silent is an error
func (p *Printer) connect(ctx context.Context, who string) (Link, string, func(ok bool), Status, error) {
	release, err := p.Session.acquire(ctx, who)
	if err != nil {
		return nil, "", nil, 0, err
	}
	dev, err := p.Resolve(ctx)
	if err != nil {
		release()
		return nil, "", nil, 0, err
	}

	link := p.Session.take(dev)
	reused := link != nil
	for {
		if link == nil {
			if link, err = p.open(ctx, dev); err != nil {
				release()
				return nil, dev, nil, 0, err
			}
		} else {
			p.log("link reused", "device", dev)
		}
		stop := context.AfterFunc(ctx, func() { link.Close() }) // unblocks a write stuck on a dead link
		s, err := p.waitReady(ctx, link, dev)
		if err == nil {
			return link, dev, func(ok bool) {
				if stop() && ok {
					p.Session.keep(dev, link)
				} else {
					link.Close()
				}
				release()
			}, s, nil
		}
		stop()
		link.Close()
		if !reused || !errors.Is(err, ErrNoAnswer) {
			release()
			return nil, dev, nil, s, fmt.Errorf("%s: %w", dev, err)
		}
		p.log("kept link went silent, opening a fresh one", "device", dev)
		link, reused = nil, false
	}
}
