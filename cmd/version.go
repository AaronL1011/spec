package cmd

import (
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// Version is the spec CLI version. It is normally stamped at build time via
// ldflags (-X github.com/aaronl1011/spec/cmd.Version=...). When the binary is
// produced without ldflags — most commonly via `go install ...@latest` — this
// retains its default and resolveVersion falls back to the module version that
// the Go toolchain embeds in the build info.
var Version = "dev"

// resolveVersion returns the effective version string, preferring an
// ldflags-stamped Version and falling back to the module version recorded in
// the binary's build info. It returns "dev" only when no usable version is
// available (e.g. a plain `go build .` of a local checkout).
func resolveVersion() string {
	if Version != "" && Version != "dev" {
		return Version
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}

	// Module version is empty for `go build` and "(devel)" for builds from a
	// local checkout without a tagged module; neither is a real release.
	v := info.Main.Version
	if v == "" || v == "(devel)" {
		return "dev"
	}
	return v
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the spec CLI version",
	Run: func(cmd *cobra.Command, args []string) {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "spec %s\n", resolveVersion())
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
