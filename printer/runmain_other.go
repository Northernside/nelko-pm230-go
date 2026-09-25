//go:build !darwin || !cgo

package printer

// only macOS with cgo needs the main thread
func RunMain(f func() int) int { return f() }
