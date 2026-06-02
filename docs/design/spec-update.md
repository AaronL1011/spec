# Design: `spec update` (self-update)

## Goal

A single command, `spec update`, that brings the locally installed `spec`
binary to the newest released version by **delegating to whatever mechanism
manages the install on the user's machine**:

- Installed via Homebrew → shell out to `brew upgrade aaronl1011/tap/spec`.
- Installed via `go install` → shell out to `go install <module>@latest`.
- Installed as a raw release binary (no package manager) → download the latest
  GitHub release archive for the current OS/arch, verify its SHA-256 against
  `checksums.txt`, and atomically swap the running executable.

This is delivered as one cohesive change package, not a stack of PRs.

## Command surface

```
spec update [--check] [--force] [--yes] [--version vX.Y.Z]
```

| Flag         | Behaviour                                                            |
|--------------|----------------------------------------------------------------------|
| `--check`    | Report current vs latest and the managing mechanism; never mutate.   |
| `--version`  | Target a specific tag instead of latest (reinstall/downgrade).       |
| `--force`    | Proceed even when already on the latest version.                     |
| `--yes`      | Skip the interactive confirmation (CI/scripts).                      |
| `--json`     | (global) machine-readable plan/outcome.                              |
| `--quiet`    | (global) suppress human output.                                      |

The command works without team config — it is a maintenance command. A GitHub
token is optional and only used to lift the anonymous API rate limit; it is read
best-effort from `SPEC_GITHUB_TOKEN` / `GITHUB_TOKEN`. Public releases need no
auth.

## Architecture

`cmd/update.go` stays thin: parse flags, resolve the executable path and current
version, build the `update.Updater`, call `Plan`, prompt for confirmation, then
call `Apply`. All logic lives in `internal/update/`.

```
internal/update/
  update.go    Updater, Options, Plan, Outcome; Plan() + Apply() orchestration
  method.go    Method enum + DetectMethod(execPath) — install-method detection
  release.go   Release/Asset types, releaseSource interface, GitHub HTTP fetcher
  semver.go    version normalisation + compare (-1/0/1), handles "dev"
  binary.go    raw-binary path: download, checksum verify, extract, atomic swap
```

### Two-phase API (read vs mutate)

- `Plan(ctx) (*Plan, error)` — fetches the target release, detects the managing
  mechanism, compares versions. No side effects. Backs `--check`.
- `Apply(ctx, *Plan, stdout, stderr) (*Outcome, error)` — performs the update:
  shells out to brew/go, or self-replaces the binary.

This separation keeps `--check` side-effect-free and makes both phases testable.

### Install-method detection (`method.go`)

Resolve `os.Executable()` through `filepath.EvalSymlinks`, then classify:

- **Homebrew** — resolved path contains `/Cellar/` (covers macOS `/opt/homebrew`,
  `/usr/local`, and Linuxbrew `/home/linuxbrew/.linuxbrew`).
- **GoInstall** — directory equals `go env GOBIN`, `$GOPATH/bin`, or `$HOME/go/bin`.
- **Binary** — anything else; the self-replace path.

### Raw-binary self-replace (`binary.go`)

1. Pick the asset matching `spec_<version>_<goos>_<goarch>.{tar.gz|zip}`.
2. Download to a temp file (30s timeout).
3. Download `checksums.txt`; verify SHA-256 — **abort on mismatch**.
4. Extract the `spec` binary from the archive.
5. Write `spec.new` next to the target, `chmod 0755`, run `spec.new version` to
   sanity-check, then `os.Rename` over the target (atomic, same filesystem).
   On Windows, move the running exe to `spec.old` first.
6. On `EACCES`, surface a clear message with the manual/`sudo` next step rather
   than panicking.

## Robustness

- API call timeout 10s; download timeout 30s (per AGENTS.md network defaults).
- Network/no-release failures return actionable errors and degrade cleanly; they
  never panic.
- Checksum mismatch is fatal — never install an unverified binary.
- Atomic rename means a crash mid-update never leaves a half-written binary.

## Testing

- `method_test.go` — table-driven path fixtures (`t.TempDir()`) for each method.
- `semver_test.go` — comparison matrix incl. `dev`, `v`-prefix, pre-release.
- `release_test.go` — `releaseSource` test double; asset selection by os/arch.
- `binary_test.go` — fake archive + `checksums.txt`; verify mismatch aborts and
  a good archive swaps the file atomically.

## Companion fix

`Makefile` ldflags inject into `github.com/aaronl1011/spec-cli/cmd.Version`, but
the module is `github.com/aaronl1011/spec`, so `make install`/`make build` never
stamp the version (it stays `dev`). `spec update` depends on an accurate
installed version, so this is corrected to `github.com/aaronl1011/spec/cmd.Version`
in the same change package.
