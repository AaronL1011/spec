// Package urgency computes how "stale" a task is from the time it has dwelt in
// its current pipeline stage, expressed as a 0..1 intensity for colour ramps.
//
// It is intentionally pure: it depends only on the standard library so it can
// be table-tested in isolation and reused by both the dashboard and the
// pipeline screen. The mapping from a stage's configured window to a duration,
// and from a config string to a Curve, lives in internal/config so this
// package never imports config (avoiding an import cycle).
package urgency

import "time"

// Curve selects how the raw dwell fraction is shaped into a display intensity.
type Curve int

const (
	// Linear maps the fraction unchanged (f = r).
	Linear Curve = iota
	// EaseIn keeps the value cool early and intensifies late (f = r²). Default.
	EaseIn
	// EaseInStrong is a more aggressive ease-in (f = r³).
	EaseInStrong
)

// Easing names accepted in team config (dashboard.urgency.easing).
const (
	EasingLinear       = "linear"
	EasingEaseIn       = "ease-in"
	EasingEaseInStrong = "ease-in-strong"
)

// EasingNames lists the recognised easing names in declaration order. Used for
// lint validation and did-you-mean suggestions.
func EasingNames() []string {
	return []string{EasingLinear, EasingEaseIn, EasingEaseInStrong}
}

// ParseCurve resolves an easing name to a Curve. An empty string yields the
// default (EaseIn). ok is false for an unrecognised name, in which case the
// returned Curve is the default.
func ParseCurve(name string) (curve Curve, ok bool) {
	switch name {
	case "":
		return EaseIn, true
	case EasingLinear:
		return Linear, true
	case EasingEaseIn:
		return EaseIn, true
	case EasingEaseInStrong:
		return EaseInStrong, true
	default:
		return EaseIn, false
	}
}

// Fraction returns dwell/window clamped to [0,1]. It returns 0 when window is
// non-positive — a stage with no stale window is never stale.
func Fraction(dwell, window time.Duration) float64 {
	if window <= 0 || dwell <= 0 {
		return 0
	}
	return clamp01(float64(dwell) / float64(window))
}

// Ease shapes a raw fraction r into a display intensity using curve. All curves
// satisfy Ease(0)=0 and Ease(1)=1 and are monotonic non-decreasing in r; the
// eased curves are convex (gentle early, sharp late). For equal r the ordering
// is EaseInStrong ≤ EaseIn ≤ Linear.
func Ease(r float64, curve Curve) float64 {
	r = clamp01(r)
	switch curve {
	case Linear:
		return r
	case EaseInStrong:
		return r * r * r
	case EaseIn:
		return r * r
	default:
		return r * r
	}
}

// Value composes Ease(Fraction(dwell, window), curve) — the eased intensity for
// a task that has dwelt for dwell against a stale window.
func Value(dwell, window time.Duration, curve Curve) float64 {
	return Ease(Fraction(dwell, window), curve)
}

func clamp01(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	default:
		return v
	}
}
