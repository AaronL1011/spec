package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Day and week durations are not understood by time.ParseDuration, which tops
// out at hours. They are the natural unit for stale windows, so ParseDuration
// below adds them.
const (
	day  = 24 * time.Hour
	week = 7 * day
)

var extendedUnits = map[string]time.Duration{
	"d": day,
	"w": week,
}

// ParseDuration parses a duration string with support for day ("d") and week
// ("w") units in addition to Go's standard units (ns, us, ms, s, m, h).
// Tokens may be combined, e.g. "1w2d", "5d", "30m", "48h". A bare "0" yields
// zero. Whitespace is trimmed and parsing is case-insensitive.
func ParseDuration(s string) (time.Duration, error) {
	orig := s
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}
	if s == "0" {
		return 0, nil
	}

	var total time.Duration
	i := 0
	for i < len(s) {
		// Read the numeric part (digits and decimal point).
		start := i
		for i < len(s) && (s[i] == '.' || (s[i] >= '0' && s[i] <= '9')) {
			i++
		}
		if i == start {
			return 0, fmt.Errorf("invalid duration %q", orig)
		}
		numStr := s[start:i]

		// Read the unit part (ASCII letters).
		unitStart := i
		for i < len(s) && s[i] >= 'a' && s[i] <= 'z' {
			i++
		}
		unit := s[unitStart:i]
		if unit == "" {
			return 0, fmt.Errorf("invalid duration %q: missing unit after %q", orig, numStr)
		}

		if mult, ok := extendedUnits[unit]; ok {
			val, err := strconv.ParseFloat(numStr, 64)
			if err != nil {
				return 0, fmt.Errorf("invalid duration %q: %w", orig, err)
			}
			total += time.Duration(val * float64(mult))
			continue
		}

		// Delegate standard units to the stdlib parser one token at a time so
		// error messages stay specific and units like "ms" are handled.
		d, err := time.ParseDuration(numStr + unit)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q: unknown unit %q (use m, h, d, w)", orig, unit)
		}
		total += d
	}

	return total, nil
}

// parseStaleWindow interprets a stage's stale_after value. ok is false — the
// stage is never stale — when the value is empty, "none", or "0". A value that
// fails to parse also degrades to "never stale" so a malformed config never
// breaks rendering; lint surfaces the error separately.
func parseStaleWindow(s string) (window time.Duration, ok bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || s == "none" || s == "0" {
		return 0, false
	}
	d, err := ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, false
	}
	return d, true
}

// validateStaleAfter reports whether a stale_after value is acceptable. Empty,
// "none", and "0" are valid (they disable staleness); anything else must parse
// to a positive duration.
func validateStaleAfter(s string) error {
	t := strings.ToLower(strings.TrimSpace(s))
	if t == "" || t == "none" || t == "0" {
		return nil
	}
	d, err := ParseDuration(t)
	if err != nil {
		return err
	}
	if d <= 0 {
		return fmt.Errorf("must be a positive duration")
	}
	return nil
}
