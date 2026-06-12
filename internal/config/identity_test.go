package config

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// teamWith builds a minimal team config with the given category->provider map.
func teamWith(providers map[string]string) *TeamConfig {
	tc := &TeamConfig{}
	tc.Integrations.Comms.Provider = providers["comms"]
	tc.Integrations.PM.Provider = providers["pm"]
	tc.Integrations.Docs.Provider = providers["docs"]
	tc.Integrations.Repo.Provider = providers["repo"]
	tc.Integrations.Agent.Provider = providers["agent"]
	tc.Integrations.AI.Provider = providers["ai"]
	tc.Integrations.Design.Provider = providers["design"]
	tc.Integrations.Deploy.Provider = providers["deploy"]
	return tc
}

func userWith(handle, name string, identities map[string]string) *UserConfig {
	uc := &UserConfig{}
	uc.User.Handle = handle
	uc.User.Name = name
	uc.User.Identities = identities
	return uc
}

func TestCanonicalHandle(t *testing.T) {
	tests := []struct {
		name string
		rc   ResolvedConfig
		want string
	}{
		{"handle set", ResolvedConfig{User: userWith("aaron", "Aaron Lewis", nil)}, "aaron"},
		{"falls back to name", ResolvedConfig{User: userWith("", "Aaron Lewis", nil)}, "Aaron Lewis"},
		{"no user", ResolvedConfig{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.rc.CanonicalHandle(); got != tt.want {
				t.Errorf("CanonicalHandle() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestProviderHandle(t *testing.T) {
	user := userWith("aaron", "Aaron Lewis", map[string]string{
		"github": "AaronL1011",
		"slack":  "@aaron",
	})
	tests := []struct {
		name     string
		rc       ResolvedConfig
		provider string
		want     string
	}{
		{"mapped provider", ResolvedConfig{User: user}, "github", "AaronL1011"},
		{"mapped provider case-insensitive", ResolvedConfig{User: user}, "GitHub", "AaronL1011"},
		{"unmapped provider falls back to canonical", ResolvedConfig{User: user}, "jira", "aaron"},
		{"empty provider yields canonical", ResolvedConfig{User: user}, "", "aaron"},
		{"none provider yields canonical", ResolvedConfig{User: user}, "none", "aaron"},
		{"no identities map falls back", ResolvedConfig{User: userWith("aaron", "Aaron", nil)}, "github", "aaron"},
		{"no user", ResolvedConfig{}, "github", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.rc.ProviderHandle(tt.provider); got != tt.want {
				t.Errorf("ProviderHandle(%q) = %q, want %q", tt.provider, got, tt.want)
			}
		})
	}
}

func TestIdentityForCategory(t *testing.T) {
	team := teamWith(map[string]string{"repo": "github", "comms": "slack", "pm": "jira"})
	user := userWith("aaron", "Aaron Lewis", map[string]string{
		"github": "AaronL1011",
		"slack":  "@aaron",
	})

	tests := []struct {
		name     string
		rc       ResolvedConfig
		category string
		want     string
	}{
		{"repo -> github login", ResolvedConfig{Team: team, User: user}, "repo", "AaronL1011"},
		{"comms -> slack handle", ResolvedConfig{Team: team, User: user}, "comms", "@aaron"},
		{"pm provider unmapped falls back to canonical", ResolvedConfig{Team: team, User: user}, "pm", "aaron"},
		{"category with no provider falls back", ResolvedConfig{Team: team, User: user}, "design", "aaron"},
		{"no team config falls back to canonical", ResolvedConfig{User: user}, "repo", "aaron"},
		{"unknown category falls back", ResolvedConfig{Team: team, User: user}, "bogus", "aaron"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.rc.IdentityForCategory(tt.category); got != tt.want {
				t.Errorf("IdentityForCategory(%q) = %q, want %q", tt.category, got, tt.want)
			}
		})
	}
}

// TestIdentityForCategory_LegacyConfig proves a config with only `handle`
// resolves every category to that handle (full backward compatibility).
func TestIdentityForCategory_LegacyConfig(t *testing.T) {
	team := teamWith(map[string]string{"repo": "github", "comms": "slack", "pm": "jira"})
	user := userWith("aaron", "Aaron Lewis", nil) // no identities map at all
	rc := ResolvedConfig{Team: team, User: user}

	for _, cat := range []string{"repo", "comms", "pm", "docs", "design", "deploy"} {
		if got := rc.IdentityForCategory(cat); got != "aaron" {
			t.Errorf("legacy IdentityForCategory(%q) = %q, want aaron", cat, got)
		}
	}
}

func TestUserIdentities(t *testing.T) {
	tests := []struct {
		name string
		rc   ResolvedConfig
		want []string
	}{
		{
			name: "handle, name, and provider handles deduped",
			rc: ResolvedConfig{User: userWith("aaron", "Aaron Lewis", map[string]string{
				"github": "AaronL1011",
				"slack":  "@aaron", // dupes canonical "aaron" after @/case normalisation
			})},
			want: []string{"aaron", "Aaron Lewis", "AaronL1011"},
		},
		{
			name: "no identities",
			rc:   ResolvedConfig{User: userWith("aaron", "Aaron Lewis", nil)},
			want: []string{"aaron", "Aaron Lewis"},
		},
		{"no user", ResolvedConfig{}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.rc.UserIdentities()
			sortedGot := append([]string(nil), got...)
			sortedWant := append([]string(nil), tt.want...)
			sort.Strings(sortedGot)
			sort.Strings(sortedWant)
			if len(sortedGot) != len(sortedWant) {
				t.Fatalf("UserIdentities() = %v, want %v", got, tt.want)
			}
			for i := range sortedGot {
				if sortedGot[i] != sortedWant[i] {
					t.Fatalf("UserIdentities() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

// TestUserConfigIdentitiesRoundTrip guards against the map being dropped on a
// settings save (write -> reload must preserve every entry).
func TestUserConfigIdentitiesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	cfg := userWith("aaron", "Aaron Lewis", map[string]string{
		"github": "AaronL1011",
		"slack":  "@aaron",
		"jira":   "aaron.lewis",
	})
	if err := WriteUserConfig(path, cfg); err != nil {
		t.Fatalf("WriteUserConfig: %v", err)
	}
	got, err := LoadUserConfig(path)
	if err != nil {
		t.Fatalf("LoadUserConfig: %v", err)
	}
	want := map[string]string{"github": "AaronL1011", "slack": "@aaron", "jira": "aaron.lewis"}
	if len(got.User.Identities) != len(want) {
		t.Fatalf("identities = %v, want %v", got.User.Identities, want)
	}
	for k, v := range want {
		if got.User.Identities[k] != v {
			t.Errorf("identities[%q] = %q, want %q (dropped on marshal?)", k, got.User.Identities[k], v)
		}
	}
}

// TestUserConfigParsesIdentities confirms the YAML shape from the plan parses.
func TestUserConfigParsesIdentities(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `user:
  owner_role: engineer
  name: "Aaron Lewis"
  handle: aaron
  identities:
    github: AaronL1011
    slack: "@aaron"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	uc, err := LoadUserConfig(path)
	if err != nil {
		t.Fatalf("LoadUserConfig: %v", err)
	}
	if uc.User.Identities["github"] != "AaronL1011" {
		t.Errorf("github identity = %q", uc.User.Identities["github"])
	}
	if uc.User.Identities["slack"] != "@aaron" {
		t.Errorf("slack identity = %q", uc.User.Identities["slack"])
	}
}
