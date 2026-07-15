package adapter

import (
	"context"
	"strings"
	"time"
)

// Severity is a normalized vulnerability severity, aligned with the levels
// security providers report. GitHub Dependabot alerts use
// low/medium/high/critical (the GitHub UI renders "medium" as "Moderate").
type Severity string

const (
	SeverityUnknown  Severity = ""
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

// NormalizeSeverity maps a provider's raw severity string to a normalized
// Severity. It is case-insensitive and treats GitHub's UI term "moderate" as
// medium. An unrecognised value yields SeverityUnknown.
func NormalizeSeverity(s string) Severity {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "critical":
		return SeverityCritical
	case "high":
		return SeverityHigh
	case "medium", "moderate":
		return SeverityMedium
	case "low":
		return SeverityLow
	default:
		return SeverityUnknown
	}
}

// SecurityAlert is a normalized dependency-vulnerability alert from a scanning
// provider (Dependabot, Renovate, Snyk, or a custom tool).
type SecurityAlert struct {
	Number     int
	Title      string    // advisory summary
	Severity   Severity  // normalized severity
	Package    string    // affected package name
	Manifest   string    // manifest path where the dependency is declared
	Repo       string    // repository the alert belongs to
	State      string    // provider state, e.g. "open"
	CreatedAt  time.Time // detection time — the SLA clock
	URL        string    // deep link to the alert
	Identifier string    // CVE or GHSA identifier
	FixPRURL   string    // optional link to a fix PR, when resolvable
}

// SecurityAdapter fetches dependency-vulnerability alerts from a scanning
// provider. It is read-only: the TUI surfaces alerts and their deadlines but
// never mutates provider state.
type SecurityAdapter interface {
	// Alerts returns open vulnerability alerts for the configured scope.
	Alerts(ctx context.Context) ([]SecurityAlert, error)
}
