//go:build darwin

package printer

import (
	"context"
	"errors"
	"os/exec"
	"sync"
	"time"

	"go.bug.st/serial"
)

type serialLink struct {
	port serial.Port
	once sync.Once
}

func openSerial(device string) (Link, error) {
	p, err := serial.Open(device, &serial.Mode{
		BaudRate:          115200,
		InitialStatusBits: &serial.ModemOutputBits{DTR: true, RTS: true}, // as pyserial, the python driver printed like this
	})
	if err != nil {
		return nil, err
	}

	return &serialLink{port: p}, nil
}

func (l *serialLink) Write(p []byte) (int, error) { return writeFull(l.port.Write, p) }

func (l *serialLink) ReadTimeout(p []byte, d time.Duration) (int, error) {
	if err := l.port.SetReadTimeout(d); err != nil {
		return 0, err
	}

	return l.port.Read(p)
}

func (l *serialLink) Close() error {
	var err error
	l.once.Do(func() {
		err = errors.Join(l.port.Drain(), l.port.Close()) // Drain = tcdrain / FlushFileBuffers
	})

	return err
}

func run(ctx context.Context, timeout time.Duration, name string, args ...string) ([]byte, bool) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	return out, err == nil
}
