package onboard

import (
	"testing"
)

func TestParseRepoRef(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		provider string
		owner    string
		repo     string
		wantErr  bool
	}{
		{
			name:     "simple org/repo defaults to github",
			input:    "acme/specs",
			provider: "github",
			owner:    "acme",
			repo:     "specs",
		},
		{
			name:     "explicit github.com host",
			input:    "github.com/acme/specs",
			provider: "github",
			owner:    "acme",
			repo:     "specs",
		},
		{
			name:     "gitlab.com host",
			input:    "gitlab.com/acme/specs",
			provider: "gitlab",
			owner:    "acme",
			repo:     "specs",
		},
		{
			name:     "bitbucket.org host",
			input:    "bitbucket.org/acme/specs",
			provider: "bitbucket",
			owner:    "acme",
			repo:     "specs",
		},
		{
			name:     "https URL",
			input:    "https://github.com/acme/specs",
			provider: "github",
			owner:    "acme",
			repo:     "specs",
		},
		{
			name:     "https URL with .git suffix",
			input:    "https://github.com/acme/specs.git",
			provider: "github",
			owner:    "acme",
			repo:     "specs",
		},
		{
			name:     "http URL",
			input:    "http://gitlab.com/acme/specs",
			provider: "gitlab",
			owner:    "acme",
			repo:     "specs",
		},
		{
			name:     "trailing slash stripped",
			input:    "github.com/acme/specs/",
			provider: "github",
			owner:    "acme",
			repo:     "specs",
		},
		{
			name:    "single segment invalid",
			input:   "specs",
			wantErr: true,
		},
		{
			name:    "empty string invalid",
			input:   "",
			wantErr: true,
		},
		{
			name:    "unknown provider",
			input:   "unknown.com/acme/specs",
			wantErr: true,
		},
		{
			name:    "too many segments",
			input:   "github.com/acme/specs/extra/path",
			wantErr: true,
		},
		{
			name:    "empty owner",
			input:   "/specs",
			wantErr: true,
		},
		{
			name:    "empty repo",
			input:   "acme/",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, owner, repo, err := ParseRepoRef(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseRepoRef(%q) expected error, got nil", tt.input)
				}
				return
			}

			if err != nil {
				t.Errorf("ParseRepoRef(%q) unexpected error: %v", tt.input, err)
				return
			}

			if provider != tt.provider {
				t.Errorf("ParseRepoRef(%q) provider = %q, want %q", tt.input, provider, tt.provider)
			}
			if owner != tt.owner {
				t.Errorf("ParseRepoRef(%q) owner = %q, want %q", tt.input, owner, tt.owner)
			}
			if repo != tt.repo {
				t.Errorf("ParseRepoRef(%q) repo = %q, want %q", tt.input, repo, tt.repo)
			}
		})
	}
}

func TestTokenEnvVar(t *testing.T) {
	tests := []struct {
		provider string
		want     string
	}{
		{"github", "GITHUB_TOKEN"},
		{"gitlab", "GITLAB_TOKEN"},
		{"bitbucket", "BITBUCKET_TOKEN"},
		{"unknown", "GITHUB_TOKEN"}, // defaults to github
	}

	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			got := tokenEnvVar(tt.provider)
			if got != tt.want {
				t.Errorf("tokenEnvVar(%q) = %q, want %q", tt.provider, got, tt.want)
			}
		})
	}
}

func TestProviderFromHost(t *testing.T) {
	tests := []struct {
		host string
		want string
	}{
		{"github.com", "github"},
		{"gitlab.com", "gitlab"},
		{"bitbucket.org", "bitbucket"},
		{"unknown.com", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			got := providerFromHost(tt.host)
			if got != tt.want {
				t.Errorf("providerFromHost(%q) = %q, want %q", tt.host, got, tt.want)
			}
		})
	}
}
