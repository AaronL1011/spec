package cmd

import (
	"reflect"
	"testing"
)

func TestIsAgentHandle(t *testing.T) {
	tests := []struct {
		handle string
		want   bool
	}{
		{"agent", true},
		{"@agent", true},
		{"AGENT", true},
		{"agent:claude", true},
		{"@agent:claude", true},
		{"agent:pi", true},
		{"@bob", false},
		{"agentx", false}, // not a recognised agent identity, just a handle that starts similarly
	}
	for _, tt := range tests {
		if got := isAgentHandle(tt.handle); got != tt.want {
			t.Errorf("isAgentHandle(%q) = %v, want %v", tt.handle, got, tt.want)
		}
	}
}

// TestExcludeIdentity_DropsActingUserRegardlessOfHandleForm proves the
// "answer notifies participants minus the replier" and "resolve notifies
// participants minus the resolver" rules (discussion-01 §3.2) hold even when
// Participants() records the acting user under a different handle/name form
// than threadAuthor(rc) returns.
func TestExcludeIdentity_DropsActingUserRegardlessOfHandleForm(t *testing.T) {
	tests := []struct {
		name    string
		handles []string
		self    string
		want    []string
	}{
		{"exact match dropped", []string{"@mike", "@bob"}, "@mike", []string{"@bob"}},
		{"case-insensitive match dropped", []string{"@Mike", "@bob"}, "@mike", []string{"@bob"}},
		{"at-prefix tolerated", []string{"mike", "@bob"}, "@mike", []string{"@bob"}},
		{"no match keeps everyone", []string{"@bob", "@carlos"}, "@mike", []string{"@bob", "@carlos"}},
		{"empty handles", nil, "@mike", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := excludeIdentity(tt.handles, tt.self)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("excludeIdentity(%v, %q) = %v, want %v", tt.handles, tt.self, got, tt.want)
			}
		})
	}
}
