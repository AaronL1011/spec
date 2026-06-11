package slack

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aaronl1011/spec/internal/adapter"
	slackapi "github.com/slack-go/slack"
)

// slackTestClient returns a Client whose underlying slack-go API is pointed at
// the given httptest server, so contract tests never touch the network.
func slackTestClient(t *testing.T, serverURL, defaultCh, standupCh string) *Client {
	t.Helper()
	api := slackapi.New("xoxb-test", slackapi.OptionAPIURL(serverURL+"/"))
	if standupCh == "" {
		standupCh = defaultCh
	}
	return &Client{
		api:            api,
		defaultChannel: normaliseChannel(defaultCh),
		standupChannel: normaliseChannel(standupCh),
	}
}

// okServer replays a fixed Slack JSON body for every request.
func okServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestNotify_PostsSuccessfully asserts the happy path posts without error.
func TestNotify_PostsSuccessfully(t *testing.T) {
	srv := okServer(t, `{"ok":true,"channel":"platform","ts":"123.456"}`)
	c := slackTestClient(t, srv.URL, "#platform", "")

	err := c.Notify(context.Background(), adapter.Notification{
		SpecID: "SPEC-001", Title: "Title", Message: "Body", Mention: "@dev",
	})
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}
}

// TestNotify_APIError_ReturnsError asserts a Slack API error is surfaced as an
// error (failure isolation contract — never a panic).
func TestNotify_APIError_ReturnsError(t *testing.T) {
	srv := okServer(t, `{"ok":false,"error":"channel_not_found"}`)
	c := slackTestClient(t, srv.URL, "#nope", "")

	err := c.Notify(context.Background(), adapter.Notification{SpecID: "SPEC-001", Message: "x"})
	if err == nil {
		t.Fatal("expected an error when Slack rejects the post")
	}
}

// TestPostStandup_PostsSuccessfully asserts the standup happy path.
func TestPostStandup_PostsSuccessfully(t *testing.T) {
	srv := okServer(t, `{"ok":true,"channel":"standup","ts":"1.2"}`)
	c := slackTestClient(t, srv.URL, "#platform", "#standup")

	err := c.PostStandup(context.Background(), adapter.StandupReport{
		UserName: "Dev", Date: "2026-01-01",
		Yesterday: []string{"did a thing"},
		Today:     []string{"do another"},
		Blockers:  []string{"none"},
	})
	if err != nil {
		t.Fatalf("PostStandup: %v", err)
	}
}

// TestPostStandup_APIError_ReturnsError asserts standup failures isolate.
func TestPostStandup_APIError_ReturnsError(t *testing.T) {
	srv := okServer(t, `{"ok":false,"error":"not_in_channel"}`)
	c := slackTestClient(t, srv.URL, "#platform", "#standup")

	err := c.PostStandup(context.Background(), adapter.StandupReport{UserName: "Dev", Date: "2026-01-01"})
	if err == nil {
		t.Fatal("expected an error when Slack rejects the standup post")
	}
}

// TestFetchMentions_DegradesGracefully asserts that a search failure (free
// plan / missing scope) returns (nil, nil) rather than an error — mentions are
// best-effort.
func TestFetchMentions_DegradesGracefully(t *testing.T) {
	srv := okServer(t, `{"ok":false,"error":"missing_scope"}`)
	c := slackTestClient(t, srv.URL, "#platform", "")

	mentions, err := c.FetchMentions(context.Background(), time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Errorf("FetchMentions should degrade to (nil,nil), got err: %v", err)
	}
	if mentions != nil {
		t.Errorf("FetchMentions = %v, want nil on degraded search", mentions)
	}
}

// TestFetchMentions_ParsesMatches asserts a successful search extracts spec IDs.
func TestFetchMentions_ParsesMatches(t *testing.T) {
	body := `{"ok":true,"messages":{"matches":[
		{"text":"Please look at SPEC-042 today","username":"alice","ts":"1700000000.000100","channel":{"name":"platform"}},
		{"text":"no spec here","username":"bob","ts":"1700000001.000100","channel":{"name":"platform"}}
	],"total":2}}`
	srv := okServer(t, body)
	c := slackTestClient(t, srv.URL, "#platform", "")

	mentions, err := c.FetchMentions(context.Background(), time.Unix(1699999999, 0))
	if err != nil {
		t.Fatalf("FetchMentions: %v", err)
	}
	if len(mentions) != 1 {
		t.Fatalf("got %d mentions, want 1 (only the SPEC-042 message)", len(mentions))
	}
	if mentions[0].SpecID != "SPEC-042" || mentions[0].Author != "alice" {
		t.Errorf("mention = %+v, want SPEC-042 by alice", mentions[0])
	}
}

func TestParseSlackTimestamp(t *testing.T) {
	ts := parseSlackTimestamp("1700000000.000100")
	if ts.Unix() != 1700000000 {
		t.Errorf("parseSlackTimestamp = %d, want 1700000000", ts.Unix())
	}
	// Malformed input should not panic; it returns a zero-ish time.
	_ = parseSlackTimestamp("")
}

func TestNewClient_DefaultsStandupToDefaultChannel(t *testing.T) {
	c := NewClient("xoxb-test", "#platform", "")
	if c.standupChannel != "platform" {
		t.Errorf("standupChannel = %q, want it to default to the default channel", c.standupChannel)
	}
}
