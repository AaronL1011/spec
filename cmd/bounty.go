package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aaronl1011/spec/internal/bounty"
	"github.com/aaronl1011/spec/internal/config"
	gitpkg "github.com/aaronl1011/spec/internal/git"
	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/aaronl1011/spec/internal/tui"
	"github.com/spf13/cobra"
)

var bountyCmd = &cobra.Command{
	Use:   "bounty",
	Short: "Mark a spec as worth claiming, and see who earned one",
	Long: `Place a bounty on a spec to invite an engineer to claim it.

A bounty is a pull signal, not an instruction: it makes a spec visibly
desirable wherever it appears, without assigning it, reordering queues, or
notifying anyone. Bounties are capped (bounty.max_active) so the marker stays
scarce, and every one carries a written reason.

Claiming and finishing a bountied spec is recorded in the spec itself, so
'spec bounty ledger' can tally earned bounties per person from git alone.`,
	Example: "  spec bounty set SPEC-042 --reason \"unblocks the billing migration\"\n  spec bounty list\n  spec bounty clear SPEC-042",
	RunE:    runBountyList,
}

var bountySetCmd = &cobra.Command{
	Use:   "set [id]",
	Short: "Place a bounty on a spec",
	Long: `Place a bounty on a spec, or update the reason on one it already carries.

Requires a role listed in bounty.grantable_by. Fails when the team's active
bounty cap is reached — scarcity is what makes the marker mean anything.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runBountySet,
}

var bountyClearCmd = &cobra.Command{
	Use:   "clear [id]",
	Short: "Remove a spec's bounty",
	Long: `Remove an active bounty.

A bounty someone has already claimed needs --force to remove: retracting an
accepted invitation should be a deliberate act.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runBountyClear,
}

var bountyListCmd = &cobra.Command{
	Use:   "list",
	Short: "List specs currently carrying a bounty",
	Args:  cobra.NoArgs,
	RunE:  runBountyList,
}

func init() {
	bountySetCmd.Flags().String("reason", "", "why this spec is worth claiming now")
	bountyClearCmd.Flags().Bool("force", false, "remove a bounty that has already been claimed")
	bountyCmd.AddCommand(bountySetCmd)
	bountyCmd.AddCommand(bountyClearCmd)
	bountyCmd.AddCommand(bountyListCmd)
	rootCmd.AddCommand(bountyCmd)
}

// bountyView is the render-ready shape of one bountied spec, shared by the
// human and JSON output paths.
type bountyView struct {
	SpecID    string `json:"spec_id"`
	Title     string `json:"title"`
	Stage     string `json:"stage"`
	GrantedBy string `json:"granted_by"`
	GrantedAt string `json:"granted_at"`
	Reason    string `json:"reason,omitempty"`
	ClaimedBy string `json:"claimed_by,omitempty"`
	EarnedBy  string `json:"earned_by,omitempty"`
	EarnedAt  string `json:"earned_at,omitempty"`
}

