package cmd

import (
	"fmt"

	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/hierarchy"
)

// loadHierarchyIn builds the parent/child graph from a specs content directory.
// Use it inside a WithSpecsRepo mutator, where the just-pulled clone is
// authoritative, passing specsDir(repoPath).
func loadHierarchyIn(baseDir string, rc *config.ResolvedConfig) (*hierarchy.Graph, error) {
	return hierarchy.Load(baseDir, config.ArchiveDir(rc.Team))
}

// loadHierarchy builds the parent/child graph from the local specs clone. It is
// the read-path counterpart of loadHierarchyIn.
func loadHierarchy(rc *config.ResolvedConfig) (*hierarchy.Graph, error) {
	if rc.SpecsRepoDir == "" {
		return nil, fmt.Errorf("specs repo not configured — ensure spec.config.yaml has specs_repo settings")
	}
	return loadHierarchyIn(rc.SpecsRepoDir, rc)
}
