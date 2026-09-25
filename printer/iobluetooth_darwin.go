//go:build darwin && cgo

package printer

/*
#cgo CFLAGS: -fobjc-arc -Wall
#cgo LDFLAGS: -framework Foundation -framework IOBluetooth
#include <stdlib.h>
#include "iobluetooth_darwin.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unsafe"
)

const haveIOBluetooth = true

var errNoMainLoop = errors.New("IOBluetooth needs the main thread, run the program through printer.RunMain")

// IOBluetooth delivers callbacks on the main run loop only, RunMain parks the main thread there while f runs
// main() must hold the main thread (runtime.LockOSThread in init)
func RunMain(f func() int) int {
	done := make(chan int, 1)
	go func() {
		defer C.nk_stop_main_loop()
		done <- f()
	}()
	C.nk_main_loop() // returns at once off the main thread, calls then fail with errNoMainLoop

	return <-done
}

func btPaired() ([]pairedDevice, error) {
	var devs *C.nk_device
	n := int(C.nk_paired(&devs))
	if n < 0 {
		C.nk_free_devices(devs, 0)
		return nil, errNoMainLoop
	}
	defer C.nk_free_devices(devs, C.int(n))

	out := goDevices(devs, n)
	return out, nil
}

var errNotPermitted = errors.New("macOS does not let this program use Bluetooth. Allow your terminal app in System Settings > Privacy & Security > Bluetooth, then start it again")

const (
	ioReturnNotPermitted = 0xe00002e2
	ioReturnTimeout      = 0xe00002d6
)

func ioReturnError(what string, r C.int) error {
	switch uint32(r) {
	case 0:
		return nil
	case ioReturnNotPermitted:
		return errNotPermitted
	case ioReturnTimeout:
		return errors.New(what + " timed out")
	}
	return fmt.Errorf("%s failed (IOReturn 0x%08x)", what, uint32(r))
}

func btInquiry(seconds int) ([]pairedDevice, error) {
	var devs *C.nk_device
	var r C.int
	stopAt := C.CString(Name)
	defer C.free(unsafe.Pointer(stopAt))
	n := int(C.nk_inquiry(C.int(seconds), stopAt, &devs, &r))
	defer C.nk_free_devices(devs, C.int(n))
	if r == -2 {
		return nil, errNoMainLoop
	}

	if n == 0 && r == 1 {
		return nil, errNotPermitted
	}

	if n == 0 {
		if err := ioReturnError("the bluetooth inquiry", r); err != nil {
			return nil, err
		}
	}

	out := goDevices(devs, n)
	return out, nil
}

func btPair(mac string) error {
	addr := C.CString(btAddr(mac))
	defer C.free(unsafe.Pointer(addr))
	var cerr *C.char
	if C.nk_pair(addr, 60, &cerr) != 0 {
		return cError(cerr)
	}

	return nil
}

func goDevices(devs *C.nk_device, n int) []pairedDevice {
	out := make([]pairedDevice, n)
	for i, d := range unsafe.Slice(devs, n) {
		out[i] = pairedDevice{MAC: macFromIOBluetooth(C.GoString(d.addr)), Name: C.GoString(d.name)}
	}

	return out
}

func btAddr(mac string) string { return strings.ToLower(strings.ReplaceAll(mac, ":", "-")) }

func macFromIOBluetooth(s string) string {
	return strings.ToUpper(strings.ReplaceAll(s, "-", ":"))
}

type btLink struct {
	h    C.int
	once sync.Once
}

func dialIOBluetooth(mac string) (Link, error) {
	addr := C.CString(btAddr(mac))
	defer C.free(unsafe.Pointer(addr))
	var cerr *C.char
	h := C.nk_open(addr, &cerr)
	if h == 0 {
		err := cError(cerr)
		if strings.Contains(err.Error(), fmt.Sprintf("0x%08x", ioReturnNotPermitted)) {
			return nil, errNotPermitted
		}

		return nil, err
	}

	return &btLink{h: h}, nil
}

func cError(e *C.char) error {
	if e == nil {
		return errors.New("iobluetooth failed without saying why")
	}
	defer C.free(unsafe.Pointer(e))
	return errors.New(C.GoString(e))
}

func (l *btLink) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	var cerr *C.char
	if C.nk_write(l.h, unsafe.Pointer(&p[0]), C.size_t(len(p)), &cerr) != 0 {
		return 0, cError(cerr)
	}

	return len(p), nil
}

func (l *btLink) String() string {
	return fmt.Sprintf("iobluetooth rfcomm channel %d", int(C.nk_channel(l.h)))
}

func (l *btLink) ReadTimeout(p []byte, d time.Duration) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	n := int(C.nk_read(l.h, unsafe.Pointer(&p[0]), C.size_t(len(p)), C.int(d.Milliseconds())))
	if n < 0 {
		return 0, io.EOF
	}

	return n, nil
}

func (l *btLink) Alive() bool { return C.nk_alive(l.h) != 0 }
func (l *btLink) Flush()      { C.nk_flush(l.h) }

func (l *btLink) Close() error {
	l.once.Do(func() { C.nk_close(l.h) })
	return nil
}
