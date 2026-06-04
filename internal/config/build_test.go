package config

import "testing"

func TestBuildConfig_GetMaxParallel(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want int
	}{
		{"unset defaults to 4", 0, 4},
		{"negative defaults to 4", -2, 4},
		{"explicit value respected", 8, 8},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := (BuildConfig{MaxParallel: tt.in}).GetMaxParallel(); got != tt.want {
				t.Errorf("GetMaxParallel() = %d, want %d", got, tt.want)
			}
		})
	}
}
