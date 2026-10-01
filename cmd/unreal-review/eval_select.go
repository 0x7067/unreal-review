package main

import (
	"fmt"
	"strings"
)

func selectNamed[T any](cases []T, names, kind string, name func(T) string) ([]T, error) {
	if names == "" {
		return cases, nil
	}
	byName := make(map[string]T, len(cases))
	for _, c := range cases {
		byName[name(c)] = c
	}
	var picked []T
	seen := make(map[string]bool)
	for _, raw := range strings.Split(names, ",") {
		n := strings.TrimSpace(raw)
		c, ok := byName[n]
		if !ok {
			return nil, fmt.Errorf("unknown %s case %q", kind, n)
		}
		if seen[n] {
			return nil, fmt.Errorf("duplicate %s case %q", kind, n)
		}
		seen[n] = true
		picked = append(picked, c)
	}
	return picked, nil
}
