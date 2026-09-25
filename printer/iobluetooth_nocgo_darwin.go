//go:build darwin && !cgo

package printer

import "errors"

const haveIOBluetooth = false

var errNoCgo = errors.New("built without cgo, bluetooth only goes through /dev/cu.* (rebuild with CGO_ENABLED=1)")

func btPaired() ([]pairedDevice, error)    { return nil, errNoCgo }
func dialIOBluetooth(string) (Link, error) { return nil, errNoCgo }

func btInquiry(int) ([]pairedDevice, error) { return nil, errNoCgo }
func btPair(string) error                   { return errNoCgo }