func runBountySet(cmd *cobra.Command, args []string) error {
	p := newPrinter(cmd)
	reason, _ := cmd.Flags().GetString("reason")

	rc, specID, err := bountyContext(args, "spec bounty set <id> --reason \"...\"")
	if err != nil {
		return err
	}
	role, err := requireRole(rc)
	if err != nil {
		return err
	}
	cfg := rc.Bounties()
	if err := bounty.Authorize(role, cfg); err != nil {
		return err
	}
	granter := selfIdentity(rc)
	if granter == "" {
		return fmt.Errorf("no identity to grant as — set user.handle or user.name in ~/.spec/config.yaml")
	}

	var view bountyView
	gitErr := gitpkg.WithSpecsRepoOpts(context.Background(), &rc.Team.SpecsRepo, syncOpts(cmd, specID), func(repoPath string) (string, error) {
		path, err := specPathIn(repoPath, rc, specID)
		if err != nil {
			return "", err
		}
		meta, err := markdown.ReadMeta(path)
		if err != nil {
			return "", err
		}

		// Cap check excludes this spec, so re-stating an existing bounty's
		// reason is never blocked by the cap it already occupies.
		active, err := activeBountySpecIDs(specsDir(repoPath), specID)
		if err != nil {
			return "", err
		}
		if !meta.BountyActive() {
			if err := bounty.CheckCap(active, cfg); err != nil {
				return "", err
			}
		}

		update := meta.BountyActive()
		if err := bounty.Grant(meta, granter, reason, cfg.ReasonRequired(), time.Now()); err != nil {
			return "", err
		}
		if err := markdown.WriteMeta(path, meta); err != nil {
			return "", err
		}
		view = newBountyView(meta)
		logBountyActivity(rc, specID, "bounty_granted", fmt.Sprintf("bounty granted by %s", granter),
			fmt.Sprintf(`{"granted_by":%q,"update":%t}`, granter, update))
		if update {
			return fmt.Sprintf("chore: update bounty reason on %s", specID), nil
		}
		return fmt.Sprintf("chore: bounty %s", specID), nil
	})
	if gitErr != nil {
		return gitErr
	}

	if p.JSONEnabled() {
		return p.JSON(view)
	}
	p.Line("%s %s bountied — %s", tui.IconSpark, specID, view.Reason)
	p.Line("  Anyone in the owning role can claim it; finishing it records the award.")
	return nil
}

func runBountyClear(cmd *cobra.Command, args []string) error {
	p := newPrinter(cmd)
	force, _ := cmd.Flags().GetBool("force")

	rc, specID, err := bountyContext(args, "spec bounty clear <id>")
	if err != nil {
		return err
	}
	role, err := requireRole(rc)
	if err != nil {
		return err
	}
	if err := bounty.Authorize(role, rc.Bounties()); err != nil {
		return err
	}

	gitErr := gitpkg.WithSpecsRepoOpts(context.Background(), &rc.Team.SpecsRepo, syncOpts(cmd, specID), func(repoPath string) (string, error) {
		path, err := specPathIn(repoPath, rc, specID)
		if err != nil {
			return "", err
		}
		meta, err := markdown.ReadMeta(path)
		if err != nil {
			return "", err
		}
		if err := bounty.Clear(specID, meta, force); err != nil {
			return "", err
		}
		if err := markdown.WriteMeta(path, meta); err != nil {
			return "", err
		}
		logBountyActivity(rc, specID, "bounty_cleared", "bounty cleared", fmt.Sprintf(`{"forced":%t}`, force))
		return fmt.Sprintf("chore: clear bounty on %s", specID), nil
	})
	if gitErr != nil {
		return gitErr
	}

	if p.JSONEnabled() {
		return p.JSON(map[string]any{"spec_id": specID, "bounty": nil})
	}
	p.Line("✓ bounty cleared on %s", specID)
	return nil
}

func runBountyList(cmd *cobra.Command, args []string) error {
	_ = args
	p := newPrinter(cmd)

	rc, err := resolveConfig()
	if err != nil {
		return err
	}
	if err := requireTeamConfig(rc); err != nil {
		return err
	}
	if !rc.BountyEnabled() {
		return bounty.ErrDisabled
	}
	if _, err := gitpkg.EnsureSpecsRepo(ctx(), &rc.Team.SpecsRepo); err != nil {
		return fmt.Errorf("syncing specs repo: %w", err)
	}

	metas, err := readSpecMetas(rc.SpecsRepoDir)
	if err != nil {
		return err
	}
	var views []bountyView
	for _, meta := range metas {
		if meta.BountyActive() {
			views = append(views, newBountyView(&meta))
		}
	}
	sort.Slice(views, func(i, j int) bool { return views[i].SpecID < views[j].SpecID })

	if p.JSONEnabled() {
		return p.JSON(views)
	}
	limit := rc.Bounties().Cap()
	if len(views) == 0 {
		p.Line("No active bounties. Up to %d can be active at once.", limit)
		return nil
	}
	p.Line("Active bounties (%d of %d):\n", len(views), limit)
	for _, v := range views {
		claim := "unclaimed"
		if v.ClaimedBy != "" {
			claim = "claimed by " + v.ClaimedBy
		}
		p.Line("  %s %-10s  %-36s  %s  ·  %s", tui.IconSpark, v.SpecID, truncate(v.Title, 36), v.Stage, claim)
		if v.Reason != "" {
			p.Line("      %s (%s)", v.Reason, v.GrantedBy)
		}
	}
	return nil
}

