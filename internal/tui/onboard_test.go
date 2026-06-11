package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/aaronl1011/spec/internal/config"
)

// TestRunOnboarding_NonInteractiveErrors asserts the wizard refuses to run
// without a terminal and points the user at the scriptable path.
func TestRunOnboarding_NonInteractiveErrors(t *testing.T) {
	// In the test harness stdin is not a char device, so IsInteractive() is
	// false. Guard anyway in case a TTY is attached.
	if IsInteractive() {
		t.Skip("stdin is a terminal; non-interactive guard cannot be exercised here")
	}
	_, err := RunOnboarding(context.Background(), false)
	if err == nil {
		t.Fatal("expected an error in a non-interactive shell")
	}
	if !strings.Contains(err.Error(), "spec config init") {
		t.Errorf("error = %q, want it to point at 'spec config init'", err)
	}
}

func TestValidateRepoRef(t *testing.T) {
	tests := []struct {
		in      string
		wantErr bool
	}{
		{"acme/specs", false},
		{"github.com/acme/specs", false},
		{"https://github.com/acme/specs.git", false},
		{"", true},
		{"   ", true},
		{"not-a-repo", true},
		{"too/many/parts/here", true},
	}
	for _, tt := range tests {
		err := validateRepoRef(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("validateRepoRef(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
		}
	}
}

// TestWriteUserIdentity_RoundTrips drives the identity writer directly (the huh
// form itself needs a TTY, but the persistence path is pure) and confirms the
// written config loads back into a usable identity.
func TestWriteUserIdentity_RoundTrips(t *testing.T) {
	// Sandbox HOME so the write lands in a temp dir, not the developer's real
	// ~/.spec/config.yaml.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)

	if err := writeUserIdentity("  Ada Lovelace  ", "engineer", " @ada "); err != nil {
		t.Fatalf("writeUserIdentity: %v", err)
	}

	cfg, err := config.LoadUserConfig(config.UserConfigPath())
	if err != nil {
		t.Fatalf("load written config: %v", err)
	}
	if cfg.User.Name != "Ada Lovelace" {
		t.Errorf("name = %q, want trimmed 'Ada Lovelace'", cfg.User.Name)
	}
	if cfg.User.OwnerRole != "engineer" {
		t.Errorf("role = %q, want engineer", cfg.User.OwnerRole)
	}
	if cfg.User.Handle != "@ada" {
		t.Errorf("handle = %q, want trimmed '@ada'", cfg.User.Handle)
	}
}

// TestOnboardResult_ZeroValue documents the result contract: nothing is
// complete until a team is joined.
func TestOnboardResult_ZeroValue(t *testing.T) {
	var res OnboardResult
	if res.Completed || res.JoinedTeam || res.WroteUserConfig {
		t.Errorf("zero-value OnboardResult = %+v, want all false", res)
	}
}
