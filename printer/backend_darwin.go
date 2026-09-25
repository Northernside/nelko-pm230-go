package printer

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/Northernside/nelko-pm230-go/internal/enum"
)

func Default() Backend { return darwin{} }

// /dev/cu.PM230 opens whether the RFCOMM channel is up or not and drops what is written meanwhile
// (2026-09-24) -> the MAC over IOBluetooth comes first, /dev paths stay usable with --port
type darwin struct{}

func (darwin) Candidates(context.Context) ([]Candidate, error) {
	var found []Candidate
	if haveIOBluetooth {
		paired, err := btPaired()
		if err != nil {
			return nil, err
		}
		found = rfcommCandidates(paired)
	}

	paths, err := filepath.Glob("/dev/cu.*")
	if err != nil {
		return nil, err
	}

	for _, c := range macCandidates(paths) {
		if haveIOBluetooth {
			c.Confidence = ConfidenceMaybe
		}

		found = append(found, c)
	}

	return rank(found), nil
}

func (darwin) NotFound(ctx context.Context) string {
	var names []string
	if paired, err := btPaired(); err == nil {
		names = enum.Collect(paired, func(d pairedDevice) string { return d.Name })
	} else if out, ok := run(ctx, 30*time.Second, "system_profiler", "SPBluetoothDataType", "-json"); ok {
		names, _ = parsePairedNames(out)
	}

	return macNotFound(names)
}

func (darwin) Open(ctx context.Context, device string) (Link, error) {
	if strings.HasPrefix(device, "/dev/") {
		return openSerial(device)
	}

	link, err := blocking(ctx, func() (Link, error) { return dialIOBluetooth(device) })
	if err != nil && link != nil {
		link.Close()
	}

	return link, err
}

func (darwin) Pair(ctx context.Context) (Candidate, error) {
	seen, err := blocking(ctx, func() ([]pairedDevice, error) { return btInquiry(10) })
	if err != nil {
		return Candidate{}, err
	}

	return pairFrom(seen, func(mac string) error {
		_, err := blocking(ctx, func() (struct{}, error) { return struct{}{}, btPair(mac) })
		return err
	})
}

// IOBluetooth calls cannot be interrupted, ctx only stops the waiting
func blocking[T any](ctx context.Context, f func() (T, error)) (T, error) {
	type result struct {
		v   T
		err error
	}

	done := make(chan result, 1)
	go func() {
		v, err := f()
		done <- result{v, err}
	}()

	select {
	case r := <-done:
		return r.v, r.err
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	}
}