// bountyContext resolves the spec ID and config for a mutating bounty verb,
// rejecting anything that is not a spec (triage items carry no bounty).
func bountyContext(args []string, usage string) (*config.ResolvedConfig, string, error) {
	specID, err := resolveSpecIDArg(args, usage)
	if err != nil {
		return nil, "", err
	}
	if !looksLikeSpecID(specID) {
		return nil, "", fmt.Errorf("%s is not a spec — bounties apply to specs, not triage items", specID)
	}
	rc, err := resolveConfig()
	if err != nil {
		return nil, "", err
	}
	if err := requireTeamConfig(rc); err != nil {
		return nil, "", err
	}
	return rc, specID, nil
}

// newBountyView projects a spec's bounty into render-ready form.
func newBountyView(meta *markdown.SpecMeta) bountyView {
	v := bountyView{SpecID: meta.ID, Title: meta.Title, Stage: meta.Status}
	if meta.Bounty != nil {
		v.GrantedBy = meta.Bounty.GrantedBy
		v.GrantedAt = meta.Bounty.GrantedAt
		v.Reason = meta.Bounty.Reason
		v.ClaimedBy = meta.Bounty.ClaimedBy
		v.EarnedBy = meta.Bounty.EarnedBy
		v.EarnedAt = meta.Bounty.EarnedAt
	}
	return v
}

// activeBountySpecIDs lists the live specs carrying an unearned bounty,
// excluding excludeID. Only the specs/ root is scanned: archived specs are
// terminal, so they can never compete for a cap slot.
func activeBountySpecIDs(baseDir, excludeID string) ([]string, error) {
	metas, err := readSpecMetas(baseDir)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, meta := range metas {
		if meta.ID != excludeID && meta.BountyActive() {
			ids = append(ids, meta.ID)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// readSpecMetas parses the frontmatter of every spec directly inside dir.
// Unreadable files are skipped: one hand-mangled spec must not break a read.
func readSpecMetas(dir string) ([]markdown.SpecMeta, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading specs directory %q: %w", dir, err)
	}
	var metas []markdown.SpecMeta
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" || !strings.HasPrefix(e.Name(), "SPEC-") {
			continue
		}
		meta, err := markdown.ReadMeta(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		metas = append(metas, *meta)
	}
	return metas, nil
}

// logBountyActivity records a bounty event, best-effort: the activity log is a
// local convenience, while the durable record lives in the spec's frontmatter.
func logBountyActivity(rc *config.ResolvedConfig, specID, eventType, summary, metaJSON string) {
	db, err := openDB()
	if err != nil || db == nil {
		return
	}
	defer func() { _ = db.Close() }()
	_ = db.ActivityLog(specID, eventType, summary, metaJSON, rc.UserName())
}

// stampBountyClaim records the claimant on a spec's active bounty when the
// team has bounties on. It returns whether the claim was recorded and whether
// it was refused as a self-claim, so callers can explain the refusal without
// blocking the assignment itself — assignment and bounty claiming are separate
// facts, and only the second one has an award attached.
func stampBountyClaim(rc *config.ResolvedConfig, meta *markdown.SpecMeta, who string) (recorded, selfClaim bool) {
	if !rc.BountyEnabled() || !meta.BountyActive() {
		return false, false
	}
	if bounty.SelfClaim(meta, who) {
		return false, true
	}
	return bounty.Claim(meta, who, time.Now()), false
}
