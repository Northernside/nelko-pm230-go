package printer

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
)

// system_profiler SPBluetoothDataType -json, names are the keys under the device_* sections
func parsePairedNames(profiler []byte) ([]string, error) {
	var doc struct {
		Sections []map[string]json.RawMessage `json:"SPBluetoothDataType"`
	}

	if err := json.Unmarshal(profiler, &doc); err != nil {
		return nil, err
	}

	var names []string
	for _, sec := range doc.Sections {
		for key, raw := range sec {
			if !strings.HasPrefix(key, "device_") {
				continue
			}
			var list []map[string]json.RawMessage
			if json.Unmarshal(raw, &list) != nil {
				var one map[string]json.RawMessage
				if json.Unmarshal(raw, &one) != nil {
					continue
				}
				list = append(list, one)
			}
			for _, entry := range list {
				for name := range entry {
					names = append(names, name)
				}
			}
		}
	}
	slices.Sort(names)
	return names, nil
}

// cu.* because opening tty.* waits for carrier detect, which SPP never raises
// paths sorted
func matchCallout(paths []string) string {
	if i := slices.IndexFunc(paths, func(p string) bool { return strings.HasPrefix(p, "/dev/cu."+Name) }); i >= 0 {
		return paths[i]
	}
	return ""
}

func macCandidates(paths []string) []Candidate {
	paths = slices.Sorted(slices.Values(paths))
	printer := matchCallout(paths)
	var found []Candidate
	for _, p := range paths {
		c := Candidate{Device: p, Label: p, Kind: "serial", Confidence: ConfidenceMaybe}
		if p == printer {
			c.Label, c.Confidence = Name+" ("+p+")", ConfidencePrinter
		}

		found = append(found, c)
	}

	return rank(found)
}

func macNotFound(paired []string) string {
	if !slices.ContainsFunc(paired, isPrinterName) {
		return "no paired Bluetooth device named " + strconv.Quote(Name) + ". Run nelko pair, or pair it in System Settings > Bluetooth"
	}

	return Name + " is paired but exposes no serial port, there is no /dev/cu." + Name + "* device. " +
		"macOS only creates one for a device advertising the SPP service. Pass --port if you know the device path"
}
