//go:build !darwin

package printer

import (
	"context"
	"errors"
	"runtime"
)

func Default() Backend { return unsupported{} }

type unsupported struct{}

var errUnsupported = errors.New("no discovery backend for " + runtime.GOOS)

func (unsupported) Candidates(context.Context) ([]Candidate, error) { return nil, errUnsupported }
func (unsupported) NotFound(context.Context) string                 { return errUnsupported.Error() }
func (unsupported) Open(context.Context, string) (Link, error)      { return nil, errUnsupported }
