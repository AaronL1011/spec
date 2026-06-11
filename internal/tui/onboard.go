package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"charm.land/huh/v2"

	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/onboard"
)

// OnboardResult reports the outcome of the first-run wizard.
type OnboardResult struct {
	// Completed is true when the user finished the wizard with both a user
	// identity and a joined team, so the caller can re-resolve config and land
	// on the live dashboard without a restart.
	Completed bool
	// WroteUserConfig is true when a new user identity was written.
	WroteUserConfig bool
	// JoinedTeam is true when a specs repo was successfully joined.
	JoinedTeam bool
}

// ErrOnboardCancelled signals the user backed out of the wizard (esc/ctrl-c).
var ErrOnboardCancelled = errors.New("onboarding cancelled")

// RunOnboarding hosts the first-run wizard in the terminal: identity →
// join-or-create team. It reuses the existing onboard.Join flow and config
// writers; it adds no new flow logic of its own (SPEC-027 §7.2). On success it
// reports what was configured so the caller can re-resolve and open the
// dashboard. A user who chooses "create" or backs out is handed to
// `spec config init`, which stays the advanced-setup path.
//
// hasUser indicates whether a user identity already exists, so a returning user
// with no team config skips straight to the join step.
func RunOnboarding(ctx context.Context, hasUser bool) (OnboardResult, error) {
	if !IsInteractive() {
		return OnboardResult{}, fmt.Errorf("not an interactive terminal — run 'spec config init' to set up")
	}

	var res OnboardResult

	if !hasUser {
		wrote, err := runIdentityStep()
		if err != nil {
			return res, err
		}
		res.WroteUserConfig = wrote
	}

	joined, err := runJoinStep(ctx)
	if err != nil {
		return res, err
	}
	res.JoinedTeam = joined
	res.Completed = joined
	return res, nil
}

// runIdentityStep collects name, role, and handle and writes the user config.
// It is step 1 of the wizard (§5.1: identity).
func runIdentityStep() (bool, error) {
	var name, role, handle string

	roleOptions := make([]huh.Option[string], len(config.ValidRoles()))
	for i, r := range config.ValidRoles() {
		roleOptions[i] = huh.NewOption(r, r)
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("Welcome to spec").
				Description("Step 1 of 2 — set up your identity."),
			huh.NewInput().
				Title("Your name").
				Placeholder("Ada Lovelace").
				Validate(huh.ValidateNotEmpty()).
				Value(&name),
			huh.NewSelect[string]().
				Title("Your role").
				Options(roleOptions...).
				Value(&role),
			huh.NewInput().
				Title("Your comms handle").
				Description("e.g. @ada or ada@org.com (optional)").
				Value(&handle),
		),
	)

	if err := runForm(form); err != nil {
		return false, err
	}

	if err := writeUserIdentity(name, role, handle); err != nil {
		return false, err
	}
	return true, nil
}

// writeUserIdentity persists a user config from the collected identity fields.
// Split out from the form step so the persistence path is unit-testable
// without a TTY.
func writeUserIdentity(name, role, handle string) error {
	cfg := &config.UserConfig{}
	cfg.User.Name = strings.TrimSpace(name)
	cfg.User.OwnerRole = role
	cfg.User.Handle = strings.TrimSpace(handle)
	aiDrafts := true
	cfg.Preferences.AIDrafts = &aiDrafts

	return config.WriteUserConfig(config.UserConfigPath(), cfg)
}

// runJoinStep is step 2 (§5.1: join-or-create). Joining clones a specs repo via
// the existing onboard.Join; creating hands off to `spec config init`.
func runJoinStep(ctx context.Context) (bool, error) {
	const (
		choiceJoin   = "join"
		choiceCreate = "create"
	)

	var choice string
	chooser := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("Join or create a team").
				Description("Step 2 of 2 — connect to your team's specs repo."),
			huh.NewSelect[string]().
				Title("How do you want to start?").
				Options(
					huh.NewOption("Join an existing specs repo", choiceJoin),
					huh.NewOption("Create a new team (advanced setup)", choiceCreate),
				).
				Value(&choice),
		),
	)
	if err := runForm(chooser); err != nil {
		return false, err
	}

	if choice == choiceCreate {
		// Creating a team is the advanced path: hand off to `spec config init`
		// rather than duplicating the full team-config wizard here (§7.2).
		return false, nil
	}

	var repoRef, branch, token string
	branch = "main"
	joinForm := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Specs repo").
				Description("org/repo, or a full github.com/org/repo URL").
				Placeholder("acme/specs").
				Validate(validateRepoRef).
				Value(&repoRef),
			huh.NewInput().
				Title("Branch").
				Value(&branch),
			huh.NewInput().
				Title("Access token").
				Description("Leave blank to use $SPEC_GITHUB_TOKEN").
				EchoMode(huh.EchoModePassword).
				Value(&token),
		),
	)
	if err := runForm(joinForm); err != nil {
		return false, err
	}
	if strings.TrimSpace(branch) == "" {
		branch = "main"
	}

	if err := onboard.Join(ctx, strings.TrimSpace(repoRef), branch, strings.TrimSpace(token)); err != nil {
		return false, fmt.Errorf("joining team: %w", err)
	}
	return true, nil
}

// validateRepoRef checks a repo reference parses before the clone is attempted.
func validateRepoRef(s string) error {
	if strings.TrimSpace(s) == "" {
		return fmt.Errorf("a specs repo reference is required")
	}
	if _, _, _, err := onboard.ParseRepoRef(strings.TrimSpace(s)); err != nil {
		return err
	}
	return nil
}

// runForm runs a huh form, mapping the user-abort error to ErrOnboardCancelled
// so callers can distinguish a deliberate back-out from a real failure.
func runForm(form *huh.Form) error {
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return ErrOnboardCancelled
		}
		return err
	}
	return nil
}
