package resolve

import (
	"context"
	"strings"
	"testing"

	"github.com/aaronl1011/spec/internal/config"
)

func TestResolveSecurity_Unconfigured(t *testing.T) {
	reg, _ := All(&config.TeamConfig{})
	if reg.Security() == nil {
		t.Fatal("Security adapter is nil for empty config")
	}
	alerts, err := reg.Security().Alerts(context.Background())
	if err != nil || len(alerts) != 0 {
		t.Errorf("noop Alerts = (%v, %v), want (empty, nil)", alerts, err)
	}
}

func TestResolveSecurity_Dependabot(t *testing.T) {
	cfg := &config.TeamConfig{}
	cfg.Integrations.Security = config.ProviderConfig{
		Provider: "dependabot",
		Extra:    map[string]string{"token": "t", "owner": "NEXL-LTS", "scope": "org"},
	}
	reg, warns := All(cfg)
	if reg.Security() == nil {
		t.Fatal("configured Security adapter is nil")
	}
	for _, w := range warns {
		if strings.Contains(w, "security") {
			t.Errorf("unexpected security warning for a valid config: %q", w)
		}
	}
}

func TestResolveSecurity_MissingToken(t *testing.T) {
	cfg := &config.TeamConfig{}
	cfg.Integrations.Security = config.ProviderConfig{
		Provider: "dependabot",
		Extra:    map[string]string{"owner": "NEXL-LTS"},
	}
	_, warns := All(cfg)
	if !hasWarning(warns, "token not configured") {
		t.Errorf("expected a missing-token warning, got %v", warns)
	}
}

func TestResolveSecurity_SnykNotImplemented(t *testing.T) {
	cfg := &config.TeamConfig{}
	cfg.Integrations.Security = config.ProviderConfig{Provider: "snyk"}
	_, warns := All(cfg)
	if !hasWarning(warns, "snyk") {
		t.Errorf("expected a snyk not-implemented warning, got %v", warns)
	}
}

func hasWarning(warns []string, sub string) bool {
	for _, w := range warns {
		if strings.Contains(w, sub) {
			return true
		}
	}
	return false
}
