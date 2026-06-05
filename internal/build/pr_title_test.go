package build

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFillPRTitle(t *testing.T) {
	cases := []struct {
		name, format, typ, epic, desc, want string
	}{
		{"nexl360 bracket form", "[{epic}] {desc}", "", "DEV-42", "add limiter", "[DEV-42] add limiter"},
		{"conventional form", "{type}: {epic} {desc}", "feat", "DEV-42", "add limiter", "feat: DEV-42 add limiter"},
		{"missing epic stays tidy", "[{epic}] {desc}", "", "", "add limiter", "add limiter"},
		{"missing type stays tidy", "{type}: {epic} {desc}", "", "DEV-7", "wire it", "DEV-7 wire it"},
	}
	for _, c := range cases {
		if got := fillPRTitle(c.format, c.typ, c.epic, c.desc); got != c.want {
			t.Errorf("%s: fillPRTitle = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestComposePRTitle_RepoConvention asserts spec-cli applies a repo's pr_title
// template from the registry, that an explicit title overrides it, and that an
// undeclared repo falls back to the generic default.
func TestComposePRTitle_RepoConvention(t *testing.T) {
	repo := t.TempDir()
	skillsDir := filepath.Join(repo, ".agents", "skills")
	writeSkill(t, skillsDir, "svc", "# svc")
	registry := "conventions:\n  pr_title: \"[{epic}] {desc}\"\nskills:\n  - name: svc\n    kind: layer\n    path: .agents/skills/svc\n    applies_to: [\"layer:svc\"]\n"
	if err := os.WriteFile(filepath.Join(skillsDir, "registry.yaml"), []byte(registry), 0o644); err != nil {
		t.Fatal(err)
	}

	session := &SessionState{SpecID: "SPEC-9", WorkDir: repo, Steps: []PRStep{{Number: 1, ID: "n1", Repo: filepath.Base(repo), Layer: "svc", Description: "add limiter"}}}
	s := NewMCPServer(session, &BuildContext{}, nil, "", Options{})

	// No specPath → empty epic → the template tidies "[] add limiter" to "add limiter".
	if got := s.composePRTitle("", filepath.Base(repo), "feat", "add limiter"); got != "add limiter" {
		t.Errorf("convention compose with empty epic = %q, want %q", got, "add limiter")
	}
	if got := s.composePRTitle("explicit title", filepath.Base(repo), "feat", "x"); got != "explicit title" {
		t.Errorf("explicit title should win, got %q", got)
	}

	// A repo with no registry falls back to the generic default.
	bare := t.TempDir()
	s2 := NewMCPServer(&SessionState{SpecID: "SPEC-9", WorkDir: bare, Steps: []PRStep{{Number: 1, ID: "n1", Repo: filepath.Base(bare), Description: "do thing"}}}, &BuildContext{}, nil, "", Options{})
	if got := s2.composePRTitle("", filepath.Base(bare), "feat", "do thing"); got != "SPEC-9: do thing" {
		t.Errorf("fallback = %q, want %q", got, "SPEC-9: do thing")
	}
}
