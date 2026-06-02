package update

import "testing"

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		name    string
		a, b    string
		want    int
		wantErr bool
	}{
		{name: "equal", a: "v1.2.3", b: "1.2.3", want: 0},
		{name: "patch lower", a: "1.2.3", b: "1.2.4", want: -1},
		{name: "minor higher", a: "1.3.0", b: "1.2.9", want: 1},
		{name: "major lower", a: "1.9.9", b: "2.0.0", want: -1},
		{name: "prerelease below release", a: "1.2.3-rc.1", b: "1.2.3", want: -1},
		{name: "release above prerelease", a: "1.2.3", b: "1.2.3-rc.1", want: 1},
		{name: "prerelease lexical", a: "1.2.3-rc.1", b: "1.2.3-rc.2", want: -1},
		{name: "invalid left", a: "dev", b: "1.2.3", wantErr: true},
		{name: "invalid right", a: "1.2.3", b: "latest", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := compareVersions(tc.a, tc.b)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error comparing %q and %q", tc.a, tc.b)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("compareVersions(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestIsDev(t *testing.T) {
	for _, v := range []string{"", "dev", "  dev  "} {
		if !IsDev(v) {
			t.Errorf("IsDev(%q) = false, want true", v)
		}
	}
	if IsDev("v1.0.0") {
		t.Error("IsDev(v1.0.0) = true, want false")
	}
}

func TestUpdateAvailable(t *testing.T) {
	tests := []struct {
		name            string
		current, target string
		want            bool
	}{
		{name: "dev always updates", current: "dev", target: "v1.0.0", want: true},
		{name: "older current", current: "v1.0.0", target: "v1.1.0", want: true},
		{name: "same version", current: "v1.1.0", target: "v1.1.0", want: false},
		{name: "newer current", current: "v1.2.0", target: "v1.1.0", want: false},
		{name: "unparseable target updates", current: "v1.0.0", target: "nightly", want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := updateAvailable(tc.current, tc.target); got != tc.want {
				t.Errorf("updateAvailable(%q, %q) = %v, want %v", tc.current, tc.target, got, tc.want)
			}
		})
	}
}
