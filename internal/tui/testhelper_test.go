package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/aaronl1011/spec/internal/adapter"
	"github.com/aaronl1011/spec/internal/adapter/noop"
	"github.com/aaronl1011/spec/internal/config"
)

// keyMsg creates a tea.KeyMsg from a string for testing.
func keyMsg(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// testResolvedConfig creates a minimal ResolvedConfig for testing.
func testResolvedConfig() *config.ResolvedConfig {
	rc := &config.ResolvedConfig{
		User: &config.UserConfig{},
		Team: &config.TeamConfig{},
	}
	rc.User.User.Name = "Test"
	rc.User.User.OwnerRole = "engineer"
	return rc
}

// testRegistry creates a noop adapter registry for testing.
func testRegistry() *adapter.Registry {
	reg := adapter.NewRegistry(nil)
	reg.WithComms(noop.Comms{}).
		WithPM(noop.PM{}).
		WithDocs(noop.Docs{}).
		WithRepo(noop.Repo{}).
		WithAgent(noop.Agent{}).
		WithDeploy(noop.Deploy{}).
		WithAI(noop.AI{})
	return reg
}
