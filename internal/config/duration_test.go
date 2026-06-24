package config

import (
	"testing"
	"time"

	"github.com/aaronl1011/spec/internal/urgency"
)

func TestParseDuration(t *testing.T) {
	tests := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"30m", 30 * time.Minute, false},
		{"48h", 48 * time.Hour, false},
		{"5d", 120 * time.Hour, false},
		{"2w", 14 * 24 * time.Hour, false},
		{"1w2d", 9 * 24 * time.Hour, false},
		{"0", 0, false},
		{"  5d  ", 120 * time.Hour, false},
		{"5D", 120 * time.Hour, false}, // case-insensitive
		{"90s", 90 * time.Second, false},
		{"", 0, true},
		{"abc", 0, true},
		{"5", 0, true},  // missing unit
		{"5y", 0, true}, // unknown unit
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseDuration(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseDuration(%q) = %v, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseDuration(%q) unexpected error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("ParseDuration(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseStaleWindow(t *testing.T) {
	tests := []struct {
		in     string
		want   time.Duration
		wantOK bool
	}{
		{"5d", 120 * time.Hour, true},
		{"48h", 48 * time.Hour, true},
		{"", 0, false},
		{"none", 0, false},
		{"NONE", 0, false},
		{"0", 0, false},
		{"garbage", 0, false}, // unparseable degrades to "never stale"
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, ok := parseStaleWindow(tt.in)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("parseStaleWindow(%q) = (%v, %v), want (%v, %v)", tt.in, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestValidateStaleAfter(t *testing.T) {
	valid := []string{"", "none", "0", "30m", "48h", "5d", "2w"}
	for _, v := range valid {
		if err := validateStaleAfter(v); err != nil {
			t.Errorf("validateStaleAfter(%q) = %v, want nil", v, err)
		}
	}
	invalid := []string{"abc", "5", "5y", "-3h"}
	for _, v := range invalid {
		if err := validateStaleAfter(v); err == nil {
			t.Errorf("validateStaleAfter(%q) = nil, want error", v)
		}
	}
}

func TestStageConfigStaleWindow(t *testing.T) {
	got, ok := StageConfig{StaleAfter: "5d"}.StaleWindow()
	if !ok || got != 120*time.Hour {
		t.Errorf("StaleWindow(5d) = (%v, %v), want (120h, true)", got, ok)
	}
	if _, ok := (StageConfig{}).StaleWindow(); ok {
		t.Error("StaleWindow() with no stale_after should be (_, false)")
	}
	if _, ok := (StageConfig{StaleAfter: "none"}).StaleWindow(); ok {
		t.Error("StaleWindow(none) should be (_, false)")
	}
}

func TestReviewWindow(t *testing.T) {
	got, ok := DashboardConfig{Review: ReviewConfig{StaleAfter: "2d"}}.ReviewWindow()
	if !ok || got != 48*time.Hour {
		t.Errorf("ReviewWindow(2d) = (%v, %v), want (48h, true)", got, ok)
	}
	if _, ok := (DashboardConfig{}).ReviewWindow(); ok {
		t.Error("ReviewWindow() with no review.stale_after should be (_, false)")
	}
	if _, ok := (DashboardConfig{Review: ReviewConfig{StaleAfter: "none"}}).ReviewWindow(); ok {
		t.Error("ReviewWindow(none) should be (_, false)")
	}
}

func TestEasingCurveDefault(t *testing.T) {
	// Unset easing resolves to ease-in; an explicit value is honoured.
	if got := (DashboardConfig{}).EasingCurve(); got != urgency.EaseIn {
		t.Errorf("default EasingCurve() = %v, want ease-in", got)
	}
	cfg := DashboardConfig{Urgency: UrgencyConfig{Easing: "linear"}}
	if got := cfg.EasingCurve(); got != urgency.Linear {
		t.Errorf("EasingCurve(linear) = %v, want linear", got)
	}
}
