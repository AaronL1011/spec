package urgency

import (
	"math"
	"testing"
	"time"
)

func TestFraction(t *testing.T) {
	tests := []struct {
		name   string
		dwell  time.Duration
		window time.Duration
		want   float64
	}{
		{"cold", 0, 48 * time.Hour, 0},
		{"quarter", 12 * time.Hour, 48 * time.Hour, 0.25},
		{"half", 24 * time.Hour, 48 * time.Hour, 0.5},
		{"full", 48 * time.Hour, 48 * time.Hour, 1},
		{"over clamps to 1", 96 * time.Hour, 48 * time.Hour, 1},
		{"zero window is never stale", 100 * time.Hour, 0, 0},
		{"negative window is never stale", 100 * time.Hour, -time.Hour, 0},
		{"negative dwell is cold", -time.Hour, 48 * time.Hour, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Fraction(tt.dwell, tt.window)
			if math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("Fraction(%v, %v) = %v, want %v", tt.dwell, tt.window, got, tt.want)
			}
		})
	}
}

func TestEaseEndpointsAndMonotonicity(t *testing.T) {
	for _, curve := range []Curve{Linear, EaseIn, EaseInStrong} {
		if got := Ease(0, curve); got != 0 {
			t.Errorf("Ease(0, %v) = %v, want 0", curve, got)
		}
		if got := Ease(1, curve); got != 1 {
			t.Errorf("Ease(1, %v) = %v, want 1", curve, got)
		}
		// Monotonic non-decreasing across the range.
		prev := -1.0
		for i := 0; i <= 100; i++ {
			r := float64(i) / 100
			v := Ease(r, curve)
			if v < prev-1e-9 {
				t.Errorf("Ease not monotonic for %v at r=%v: %v < %v", curve, r, v, prev)
			}
			prev = v
		}
	}
}

func TestEaseOrdering(t *testing.T) {
	// For equal r in (0,1): strong <= ease-in <= linear (eased curves stay cooler).
	for i := 1; i < 100; i++ {
		r := float64(i) / 100
		strong, easeIn, linear := Ease(r, EaseInStrong), Ease(r, EaseIn), Ease(r, Linear)
		if !(strong <= easeIn+1e-9 && easeIn <= linear+1e-9) {
			t.Fatalf("ordering violated at r=%v: strong=%v easeIn=%v linear=%v", r, strong, easeIn, linear)
		}
	}
}

func TestEaseClampsInput(t *testing.T) {
	if got := Ease(-0.5, EaseIn); got != 0 {
		t.Errorf("Ease(-0.5) = %v, want 0", got)
	}
	if got := Ease(2, EaseIn); got != 1 {
		t.Errorf("Ease(2) = %v, want 1", got)
	}
}

func TestParseCurve(t *testing.T) {
	tests := []struct {
		in     string
		want   Curve
		wantOK bool
	}{
		{"", EaseIn, true},
		{"linear", Linear, true},
		{"ease-in", EaseIn, true},
		{"ease-in-strong", EaseInStrong, true},
		{"bogus", EaseIn, false},
		{"Linear", EaseIn, false}, // case-sensitive
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, ok := ParseCurve(tt.in)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("ParseCurve(%q) = (%v, %v), want (%v, %v)", tt.in, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestValueComposition(t *testing.T) {
	// Value == Ease(Fraction(...)).
	dwell, window := 24*time.Hour, 48*time.Hour
	got := Value(dwell, window, EaseIn)
	want := Ease(Fraction(dwell, window), EaseIn)
	if got != want {
		t.Errorf("Value = %v, want %v", got, want)
	}
	// At half-window with ease-in (r²): 0.5² = 0.25.
	if math.Abs(got-0.25) > 1e-9 {
		t.Errorf("Value(half, ease-in) = %v, want 0.25", got)
	}
}
