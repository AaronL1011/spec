// Package update implements `spec update`: it brings the locally installed
// spec binary to the newest released version by delegating to whatever
// mechanism manages the install (Homebrew, go install, or a raw release
// binary it self-replaces). The engine is split into a side-effect-free Plan
// phase and a mutating Apply phase so `--check` never touches the system.
package update

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// Default repository and packaging coordinates for the spec CLI. They are
// fields on Options (with these defaults applied by NewUpdater) so tests can
// override them.
const (
	defaultOwner      = "aaronl1011"
	defaultRepo       = "spec"
	defaultModulePath = "github.com/aaronl1011/spec"
	defaultFormula    = "aaronl1011/tap/spec"
)

// Options configures an update run.
type Options struct {
	// CurrentVersion is the version stamped into the running binary.
	CurrentVersion string
	// ExecPath is the path to the running executable (os.Executable()).
	ExecPath string
	// TargetVersion pins a specific release tag; empty means latest.
	TargetVersion string
	// Force proceeds even when already on the target version.
	Force bool
}

// Updater orchestrates an update run against a release source.
type Updater struct {
	source     releaseSource
	modulePath string
	formula    string
}

// NewUpdater builds an Updater backed by the live GitHub release API.
func NewUpdater(token string) *Updater {
	return &Updater{
		source:     newGitHubReleases(defaultOwner, defaultRepo, token),
		modulePath: defaultModulePath,
		formula:    defaultFormula,
	}
}

// Plan describes the intended update without performing it. It is produced by
// Plan and consumed by Apply, and is also the payload for `--check`/`--json`.
type Plan struct {
	Method          Method `json:"method"`
	CurrentVersion  string `json:"current_version"`
	LatestVersion   string `json:"latest_version"`
	UpdateAvailable bool   `json:"update_available"`
	// release is retained for Apply's self-replace path; not serialised.
	release Release
	opts    Options
}

// Plan resolves the target release and managing mechanism and compares it
// against the running version. It performs no mutations.
func (u *Updater) Plan(ctx context.Context, opts Options) (*Plan, error) {
	rel, err := u.resolveRelease(ctx, opts.TargetVersion)
	if err != nil {
		return nil, err
	}
	return &Plan{
		Method:          DetectMethod(opts.ExecPath),
		CurrentVersion:  opts.CurrentVersion,
		LatestVersion:   rel.Tag,
		UpdateAvailable: updateAvailable(opts.CurrentVersion, rel.Tag),
		release:         rel,
		opts:            opts,
	}, nil
}

// resolveRelease fetches the pinned tag or the latest release.
func (u *Updater) resolveRelease(ctx context.Context, tag string) (Release, error) {
	if tag != "" {
		return u.source.ByTag(ctx, tag)
	}
	return u.source.Latest(ctx)
}

// updateAvailable reports whether target is newer than current. Dev builds and
// unparseable versions always report true so the user can move to a real
// release; a pinned target that does not parse still counts as available.
func updateAvailable(current, target string) bool {
	if IsDev(current) {
		return true
	}
	cmp, err := compareVersions(current, target)
	if err != nil {
		return true
	}
	return cmp < 0
}

// Apply performs the update described by plan, streaming any subprocess output
// to stdout/stderr. It is a no-op (nil error) when no update is available and
// Force was not set. The caller is responsible for any confirmation prompt.
func (u *Updater) Apply(ctx context.Context, plan *Plan, stdout, stderr io.Writer) error {
	if !plan.UpdateAvailable && !plan.opts.Force {
		return nil
	}
	switch plan.Method {
	case MethodHomebrew:
		return u.run(ctx, stdout, stderr, "brew", "upgrade", u.formula)
	case MethodGoInstall:
		return u.run(ctx, stdout, stderr, "go", "install", u.moduleRef(plan))
	case MethodBinary:
		return replaceBinary(ctx, plan.release, plan.LatestVersion, plan.opts.ExecPath)
	default:
		return fmt.Errorf("unknown install method %q", plan.Method)
	}
}

// moduleRef builds the `go install` module reference. It pins the exact tag
// the Plan already resolved rather than re-resolving @latest at install time:
// the Go module proxy resolves @latest independently of the GitHub release API
// used by Plan and can lag behind it, which would install (and silently report)
// a different version than the one we told the user we were fetching. An
// explicitly requested target wins; @latest is only a last-resort fallback when
// no concrete tag is known.
func (u *Updater) moduleRef(plan *Plan) string {
	ref := plan.opts.TargetVersion
	if ref == "" {
		ref = plan.LatestVersion
	}
	if ref == "" {
		ref = "latest"
	}
	return u.modulePath + "@" + ref
}

// run executes a delegated package-manager command, surfacing a clear error
// when the tool is not installed.
func (u *Updater) run(ctx context.Context, stdout, stderr io.Writer, name string, args ...string) error {
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("%s is not installed but manages this install — install %s or update manually: %w", name, name, err)
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %v failed: %w", name, args, err)
	}
	return nil
}
