package printer

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"time"
)

const (
	Name    = "PM230" // bluetooth name, some firmwares add a suffix
	PortEnv = "NELKO_PORT"
)

const (
	ConfidencePrinter = "printer"
	ConfidenceMaybe   = "maybe"
)

type Candidate struct {
	Device     string `json:"device"`
	Label      string `json:"label"`
	Kind       string `json:"kind"` // serial or rfcomm
	Confidence string `json:"confidence"`
}

type Backend interface {
	Candidates(ctx context.Context) ([]Candidate, error)
	NotFound(ctx context.Context) string
	Open(ctx context.Context, device string) (Link, error)
}

// Pair returns the printer it found even when pairing failed, connecting may work without it
type Pairer interface {
	Pair(ctx context.Context) (Candidate, error)
}

// Write returns once every byte is handed on, Close once they are sent
type Link interface {
	Write(p []byte) (int, error)
	ReadTimeout(p []byte, d time.Duration) (int, error) // 0, nil on timeout
	Close() error                                       // safe to call twice
}

type NoPrinterError struct{ Hint string }

func (e *NoPrinterError) Error() string { return "no printer found: " + e.Hint }

type Printer struct {
	Backend Backend       // nil = Default()
	Device  string        // "" = $NELKO_PORT, then discovery
	Settle  time.Duration // 0 = 5s
	Session *Session      // nil = every call opens and closes its own link
	Log     *slog.Logger
}

func (p *Printer) log(msg string, args ...any) {
	if p.Log != nil {
		p.Log.Info(msg, args...)
	}
}

func (p *Printer) backend() Backend {
	if p.Backend != nil {
		return p.Backend
	}

	return Default()
}

func (c Candidate) IsPrinter() bool { return c.Confidence == ConfidencePrinter }

// Device, else $NELKO_PORT, "" = discovery decides
func (p *Printer) Fixed() string {
	return cmp.Or(p.Device, strings.TrimSpace(os.Getenv(PortEnv)))
}

// message is the backend's hint when no printer is among found
func (p *Printer) Scan(ctx context.Context) (found []Candidate, message string, err error) {
	b := p.backend()
	if found, err = b.Candidates(ctx); err != nil {
		return nil, "", err
	}

	if slices.ContainsFunc(found, Candidate.IsPrinter) {
		return found, "", nil
	}

	return found, b.NotFound(ctx), nil
}

// Device and $NELKO_PORT skip discovery so they work where a backend is wrong or missing
func (p *Printer) Resolve(ctx context.Context) (string, error) {
	if dev := p.Fixed(); dev != "" {
		return dev, nil
	}

	found, message, err := p.Scan(ctx)
	if err != nil {
		return "", &NoPrinterError{Hint: err.Error()}
	}

	if i := slices.IndexFunc(found, Candidate.IsPrinter); i >= 0 {
		return found[i].Device, nil
	}

	if _, ok := p.backend().(Pairer); !ok {
		return "", &NoPrinterError{Hint: message}
	}

	c, err := p.pair(ctx)
	if c.Device != "" {
		return c.Device, nil
	}
	if err != nil {
		message = err.Error()
	}

	return "", &NoPrinterError{Hint: message}
}

func (p *Printer) Pair(ctx context.Context) (Candidate, error) {
	release, err := p.Session.acquire(ctx, "pairing")
	if err != nil {
		return Candidate{}, err
	}
	defer release()

	return p.pair(ctx)
}

func (p *Printer) pair(ctx context.Context) (Candidate, error) {
	pr, ok := p.backend().(Pairer)
	if !ok {
		return Candidate{}, errors.New("this platform cannot pair by itself, pair the printer in the system bluetooth settings")
	}

	start := time.Now()
	p.log("looking for a printer in range")
	c, err := pr.Pair(ctx)
	p.log("pairing done", "device", c.Device, "err", err, "took", time.Since(start).Round(time.Millisecond))
	return c, err
}

func (p *Printer) open(ctx context.Context, dev string) (Link, error) {
	start := time.Now()
	link, err := p.backend().Open(ctx, dev)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", dev, err)
	}

	via := "serial"
	if s, ok := link.(fmt.Stringer); ok {
		via = s.String()
	}

	p.log("link open", "device", dev, "via", via, "took", time.Since(start).Round(time.Millisecond))
	return link, nil
}

func ctxErr(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return errors.Join(ctx.Err(), err)
	}

	return err
}
