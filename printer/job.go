package printer

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"time"

	"github.com/Northernside/nelko-pm230-go/tspl"
)

// sent after every label that came out whole, missing on the one cut in half
// (seen on a PM230, the reply grammar is undocumented)
const printDone = 'c'

// closing the link before the label is out truncates it
// https://github.com/an21p/nelko-pm230-driver/blob/main/research/FINDINGS.md#transport-behaviour
func (p *Printer) Send(ctx context.Context, job []byte) (dev string, err error) {
	link, dev, done, _, err := p.connect(ctx, "printing")
	if err != nil {
		return dev, err
	}
	defer func() { done(err == nil) }()

	start := time.Now()
	if n, err := link.Write(job); err != nil {
		return dev, fmt.Errorf("writing to %s after %d of %d bytes: %w", dev, n, len(job), ctxErr(ctx, err))
	}

	p.log("job written", "device", dev, "bytes", len(job), "took", time.Since(start).Round(time.Millisecond))
	p.settle(ctx, link, len(job))
	return dev, ctx.Err()
}

// a mostly black 47mm label outlasted the 5s that text labels needed (2026-09-25)
func printBudget(settle time.Duration, jobBytes int) time.Duration {
	mm := float64(jobBytes) / (tspl.PrintWidthDots / 8 * tspl.DotsPerMM)
	return min(settle+time.Duration(mm*float64(400*time.Millisecond)), 90*time.Second)
}

// holds the link until printDone, or the budget without it, any other reply extends the wait
func (p *Printer) settle(ctx context.Context, link Link, jobBytes int) {
	d := cmp.Or(p.Settle, 5*time.Second)
	buf := make([]byte, 256)
	var heard []byte
	done := false
	start := time.Now()
	defer func() {
		p.log("link held open", "took", time.Since(start).Round(time.Millisecond), "finished", done, "printer said", fmt.Sprintf("%q", heard))
	}()

	deadline := start.Add(printBudget(d, jobBytes))
	for now := start; now.Before(deadline) && ctx.Err() == nil; now = time.Now() {
		n, err := link.ReadTimeout(buf, 100*time.Millisecond)
		if err != nil {
			return
		}
		if n == 0 {
			continue
		}

		if len(heard) < 1024 {
			heard = append(heard, buf[:n]...)
		}

		switch {
		case done:
		case bytes.IndexByte(buf[:n], printDone) >= 0:
			done = true
			deadline = time.Now().Add(300 * time.Millisecond) // the \x00 after it
		case time.Now().Add(d).After(deadline):
			deadline = time.Now().Add(d)
		}
	}
}
