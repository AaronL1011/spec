package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/aaronl1011/spec/internal/adapter"
	"github.com/aaronl1011/spec/internal/config"
)

func testSecurityModel() securityModel {
	rc := testResolvedConfig()
	reg := testRegistry()
	styles := NewStyles(ResolveTheme("catppuccin-mocha"))
	keys := DefaultKeyMap()
	m := newSecurity(rc, reg, styles, keys)
	m.loading = false
	m.width = 100
	m.height = 30
	return m
}

func TestSecurity_EmptyQueue(t *testing.T) {
	m := testSecurityModel()
	m.items = nil
	if got := m.view(); !strings.Contains(got, "No open vulnerabilities") {
		t.Errorf("empty security tab should show 'No open vulnerabilities', got: %q", got)
	}
}

func TestSecurity_WithItems(t *testing.T) {
	m := testSecurityModel()
	now := time.Now()
	m.items = []securityItem{
		{Number: 1131, Title: "routing bypass", Severity: adapter.SeverityMedium, Package: "http-proxy-middleware", Repo: "outlook_plugin", CreatedAt: now.Add(-3 * 24 * time.Hour)},
	}
	got := m.view()
	if !strings.Contains(got, "1 open vulnerabilities") {
		t.Error("should show the count line")
	}
	if !strings.Contains(got, "MED") {
		t.Error("should render the severity badge")
	}
	if !strings.Contains(got, "http-proxy-middleware") {
		t.Error("should render the package")
	}
}

func TestSecurity_DeadlineFraction(t *testing.T) {
	m := testSecurityModel()
	now := time.Now()

	// Critical default SLA is 1 day; 12h old is partway ⇒ fraction > 0.
	crit := securityItem{Severity: adapter.SeverityCritical, CreatedAt: now.Add(-12 * time.Hour)}
	if f := m.deadlineFraction(crit, now); f <= 0 {
		t.Errorf("12h-old critical fraction = %v, want > 0", f)
	}

	// A freshly-detected alert is not urgent yet.
	fresh := securityItem{Severity: adapter.SeverityCritical, CreatedAt: now}
	if f := m.deadlineFraction(fresh, now); f != 0 {
		t.Errorf("fresh fraction = %v, want 0", f)
	}

	// Unknown severity has no SLA window ⇒ never coloured.
	unk := securityItem{Severity: adapter.SeverityUnknown, CreatedAt: now.Add(-100 * 24 * time.Hour)}
	if f := m.deadlineFraction(unk, now); f != 0 {
		t.Errorf("unknown-severity fraction = %v, want 0", f)
	}

	// Past the deadline pins to the hottest stop (fraction 1).
	over := securityItem{Severity: adapter.SeverityCritical, CreatedAt: now.Add(-48 * time.Hour)}
	if f := m.deadlineFraction(over, now); f < 1 {
		t.Errorf("overdue fraction = %v, want 1", f)
	}
}

func TestSecurity_DeadlineLabel(t *testing.T) {
	m := testSecurityModel()
	now := time.Now()

	soon := securityItem{Severity: adapter.SeverityCritical, CreatedAt: now.Add(-2 * time.Hour)}
	if got := m.deadlineLabel(soon, now); !strings.Contains(got, "left") {
		t.Errorf("deadlineLabel = %q, want a '... left' countdown", got)
	}

	over := securityItem{Severity: adapter.SeverityCritical, CreatedAt: now.Add(-48 * time.Hour)}
	if got := m.deadlineLabel(over, now); got != "overdue" {
		t.Errorf("overdue label = %q, want 'overdue'", got)
	}
}

func TestSecurity_SortByDeadline(t *testing.T) {
	m := testSecurityModel()
	now := time.Now()
	// Both detected now: critical (1d SLA) breaches sooner than low (30d SLA).
	m.items = []securityItem{
		{Number: 1, Severity: adapter.SeverityLow, CreatedAt: now},
		{Number: 2, Severity: adapter.SeverityCritical, CreatedAt: now},
	}
	m.sortByDeadline()
	if m.items[0].Number != 2 {
		t.Errorf("expected the critical alert (soonest deadline) first, got item %d", m.items[0].Number)
	}
}

func TestSecurityPRSignatures_Dependabot(t *testing.T) {
	rc := testResolvedConfig()
	rc.Team.Integrations.Security = config.ProviderConfig{Provider: "dependabot"}

	authors, prefixes := securityPRSignatures(rc)
	if len(authors) == 0 || authors[0] != "dependabot[bot]" {
		t.Fatalf("dependabot authors = %v", authors)
	}
	if !isSecurityPR(adapter.PullRequest{Author: "dependabot[bot]", Branch: "dependabot/npm/x"}, authors, prefixes) {
		t.Error("a dependabot PR should be recognised as a security PR")
	}
	// Match by branch prefix even if the author differs.
	if !isSecurityPR(adapter.PullRequest{Author: "someone", Branch: "dependabot/npm/y"}, authors, prefixes) {
		t.Error("a dependabot/* branch should be recognised as a security PR")
	}
	if isSecurityPR(adapter.PullRequest{Author: "alice", Branch: "feature/x"}, authors, prefixes) {
		t.Error("a normal PR should not be treated as a security PR")
	}
}

func TestSecurityPRSignatures_NoneConfigured(t *testing.T) {
	rc := testResolvedConfig() // no security provider
	authors, prefixes := securityPRSignatures(rc)
	if authors != nil || prefixes != nil {
		t.Errorf("no provider should yield nil signatures, got %v / %v", authors, prefixes)
	}
	// With no signatures nothing is filtered, so even a bot PR stays in Reviews.
	if isSecurityPR(adapter.PullRequest{Author: "dependabot[bot]", Branch: "dependabot/x"}, authors, prefixes) {
		t.Error("with no security provider configured, nothing should be filtered")
	}
}
