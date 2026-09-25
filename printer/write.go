//go:build darwin

package printer

import (
	"errors"
	"io"
	"syscall"
)

// write(2) on a tty returns short when a signal lands mid transfer, and Go preempts with SIGURG
func writeFull(write func([]byte) (int, error), p []byte) (int, error) {
	n, stalls := 0, 0
	for n < len(p) {
		m, err := write(p[n:])
		n += max(m, 0)
		switch {
		case errors.Is(err, syscall.EINTR), errors.Is(err, syscall.EAGAIN):
			continue
		case err != nil:
			return n, err
		case m > 0:
			stalls = 0
		default:
			if stalls++; stalls > 100 {
				return n, io.ErrShortWrite
			}
		}
	}

	return n, nil
}
