package printer

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/Northernside/nelko-pm230-go/tspl"
)

// an opened /dev/cu.* proves nothing on macOS, the status byte coming back does
func (p *Printer) Probe(ctx context.Context) (string, Status, error) {
	_, dev, done, s, err := p.connect(ctx, "checking the printer")
	if err != nil {
		return dev, s, err
	}
	done(true)
	return dev, s, nil
}

// https://github.com/an21p/nelko-pm230-driver/blob/main/research/FINDINGS.md#transport-behaviour
type Info struct {
	Device  string `json:"device"`
	Config  []byte `json:"config"`  // after "CONFIG ", only the dpi is decoded
	DPI     int    `json:"dpi"`     // config bytes 0-1 big endian, 0x00cb = 203
	Battery int    `json:"battery"` // percent, the first byte after "BATTERY ", -1 = no answer
	Status  Status `json:"status"`
}

func (p *Printer) Info(ctx context.Context) (info Info, err error) {
	link, dev, done, s, err := p.connect(ctx, "asking for its config")
	info = Info{Device: dev, Status: s}
	if err != nil {
		return info, err
	}
	defer func() { done(err == nil) }()

	if info.Config, err = ask(link, tspl.QueryConfig, "CONFIG "); err != nil {
		return info, fmt.Errorf("asking %s for its config: %w", dev, ctxErr(ctx, err))
	}

	if len(info.Config) >= 2 {
		info.DPI = int(info.Config[0])<<8 | int(info.Config[1])
	}

	battery, err := ask(link, tspl.QueryBattery, "BATTERY ")
	if err != nil {
		return info, fmt.Errorf("asking %s for its battery: %w", dev, ctxErr(ctx, err))
	}

	info.Battery = -1
	if len(battery) > 0 {
		info.Battery = int(battery[0])
	}

	return info, nil
}

// up to \r\n or 2s of silence, returns what follows prefix
func ask(link Link, q []byte, prefix string) ([]byte, error) {
	if _, err := link.Write(q); err != nil {
		return nil, err
	}

	var reply []byte
	buf := make([]byte, 256)
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline) && len(reply) < 1024; {
		n, err := link.ReadTimeout(buf, 200*time.Millisecond)
		if err != nil {
			return nil, err
		}

		reply = append(reply, buf[:n]...)
		if i := bytes.Index(reply, []byte("\r\n")); i >= 0 {
			reply = reply[:i]
			break
		}
	}

	if _, after, ok := bytes.Cut(reply, []byte(prefix)); ok {
		return after, nil
	}

	return reply, nil
}
