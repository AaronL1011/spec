package thread

import (
	"reflect"
	"testing"
)

func TestParseMentions(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []string
	}{
		{"no mentions", "just a plain question", nil},
		{"single mention", "can @bob take a look?", []string{"bob"}},
		{"multiple mentions", "cc @bob and @carlos", []string{"bob", "carlos"}},
		{"dedup keeps first-seen order", "@bob @carlos @bob", []string{"bob", "carlos"}},
		{"email address ignored", "reach me at user@example.com", nil},
		{"mid-word @ ignored", "the price is 3@5 not a mention", nil},
		{"punctuation-bounded mention", "(cc @bob) thanks", []string{"bob"}},
		{"mention at start of body", "@bob can you check?", []string{"bob"}},
		{"trailing punctuation not part of handle", "ping @bob, thanks", []string{"bob"}},
		{"agent identity with colon is mentionable", "cc @agent:claude for context", []string{"agent:claude"}},
		{"handle with dots and dashes", "@a.b-c_d", []string{"a.b-c_d"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseMentions(tt.body)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseMentions(%q) = %v, want %v", tt.body, got, tt.want)
			}
		})
	}
}

func TestThread_Participants(t *testing.T) {
	th := Thread{
		Author:   "@mike",
		Mentions: []string{"carlos"},
		Replies: []Reply{
			{Author: "@bob", Mentions: []string{"dana"}},
			{Author: "@mike"}, // duplicate author, must not reappear
			{Author: "@bob", Mentions: []string{"carlos"}}, // duplicate author + mention
		},
	}
	got := th.Participants()
	want := []string{"@mike", "carlos", "@bob", "dana"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Participants() = %v, want %v", got, want)
	}
}

// TestThread_Participants_HandleAndAtPrefixDedupe proves "bob" and "@bob"
// count as the same participant: dedup normalises the leading '@' so the
// same person mentioned once with and once without it doesn't double-count.
func TestThread_Participants_HandleAndAtPrefixDedupe(t *testing.T) {
	th := Thread{
		Author:   "@mike",
		Mentions: []string{"bob"},
		Replies:  []Reply{{Author: "@bob"}},
	}
	got := th.Participants()
	want := []string{"@mike", "bob"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Participants() = %v, want %v", got, want)
	}
}

func TestThread_Participants_Empty(t *testing.T) {
	if got := (Thread{}).Participants(); got != nil {
		t.Errorf("Participants() on empty thread = %v, want nil", got)
	}
}
