package adapter

import (
	"context"
	"strings"
	"time"

	"github.com/aaronl1011/spec/internal/config"
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

// SecurityPRSignatures returns the bot author handles and branch prefixes that
// identify the configured security provider's fix PRs, so both the Reviews tab
// and the dashboard REVIEW section can exclude them — security items live only
// in the Security tab. Returns nil when no security provider is configured.
func SecurityPRSignatures(sec config.ProviderConfig) (authors, branchPrefixes []string) {
	switch sec.Provider {
	case "dependabot":
		return []string{"dependabot[bot]"}, []string{"dependabot/"}
	case "renovate":
		return []string{"renovate[bot]", "renovate"}, []string{"renovate/"}
	case "snyk":
		return []string{"snyk-bot"}, []string{"snyk-fix-", "snyk-upgrade-", "snyk-"}
	case "custom":
		var as, bs []string
		if a := sec.Get("bot_author"); a != "" {
			as = []string{a}
		}
		if b := sec.Get("branch_prefix"); b != "" {
			bs = []string{b}
		}
		return as, bs
	default:
		return nil, nil
	}
}

// IsSecurityPR reports whether a PR is one of the security provider's fix PRs,
// by bot author or branch prefix.
func IsSecurityPR(pr PullRequest, authors, branchPrefixes []string) bool {
	for _, a := range authors {
		if strings.EqualFold(pr.Author, a) {
			return true
		}
	}
	for _, p := range branchPrefixes {
		if p != "" && strings.HasPrefix(pr.Branch, p) {
			return true
		}
	}
	return false
}
