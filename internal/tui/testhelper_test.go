package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/aaronl1011/spec/internal/adapter"
	"github.com/aaronl1011/spec/internal/adapter/noop"
	"github.com/aaronl1011/spec/internal/config"
)

// keyMsg creates a printable key press from a string for testing. In Bubble
// Tea v2 typed text travels in KeyPressMsg.Text, and Code carries the first
// rune so String()/matching behave like a real key press.
func keyMsg(s string) tea.KeyPressMsg {
	var code rune
	for _, r := range s {
		code = r
		break
	}
	return tea.KeyPressMsg{Code: code, Text: s}
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
