package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeUserCfg(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLintUserIdentitiesFile(t *testing.T) {
	team := teamWith(map[string]string{"repo": "github", "comms": "slack"})

	tests := []struct {
		name      string
		content   string
		team      *TeamConfig
		wantWarns int
		wantField string // substring expected in a diagnostic field, if wantWarns > 0
	}{
		{
			name: "all keys map to configured providers",
			content: `user:
  handle: aaron
  identities:
    github: AaronL1011
    slack: "@aaron"
`,
			team:      team,
			wantWarns: 0,
		},
		{
			name: "unused provider key warns",
			content: `user:
  handle: aaron
  identities:
    github: AaronL1011
    jira: aaron.lewis
`,
			team:      team,
			wantWarns: 1,
			wantField: "user.identities.jira",
		},
		{
			name: "typo'd provider key warns",
			content: `user:
  handle: aaron
  identities:
    gihub: AaronL1011
`,
			team:      team,
			wantWarns: 1,
			wantField: "user.identities.gihub",
		},
		{
			name: "no identities map yields no warnings",
			content: `user:
  handle: aaron
`,
			team:      team,
			wantWarns: 0,
		},
		{
			name: "nil team config yields no warnings",
			content: `user:
  handle: aaron
  identities:
    github: AaronL1011
`,
			team:      nil,
			wantWarns: 1, // github is "unused" because no providers are configured
			wantField: "user.identities.github",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeUserCfg(t, tt.content)
			res, err := LintUserIdentitiesFile(path, tt.team)
			if err != nil {
				t.Fatalf("LintUserIdentitiesFile: %v", err)
			}
			warns := 0
			var fields []string
			for _, d := range res.Diagnostics {
				if d.Severity == SeverityWarning {
					warns++
					fields = append(fields, d.Field)
				}
				if d.Severity == SeverityError {
					t.Errorf("identity lint must never emit errors, got: %+v", d)
				}
			}
			if warns != tt.wantWarns {
				t.Fatalf("warnings = %d (%v), want %d", warns, fields, tt.wantWarns)
			}
			if tt.wantField != "" {
				found := false
				for _, f := range fields {
					if f == tt.wantField {
						found = true
					}
				}
				if !found {
					t.Errorf("expected a warning for field %q, got fields %v", tt.wantField, fields)
				}
			}
		})
	}
}
