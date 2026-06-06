package tui

import (
	"testing"

	"github.com/aaronl1011/spec/internal/config"
)

func rcWithRole(role string) *config.ResolvedConfig {
	rc := testResolvedConfig()
	if rc.User == nil {
		rc.User = &config.UserConfig{}
	}
	rc.User.User.OwnerRole = role
	return rc
}

func TestRoleAllowed_ViewAndComment(t *testing.T) {
	for _, role := range []string{"", "pm", "engineer", "designer", "qa"} {
		rc := rcWithRole(role)
		if !roleAllowed(actionTriageView, rc) {
			t.Errorf("role %q: view should be allowed for all", role)
		}
		if !roleAllowed(actionTriageComment, rc) {
			t.Errorf("role %q: comment should be allowed for all", role)
		}
	}
}

func TestRoleAllowed_EditCloseEscalate(t *testing.T) {
	allowed := []string{"pm", "engineer"}
	denied := []string{"", "designer", "qa", "tl"}
	actions := []triageAction{actionTriageEdit, actionTriageClose, actionTriageEscalate}

	for _, action := range actions {
		for _, role := range allowed {
			if !roleAllowed(action, rcWithRole(role)) {
				t.Errorf("role %q: action %q should be allowed", role, action)
			}
		}
		for _, role := range denied {
			if roleAllowed(action, rcWithRole(role)) {
				t.Errorf("role %q: action %q should be denied", role, action)
			}
		}
	}
}

func TestRoleAllowed_Promote(t *testing.T) {
	allowed := []string{"pm"}
	denied := []string{"", "engineer", "designer", "qa"}

	for _, role := range allowed {
		if !roleAllowed(actionTriagePromote, rcWithRole(role)) {
			t.Errorf("role %q: promote should be allowed", role)
		}
	}
	for _, role := range denied {
		if roleAllowed(actionTriagePromote, rcWithRole(role)) {
			t.Errorf("role %q: promote should be denied", role)
		}
	}
}

func TestRoleAllowed_NilRC(t *testing.T) {
	// Nil config should default to read-only.
	if !roleAllowed(actionTriageView, nil) {
		t.Error("nil rc: view should be allowed")
	}
	if roleAllowed(actionTriageEdit, nil) {
		t.Error("nil rc: edit should be denied")
	}
	if roleAllowed(actionTriagePromote, nil) {
		t.Error("nil rc: promote should be denied")
	}
}
