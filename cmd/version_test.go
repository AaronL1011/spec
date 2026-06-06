package cmd

import "testing"

func TestResolveVersion_PrefersLdflagsStamp(t *testing.T) {
	orig := Version
	t.Cleanup(func() { Version = orig })

	Version = "v1.4.0"
	if got := resolveVersion(); got != "v1.4.0" {
		t.Errorf("resolveVersion() = %q, want %q", got, "v1.4.0")
	}
}

func TestResolveVersion_FallsBackToDevWithoutBuildInfo(t *testing.T) {
	orig := Version
	t.Cleanup(func() { Version = orig })

	// With no ldflags stamp and a test binary (whose embedded module version is
	// empty/"(devel)"), resolveVersion must degrade to the "dev" sentinel rather
	// than emit an empty or bogus version string.
	Version = "dev"
	if got := resolveVersion(); got != "dev" {
		t.Errorf("resolveVersion() = %q, want %q", got, "dev")
	}
}
