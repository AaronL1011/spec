package config

import "testing"

// TestLint_StaleAfter validates that a bad stale_after is flagged and a good
// one (incl. "none") is accepted.
func TestLint_StaleAfter(t *testing.T) {
	body := `version: "1"
pipeline:
  stages:
    - name: build
      owner: engineer
      stale_after: 5quux
    - name: done
      owner: tl
      stale_after: none
`
	path := writeLintConfig(t, body)
	res, err := LintTeamConfigFile(path)
	if err != nil {
		t.Fatalf("lint: %v", err)
	}
	d := findDiag(res.Diagnostics, "stale_after")
	if d == nil {
		t.Fatal("expected a stale_after diagnostic for an unparseable duration")
	}
	if d.Severity != SeverityError {
		t.Errorf("stale_after diagnostic severity = %v, want error", d.Severity)
	}
}

func TestLint_StaleAfterValidIsClean(t *testing.T) {
	body := `version: "1"
pipeline:
  stages:
    - name: build
      owner: engineer
      stale_after: 5d
`
	path := writeLintConfig(t, body)
	res, err := LintTeamConfigFile(path)
	if err != nil {
		t.Fatalf("lint: %v", err)
	}
	if d := findDiag(res.Diagnostics, "stale_after"); d != nil {
		t.Errorf("valid stale_after flagged: %+v", d)
	}
}

// TestLint_UrgencyEasing validates the dashboard.urgency.easing enum.
func TestLint_UrgencyEasing(t *testing.T) {
	body := `version: "1"
dashboard:
  urgency:
    easing: ease-out
`
	path := writeLintConfig(t, body)
	res, err := LintTeamConfigFile(path)
	if err != nil {
		t.Fatalf("lint: %v", err)
	}
	d := findDiag(res.Diagnostics, "urgency.easing")
	if d == nil {
		t.Fatal("expected a diagnostic for an unknown easing value")
	}
	if d.Suggestion == "" {
		t.Error("expected a did-you-mean suggestion for the easing typo")
	}
}

func TestLint_UrgencyEasingValidIsClean(t *testing.T) {
	for _, ease := range []string{"linear", "ease-in", "ease-in-strong"} {
		body := "version: \"1\"\ndashboard:\n  urgency:\n    easing: " + ease + "\n"
		path := writeLintConfig(t, body)
		res, err := LintTeamConfigFile(path)
		if err != nil {
			t.Fatalf("lint: %v", err)
		}
		if d := findDiag(res.Diagnostics, "urgency.easing"); d != nil {
			t.Errorf("valid easing %q flagged: %+v", ease, d)
		}
	}
}
