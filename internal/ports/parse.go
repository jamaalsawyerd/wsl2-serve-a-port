// Package ports parses and formats port specs like "3000, 8000-8010".
package ports

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// MaxPorts caps how many ports one spec may expand to. netsh portproxy has no
// range support, so every port becomes its own rule.
const MaxPorts = 256

// Parse expands a spec of single ports and inclusive ranges, separated by
// commas or whitespace, into a sorted, de-duplicated list.
func Parse(spec string) ([]int, error) {
	fields := strings.FieldsFunc(spec, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})
	if len(fields) == 0 {
		return nil, fmt.Errorf("enter a port like 3000 or a range like 8000-8010")
	}

	seen := make(map[int]bool)
	for _, f := range fields {
		lo, hi, err := parseField(f)
		if err != nil {
			return nil, err
		}
		if hi-lo+1 > MaxPorts {
			return nil, fmt.Errorf("range %s has %d ports; the limit is %d", f, hi-lo+1, MaxPorts)
		}
		for p := lo; p <= hi; p++ {
			seen[p] = true
		}
		if len(seen) > MaxPorts {
			return nil, fmt.Errorf("that's more than %d ports", MaxPorts)
		}
	}

	out := make([]int, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	slices.Sort(out)
	return out, nil
}

func parseField(f string) (lo, hi int, err error) {
	a, b, isRange := strings.Cut(f, "-")
	if lo, err = parsePort(a); err != nil {
		return 0, 0, err
	}
	if !isRange {
		return lo, lo, nil
	}
	if hi, err = parsePort(b); err != nil {
		return 0, 0, err
	}
	if hi < lo {
		return 0, 0, fmt.Errorf("range %s is backwards; try %d-%d", f, hi, lo)
	}
	return lo, hi, nil
}

func parsePort(s string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("%q isn't a port number", s)
	}
	if n < 1 || n > 65535 {
		return 0, fmt.Errorf("port %d is out of range (1-65535)", n)
	}
	return n, nil
}

// Format collapses a list of ports back into a compact spec, for example
// [3000 8000 8001 8002] -> "3000, 8000-8002".
func Format(ports []int) string {
	sorted := slices.Clone(ports)
	slices.Sort(sorted)
	sorted = slices.Compact(sorted)

	var parts []string
	for i := 0; i < len(sorted); {
		j := i
		for j+1 < len(sorted) && sorted[j+1] == sorted[j]+1 {
			j++
		}
		if i == j {
			parts = append(parts, strconv.Itoa(sorted[i]))
		} else {
			parts = append(parts, fmt.Sprintf("%d-%d", sorted[i], sorted[j]))
		}
		i = j + 1
	}
	return strings.Join(parts, ", ")
}
