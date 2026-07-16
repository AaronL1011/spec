package adapter

import (
	"testing"

	"github.com/aaronl1011/spec/internal/config"
)

func TestNormalizeSeverity(t *testing.T) {
	cases := []struct {
		in   string
		want Severity
	}{
		{"critical", SeverityCritical},
		{"CRITICAL", SeverityCritical},
		{"high", SeverityHigh},
		{"medium", SeverityMedium},
		{"moderate", SeverityMedium}, // GitHub UI term for medium
		{"Moderate", SeverityMedium},
		{"low", SeverityLow},
		{" low ", SeverityLow},
		{"", SeverityUnknown},
		{"bogus", SeverityUnknown},
	}
	for _, c := range cases {
		if got := NormalizeSeverity(c.in); got != c.want {
			t.Errorf("NormalizeSeverity(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSecurityPRSignatures(t *testing.T) {
	authors, prefixes := SecurityPRSignatures(config.ProviderConfig{Provider: "dependabot"})
	if len(authors) == 0 || authors[0] != "dependabot[bot]" || len(prefixes) == 0 || prefixes[0] != "dependabot/" {
		t.Errorf("dependabot signatures = %v / %v", authors, prefixes)
	}

	// No provider → no signatures → nothing is a security PR.
	na, np := SecurityPRSignatures(config.ProviderConfig{})
	if na != nil || np != nil {
		t.Errorf("unconfigured signatures = %v / %v, want nil", na, np)
	}
}

func TestIsSecurityPR(t *testing.T) {
	authors, prefixes := SecurityPRSignatures(config.ProviderConfig{Provider: "dependabot"})

	cases := []struct {
		pr   PullRequest
		want bool
	}{
		{PullRequest{Author: "dependabot[bot]", Branch: "dependabot/npm/x"}, true},
		{PullRequest{Author: "someone", Branch: "dependabot/npm/y"}, true}, // branch match
		{PullRequest{Author: "DEPENDABOT[BOT]", Branch: "z"}, true},        // case-insensitive author
		{PullRequest{Author: "alice", Branch: "feature/x"}, false},
	}
	for _, c := range cases {
		if got := IsSecurityPR(c.pr, authors, prefixes); got != c.want {
			t.Errorf("IsSecurityPR(%+v) = %v, want %v", c.pr, got, c.want)
		}
	}
	// With no signatures, nothing matches.
	if IsSecurityPR(PullRequest{Author: "dependabot[bot]"}, nil, nil) {
		t.Error("with no signatures, nothing should match")
	}
}
