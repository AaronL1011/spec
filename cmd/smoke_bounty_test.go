package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aaronl1011/spec/internal/markdown"
)

// bountyTeamConfig is the smoke team config with bounties enabled. maxActive of
// 2 keeps the cap assertion cheap.
func (e *smokeEnv) writeBountyTeamConfig(maxActive int, grantableBy string) {
	e.t.Helper()
	content := "version: \"1\"\n" +
		"team:\n" +
		"  name: Test Team\n" +
		"  cycle: Cycle 0\n" +
		"specs_repo:\n" +
		"  provider: github\n" +
		"  owner: " + e.repoOwner + "\n" +
		"  repo: " + e.repoName + "\n" +
		"  branch: " + e.branch + "\n" +
		"  token: " + smokeToken + "\n" +
		"bounty:\n" +
		"  enabled: true\n" +
		"  grantable_by: [" + grantableBy + "]\n" +
		"  max_active: " + itoa(maxActive) + "\n" +
		// A two-stage pipeline owned by tl keeps the bounty lifecycle testable
		// from one role: engineering is assignee-scoped (so it is claimable) and
		// done is the last required stage, hence terminal.
		"pipeline:\n" +
		"  stages:\n" +
		"    - name: engineering\n" +
		"      owner_role: tl\n" +
		"      dashboard:\n" +
		"        do_scope: assignee\n" +
		"    - name: done\n" +
		"      owner_role: tl\n"
	if err := os.WriteFile(filepath.Join(e.workDir, "spec.config.yaml"), []byte(content), 0o644); err != nil {
		e.t.Fatalf("write bounty team config: %v", err)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

// readSpecBounty reads a spec's bounty straight from the sandboxed clone, so
// assertions are against what was actually committed.
func (e *smokeEnv) readSpecBounty(specID string) *markdown.BountyState {
	e.t.Helper()
	meta, err := markdown.ReadMeta(filepath.Join(e.specsDirPath(), specID+".md"))
	if err != nil {
		e.t.Fatalf("read %s: %v", specID, err)
	}
	return meta.Bounty
}

// TestSmoke_BountySetStampsFrontmatter is AC-1: granting records granter,
// timestamp, and reason in the spec itself.
func TestSmoke_BountySetStampsFrontmatter(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("tl")
	e.writeBountyTeamConfig(2, "tl, pm")
	e.writeSpec(specFixture{id: "SPEC-001", title: "Billing migration", status: "engineering", author: "Dev"}, "## TL;DR\nx\n")
	e.initSpecsGit()

	out, err := e.runSpec("bounty", "set", "SPEC-001", "--reason", "unblocks the billing migration")
	if err != nil {
		t.Fatalf("bounty set: unexpected error: %v", err)
	}
	if !strings.Contains(out, "bountied") {
		t.Errorf("bounty set output = %q, want a confirmation", out)
	}

	b := e.readSpecBounty("SPEC-001")
	if b == nil {
		t.Fatal("bounty was not written to frontmatter")
	}
	if b.GrantedBy != "dev" {
		t.Errorf("granted_by = %q, want dev", b.GrantedBy)
	}
	if b.Reason != "unblocks the billing migration" {
		t.Errorf("reason = %q", b.Reason)
	}
	if b.GrantedAt == "" {
		t.Error("granted_at must be stamped")
	}
}

// TestSmoke_BountySetRequiresReason is AC-1's refusal path.
func TestSmoke_BountySetRequiresReason(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("tl")
	e.writeBountyTeamConfig(2, "tl")
	e.writeSpec(specFixture{id: "SPEC-001", title: "A", status: "engineering", author: "Dev"}, "## TL;DR\nx\n")
	e.initSpecsGit()

	_, err := e.runSpec("bounty", "set", "SPEC-001")
	if err == nil {
		t.Fatal("expected a refusal without --reason")
	}
	if !strings.Contains(err.Error(), "--reason") {
		t.Errorf("error = %q, want the next action named", err)
	}
	if e.readSpecBounty("SPEC-001") != nil {
		t.Error("a refused grant must not write a bounty")
	}
}

// TestSmoke_BountyRoleGate is AC-2: a role outside grantable_by is refused and
// told which roles may grant.
func TestSmoke_BountyRoleGate(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeBountyTeamConfig(2, "tl")
	e.writeSpec(specFixture{id: "SPEC-001", title: "A", status: "engineering", author: "Dev"}, "## TL;DR\nx\n")
	e.initSpecsGit()

	_, err := e.runSpec("bounty", "set", "SPEC-001", "--reason", "because")
	if err == nil {
		t.Fatal("expected a permission error for a non-granting role")
	}
	if !strings.Contains(err.Error(), "tl") {
		t.Errorf("error = %q, want the allowed roles named", err)
	}
	if e.readSpecBounty("SPEC-001") != nil {
		t.Error("a refused grant must not write a bounty")
	}
}

// TestSmoke_BountyDisabled is AC-13: with no bounty block, the command refuses
// and points at the config.
func TestSmoke_BountyDisabled(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("tl")
	e.writeTeamConfig() // no bounty block
	e.writeSpec(specFixture{id: "SPEC-001", title: "A", status: "engineering", author: "Dev"}, "## TL;DR\nx\n")
	e.initSpecsGit()

	_, err := e.runSpec("bounty", "set", "SPEC-001", "--reason", "because")
	if err == nil {
		t.Fatal("expected a refusal when bounties are disabled")
	}
	if !strings.Contains(err.Error(), "bounty.enabled") {
		t.Errorf("error = %q, want the config key named", err)
	}
}

// TestSmoke_BountyCap is AC-3: at the cap, granting fails and names the specs
// the granter has to choose between.
func TestSmoke_BountyCap(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("tl")
	e.writeBountyTeamConfig(2, "tl")
	for _, id := range []string{"SPEC-001", "SPEC-002", "SPEC-003"} {
		e.writeSpec(specFixture{id: id, title: id, status: "engineering", author: "Dev"}, "## TL;DR\nx\n")
	}
	e.initSpecsGit()

	for _, id := range []string{"SPEC-001", "SPEC-002"} {
		if _, err := e.runSpec("bounty", "set", id, "--reason", "worth taking"); err != nil {
			t.Fatalf("bounty set %s: %v", id, err)
		}
	}

	_, err := e.runSpec("bounty", "set", "SPEC-003", "--reason", "also worth taking")
	if err == nil {
		t.Fatal("expected the cap to block a third bounty")
	}
	for _, want := range []string{"SPEC-001", "SPEC-002", "spec bounty clear"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("cap error = %q, want it to mention %q", err, want)
		}
	}
	if e.readSpecBounty("SPEC-003") != nil {
		t.Error("a capped grant must not write a bounty")
	}

	// Clearing one frees the slot.
	if _, err := e.runSpec("bounty", "clear", "SPEC-001"); err != nil {
		t.Fatalf("bounty clear: %v", err)
	}
	if _, err := e.runSpec("bounty", "set", "SPEC-003", "--reason", "also worth taking"); err != nil {
		t.Fatalf("bounty set after clearing a slot: %v", err)
	}
}

// TestSmoke_BountyUpdateDoesNotConsumeSecondSlot covers the re-grant path: only
// the reason changes, so a spec already holding a slot is not blocked by the cap.
func TestSmoke_BountyUpdateDoesNotConsumeSecondSlot(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("tl")
	e.writeBountyTeamConfig(1, "tl")
	e.writeSpec(specFixture{id: "SPEC-001", title: "A", status: "engineering", author: "Dev"}, "## TL;DR\nx\n")
	e.initSpecsGit()

	if _, err := e.runSpec("bounty", "set", "SPEC-001", "--reason", "first reason"); err != nil {
		t.Fatalf("bounty set: %v", err)
	}
	if _, err := e.runSpec("bounty", "set", "SPEC-001", "--reason", "sharper reason"); err != nil {
		t.Fatalf("re-grant at the cap should be allowed: %v", err)
	}
	if got := e.readSpecBounty("SPEC-001").Reason; got != "sharper reason" {
		t.Errorf("reason = %q, want the updated reason", got)
	}
}

// TestSmoke_BountyClaimAndEarn is AC-8 + AC-9 end to end: assigning a bountied
// spec records the claimant, and advancing it to a terminal stage freezes the
// award.
func TestSmoke_BountyClaimAndEarn(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("tl")
	e.writeBountyTeamConfig(2, "tl")
	// engineering advances straight to done, the terminal stage, so one advance
	// settles the award.
	e.writeSpec(specFixture{id: "SPEC-001", title: "A", status: "engineering", author: "Dev"}, "## TL;DR\nx\n")
	e.initSpecsGit()

	if _, err := e.runSpec("bounty", "set", "SPEC-001", "--reason", "worth taking"); err != nil {
		t.Fatalf("bounty set: %v", err)
	}
	// A different person claims it: the granter cannot claim their own bounty.
	if _, err := e.runSpec("assign", "SPEC-001", "@priya"); err != nil {
		t.Fatalf("assign: %v", err)
	}
	b := e.readSpecBounty("SPEC-001")
	if b.ClaimedBy != "@priya" {
		t.Fatalf("claimed_by = %q, want @priya", b.ClaimedBy)
	}
	if b.EarnedAt != "" {
		t.Error("a claim must not record an award")
	}

	out, err := e.runSpec("advance", "SPEC-001")
	if err != nil {
		t.Fatalf("advance: %v", err)
	}
	if !strings.Contains(out, "bounty earned by @priya") {
		t.Errorf("advance output = %q, want the award reported", out)
	}
	b = e.readSpecBounty("SPEC-001")
	if b.EarnedBy != "@priya" || b.EarnedAt == "" {
		t.Fatalf("award not stamped: %+v", b)
	}
}

// TestSmoke_BountySelfClaimRefused is the self-dealing guard: the assignment
// still happens, but no award attaches to the granter.
func TestSmoke_BountySelfClaimRefused(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("tl")
	e.writeBountyTeamConfig(2, "tl")
	e.writeSpec(specFixture{id: "SPEC-001", title: "A", status: "engineering", author: "Dev"}, "## TL;DR\nx\n")
	e.initSpecsGit()

	if _, err := e.runSpec("bounty", "set", "SPEC-001", "--reason", "worth taking"); err != nil {
		t.Fatalf("bounty set: %v", err)
	}
	out, err := e.runSpec("assign", "SPEC-001")
	if err != nil {
		t.Fatalf("assign: %v", err)
	}
	if !strings.Contains(out, "not claimed") {
		t.Errorf("assign output = %q, want the refused self-claim explained", out)
	}
	if got := e.readSpecBounty("SPEC-001").ClaimedBy; got != "" {
		t.Errorf("claimed_by = %q, want empty for a self-claim", got)
	}
}

// TestSmoke_BountyClearRefusesClaimedWithoutForce is the retraction guard.
func TestSmoke_BountyClearRefusesClaimedWithoutForce(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("tl")
	e.writeBountyTeamConfig(2, "tl")
	e.writeSpec(specFixture{id: "SPEC-001", title: "A", status: "engineering", author: "Dev"}, "## TL;DR\nx\n")
	e.initSpecsGit()

	if _, err := e.runSpec("bounty", "set", "SPEC-001", "--reason", "worth taking"); err != nil {
		t.Fatalf("bounty set: %v", err)
	}
	if _, err := e.runSpec("assign", "SPEC-001", "@priya"); err != nil {
		t.Fatalf("assign: %v", err)
	}

	_, err := e.runSpec("bounty", "clear", "SPEC-001")
	if err == nil {
		t.Fatal("expected a claimed bounty to resist clearing")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("error = %q, want --force named", err)
	}
	if e.readSpecBounty("SPEC-001") == nil {
		t.Fatal("a refused clear must leave the bounty in place")
	}

	if _, err := e.runSpec("bounty", "clear", "SPEC-001", "--force"); err != nil {
		t.Fatalf("forced clear: %v", err)
	}
	if e.readSpecBounty("SPEC-001") != nil {
		t.Error("forced clear should remove the bounty")
	}
}

// TestSmoke_BountyListJSON is AC-11: machine-readable output carries the same
// shape the spec file stores.
func TestSmoke_BountyListJSON(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("tl")
	e.writeBountyTeamConfig(2, "tl")
	e.writeSpec(specFixture{id: "SPEC-001", title: "A", status: "engineering", author: "Dev"}, "## TL;DR\nx\n")
	e.writeSpec(specFixture{id: "SPEC-002", title: "B", status: "engineering", author: "Dev"}, "## TL;DR\nx\n")
	e.initSpecsGit()

	if _, err := e.runSpec("bounty", "set", "SPEC-001", "--reason", "worth taking"); err != nil {
		t.Fatalf("bounty set: %v", err)
	}

	out, err := e.runSpec("bounty", "list", "--json")
	if err != nil {
		t.Fatalf("bounty list --json: %v", err)
	}
	var views []struct {
		SpecID    string `json:"spec_id"`
		Reason    string `json:"reason"`
		GrantedBy string `json:"granted_by"`
	}
	if err := json.Unmarshal([]byte(out), &views); err != nil {
		t.Fatalf("unmarshal %q: %v", out, err)
	}
	if len(views) != 1 || views[0].SpecID != "SPEC-001" {
		t.Fatalf("views = %+v, want only SPEC-001", views)
	}
	if views[0].Reason != "worth taking" || views[0].GrantedBy != "dev" {
		t.Errorf("view = %+v, want the grant detail", views[0])
	}
}

// TestSmoke_BountyMarkerInList is AC-11's human path: a bountied row is marked
// in plain output, and JSON carries the bounty object.
func TestSmoke_BountyMarkerInList(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("tl")
	e.writeBountyTeamConfig(2, "tl")
	e.writeSpec(specFixture{id: "SPEC-001", title: "Bountied", status: "engineering", author: "Dev"}, "## TL;DR\nx\n")
	e.writeSpec(specFixture{id: "SPEC-002", title: "Plain", status: "engineering", author: "Dev"}, "## TL;DR\nx\n")
	e.initSpecsGit()

	if _, err := e.runSpec("bounty", "set", "SPEC-001", "--reason", "worth taking"); err != nil {
		t.Fatalf("bounty set: %v", err)
	}

	out, err := e.runSpec("list", "--all")
	if err != nil {
		t.Fatalf("list --all: %v", err)
	}
	if !strings.Contains(out, "✦ SPEC-001") {
		t.Errorf("list output = %q, want SPEC-001 marked with the spark", out)
	}
	if strings.Contains(out, "✦ SPEC-002") {
		t.Errorf("list output = %q, want SPEC-002 unmarked", out)
	}
}

// TestSmoke_BountyRejectsTriageID keeps bounties to specs: triage items have no
// bounty field, so the verb must refuse rather than half-work.
func TestSmoke_BountyRejectsTriageID(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("tl")
	e.writeBountyTeamConfig(2, "tl")
	e.initSpecsGit()

	_, err := e.runSpec("bounty", "set", "TRIAGE-001", "--reason", "because")
	if err == nil {
		t.Fatal("expected a refusal for a triage ID")
	}
	if !strings.Contains(err.Error(), "not a spec") {
		t.Errorf("error = %q, want it to explain that bounties apply to specs", err)
	}
}
