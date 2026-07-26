package harness

import (
	"fmt"
	"os"
	"path/filepath"
)

// InstallMCPConfig writes an engine-generated MCP config to
// <workDir>/.mcp.json and returns a restore function that puts back any
// pre-existing file (or removes the one we created) when the session ends.
//
// Both pi and Claude Code discover MCP servers via a project-local .mcp.json
// file rather than a CLI flag, so the harness launches with the config already
// on disk. The restore keeps a human's own .mcp.json from being clobbered when
// a spec session lands in the same working directory.
func InstallMCPConfig(mcpConfigPath, workDir string) (func(), error) {
	src, err := os.ReadFile(mcpConfigPath)
	if err != nil {
		return nil, fmt.Errorf("reading mcp config: %w", err)
	}

	dest := filepath.Join(workDir, ".mcp.json")
	prev, prevErr := os.ReadFile(dest)
	hadPrev := prevErr == nil

	if err := os.WriteFile(dest, src, 0o644); err != nil {
		return nil, fmt.Errorf("writing %s: %w", dest, err)
	}

	return func() {
		if hadPrev {
			_ = os.WriteFile(dest, prev, 0o644)
		} else {
			_ = os.Remove(dest)
		}
	}, nil
}
