package cmd

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// printer centralizes command output so every command writes through the
// command's own streams rather than the global os.Stdout. It honors the
// global --json and --quiet flags:
//
//   - Line/Raw: human-readable stdout, suppressed by --quiet or --json.
//   - Warn:     diagnostics to stderr, suppressed by --quiet (never by --json,
//     since stderr does not pollute machine-readable stdout).
//   - JSON:     machine-readable stdout, emitted only when --json is set.
type printer struct {
	out    io.Writer
	errOut io.Writer
	json   bool
	quiet  bool
}

// newPrinter builds a printer from a command's resolved flags and streams.
func newPrinter(cmd *cobra.Command) *printer {
	jsonOut, _ := cmd.Flags().GetBool("json")
	quiet, _ := cmd.Flags().GetBool("quiet")
	return &printer{
		out:    cmd.OutOrStdout(),
		errOut: cmd.ErrOrStderr(),
		json:   jsonOut,
		quiet:  quiet,
	}
}

// JSONEnabled reports whether machine-readable output was requested.
func (p *printer) JSONEnabled() bool { return p.json }

// Line writes a human-readable line to stdout. Suppressed by --quiet or --json.
func (p *printer) Line(format string, args ...interface{}) {
	if p.quiet || p.json {
		return
	}
	// Output-stream writes are best-effort; a broken pipe is not actionable.
	_, _ = fmt.Fprintf(p.out, format+"\n", args...)
}

// Raw writes pre-formatted text to stdout verbatim (no trailing newline).
// Suppressed by --quiet or --json.
func (p *printer) Raw(s string) {
	if p.quiet || p.json {
		return
	}
	_, _ = fmt.Fprint(p.out, s)
}

// Warn writes a warning to stderr. Suppressed by --quiet so scripted use stays
// clean, but unaffected by --json since warnings live on stderr.
func (p *printer) Warn(format string, args ...interface{}) {
	if p.quiet {
		return
	}
	_, _ = fmt.Fprintf(p.errOut, "warning: "+format+"\n", args...)
}

// JSON emits v as indented JSON to stdout. Only writes when --json is set;
// returns nil otherwise so callers can unconditionally `return p.JSON(res)`.
func (p *printer) JSON(v interface{}) error {
	if !p.json {
		return nil
	}
	enc := json.NewEncoder(p.out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("encoding JSON output: %w", err)
	}
	return nil
}
