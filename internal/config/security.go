package config

import (
	"strings"
	"time"
)

// Severity tiers for security alerts. These match the normalized severities
// providers report — GitHub Dependabot alerts use low/medium/high/critical
// (the GitHub UI renders "medium" as "Moderate"). They key the per-tier SLA
// windows and are reused by the adapter and TUI layers.
const (
	TierCritical = "critical"
	TierHigh     = "high"
	TierMedium   = "medium"
	TierLow      = "low"
)

// defaultSecuritySLA is the built-in SLA window per severity tier, used when a
// team leaves a tier unset: critical 1 day, high 1 week, medium 2 weeks, low 1
// month. Deadline = alert detection time + window.
var defaultSecuritySLA = map[string]time.Duration{
	TierCritical: day,
	TierHigh:     week,
	TierMedium:   2 * week,
	TierLow:      30 * day,
}

// defaultSecuritySurfaceWithin is how close to its deadline an alert must be
// before it is promoted onto the dashboard, when unconfigured.
const defaultSecuritySurfaceWithin = 24 * time.Hour

// SecurityConfig is the team's vulnerability-SLA policy. It is a sibling of
// DashboardConfig (policy), while the provider connection lives in
// integrations.security (connection).
type SecurityConfig struct {
	// SLA holds the per-severity deadline windows. Empty tiers fall back to the
	// built-in defaults (critical 1d / high 1w / medium 2w / low 30d).
	SLA SecuritySLA `yaml:"sla,omitempty"`

	// DashboardSurfaceWithin promotes an alert onto the dashboard once its
	// remaining time-to-deadline drops below this window. Empty ⇒ 24h.
	DashboardSurfaceWithin string `yaml:"dashboard_surface_within,omitempty"`
}

// SecuritySLA holds the per-severity SLA windows as duration strings
// (m/h/d/w units, e.g. "1d", "1w", "2w", "30d").
type SecuritySLA struct {
	Critical string `yaml:"critical,omitempty"`
	High     string `yaml:"high,omitempty"`
	Medium   string `yaml:"medium,omitempty"`
	Low      string `yaml:"low,omitempty"`
}

// SLAFor returns the SLA window for a severity tier. A configured value takes
// precedence; an empty or unparseable value falls back to the built-in default.
// ok is false only for an unrecognised severity.
func (s SecurityConfig) SLAFor(severity string) (window time.Duration, ok bool) {
	tier := strings.ToLower(strings.TrimSpace(severity))
	def, known := defaultSecuritySLA[tier]
	if !known {
		return 0, false
	}
	var raw string
	switch tier {
	case TierCritical:
		raw = s.SLA.Critical
	case TierHigh:
		raw = s.SLA.High
	case TierMedium:
		raw = s.SLA.Medium
	case TierLow:
		raw = s.SLA.Low
	}
	if raw != "" {
		if d, err := ParseDuration(raw); err == nil && d > 0 {
			return d, true
		}
	}
	return def, true
}

// SurfaceWindow returns how close to its deadline a security alert must be
// before it is promoted onto the dashboard, defaulting to 24h.
func (s SecurityConfig) SurfaceWindow() time.Duration {
	if s.DashboardSurfaceWithin != "" {
		if d, err := ParseDuration(s.DashboardSurfaceWithin); err == nil && d > 0 {
			return d
		}
	}
	return defaultSecuritySurfaceWithin
}

// SecurityProviderNames lists the recognised security-scanner providers. The
// three named services are presets over a shared matching mechanism (bot
// author + branch prefix + severity source); "custom" exposes those knobs
// directly so any tool works.
func SecurityProviderNames() []string {
	return []string{"dependabot", "renovate", "snyk", "custom"}
}

// SecurityScopeNames lists the recognised alert scopes.
func SecurityScopeNames() []string {
	return []string{"repo", "org"}
}

// SecurityScopeOrDefault normalizes a raw scope value to "repo" or "org",
// defaulting to "org" (org-wide alerts) when empty or unrecognised.
func SecurityScopeOrDefault(scope string) string {
	if strings.EqualFold(strings.TrimSpace(scope), "repo") {
		return "repo"
	}
	return "org"
}

// isKnownSecurityProvider reports whether name is a recognised provider.
func isKnownSecurityProvider(name string) bool {
	for _, p := range SecurityProviderNames() {
		if strings.EqualFold(name, p) {
			return true
		}
	}
	return false
}

// isKnownSecurityScope reports whether name is a recognised scope.
func isKnownSecurityScope(name string) bool {
	for _, s := range SecurityScopeNames() {
		if strings.EqualFold(name, s) {
			return true
		}
	}
	return false
}
