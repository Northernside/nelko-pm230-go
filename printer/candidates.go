//go:build darwin

package printer

import (
	"cmp"
	"errors"
	"slices"
	"strings"

	"github.com/Northernside/nelko-pm230-go/internal/enum"
)

// firmwares may pair with a suffix, PM230-B110
func isPrinterName(name string) bool { return strings.HasPrefix(name, Name) }

type pairedDevice struct{ MAC, Name string }

func (d pairedDevice) label() string { return cmp.Or(d.Name, d.MAC) }

func rfcommCandidate(d pairedDevice) Candidate {
	c := Candidate{Device: d.MAC, Label: d.Name + " (" + d.MAC + ")", Kind: "rfcomm", Confidence: ConfidenceMaybe}
	if isPrinterName(d.Name) {
		c.Confidence = ConfidencePrinter
	}

	return c
}

func rfcommCandidates(paired []pairedDevice) []Candidate {
	found := make([]Candidate, len(paired))
	for i, d := range paired {
		found[i] = rfcommCandidate(d)
	}

	return rank(found)
}

// printers first, the rest keeps its order
func rank(cs []Candidate) []Candidate {
	slices.SortStableFunc(cs, func(a, b Candidate) int {
		switch {
		case a.IsPrinter() == b.IsPrinter():
			return 0
		case a.IsPrinter():
			return -1
		}

		return 1
	})

	return cs
}

// Candidate is set when a printer was in range, even if pair failed
func pairFrom(seen []pairedDevice, pair func(mac string) error) (Candidate, error) {
	if i := slices.IndexFunc(seen, func(d pairedDevice) bool { return isPrinterName(d.Name) }); i >= 0 {
		return rfcommCandidate(seen[i]), pair(seen[i].MAC)
	}

	if len(seen) == 0 {
		return Candidate{}, errors.New("no bluetooth devices in range, is the printer switched on and not connected to a phone?")
	}

	names := enum.Collect(seen, pairedDevice.label)
	return Candidate{}, errors.New("no " + Name + " in range, saw " + strings.Join(names, ", ") + ". Is it switched on and not connected to a phone?")
}
