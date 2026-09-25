package enum

import (
	"slices"
	"strconv"
	"strings"
)

// Names[i] names value i
type Names[T ~uint8] []string

func (n Names[T]) String(v T) string {
	if int(v) < len(n) {
		return n[v]
	}
	return strconv.Itoa(int(v))
}

// case-insensitive, "" yields def
func (n Names[T]) Parse(s string, def T) (T, bool) {
	if s == "" {
		return def, true
	}
	i := slices.IndexFunc(n, func(name string) bool { return strings.EqualFold(name, s) })
	return T(max(i, 0)), i >= 0
}

func (n Names[T]) List() []string { return slices.Clone(n) }

// the name of every row of a table, in order
func Collect[S any](table []S, name func(S) string) []string {
	out := make([]string, len(table))
	for i, row := range table {
		out[i] = name(row)
	}
	return out
}
