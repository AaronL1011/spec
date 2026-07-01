package slack

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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

// usersServer replays a fixed users.list JSON body, keyed by request path, and
// a canned success for conversations.open and chat.postMessage — their
// response shapes differ (channel is an object vs. a plain ID string) so each
// needs its own body. Together this exercises NotifyUser's three-call flow
// (list users, open DM, post message) without touching the network.
func usersServer(t *testing.T, usersBody string) *httptest.Server {
	t.Helper()
	return multiEndpointServer(t, map[string]string{
		"users.list":         usersBody,
		"conversations.open": `{"ok":true,"channel":{"id":"D1"}}`,
		"chat.postMessage":   `{"ok":true,"channel":"D1","ts":"1.2"}`,
	})
}

// multiEndpointServer replies with bodies[method] keyed by the trailing path
// segment of the Slack API method the client called (e.g. "users.list").
func multiEndpointServer(t *testing.T, bodies map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		for method, body := range bodies {
			if strings.HasSuffix(r.URL.Path, method) {
				_, _ = w.Write([]byte(body))
				return
			}
		}
		_, _ = w.Write([]byte(`{"ok":false,"error":"unhandled_in_test"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestNotifyUser_ResolvesHandleAndPostsDM asserts the happy path: a handle
// resolves against the workspace user list, a DM channel opens, the message
// posts.
func TestNotifyUser_ResolvesHandleAndPostsDM(t *testing.T) {
	users := `{"ok":true,"members":[
		{"id":"U1","name":"bob","profile":{"display_name":"Bob Smith"}},
		{"id":"U2","name":"carlos","profile":{"display_name":"Carlos Diaz"}}
	],"response_metadata":{"next_cursor":""}}`
	srv := usersServer(t, users)
	c := slackTestClient(t, srv.URL, "#platform", "")

	if err := c.NotifyUser(context.Background(), "@bob", adapter.Notification{SpecID: "SPEC-1", Message: "hi"}); err != nil {
		t.Fatalf("NotifyUser: %v", err)
	}
}

// TestNotifyUser_MatchesByDisplayName proves the display name is also a
// valid match, not just the Slack username.
func TestNotifyUser_MatchesByDisplayName(t *testing.T) {
	users := `{"ok":true,"members":[
		{"id":"U1","name":"bsmith","profile":{"display_name":"Bob Smith"}}
	],"response_metadata":{"next_cursor":""}}`
	srv := usersServer(t, users)
	c := slackTestClient(t, srv.URL, "#platform", "")

	if err := c.NotifyUser(context.Background(), "Bob Smith", adapter.Notification{SpecID: "SPEC-1"}); err != nil {
		t.Fatalf("NotifyUser: %v", err)
	}
}

// TestNotifyUser_UnknownHandle_ReturnsErrRecipientUnknown proves an
// unresolvable handle degrades to the documented sentinel, not a generic
// error, so callers can fall back to a channel broadcast.
func TestNotifyUser_UnknownHandle_ReturnsErrRecipientUnknown(t *testing.T) {
	users := `{"ok":true,"members":[{"id":"U1","name":"bob"}],"response_metadata":{"next_cursor":""}}`
	srv := usersServer(t, users)
	c := slackTestClient(t, srv.URL, "#platform", "")

	err := c.NotifyUser(context.Background(), "@nobody", adapter.Notification{SpecID: "SPEC-1"})
	if !errors.Is(err, adapter.ErrRecipientUnknown) {
		t.Fatalf("NotifyUser(unknown) = %v, want ErrRecipientUnknown", err)
	}
}

// TestNotifyUser_UserListCachedAcrossCalls proves the workspace user list is
// fetched once per Client, not once per NotifyUser call — routing a thread
// notification to several recipients should cost one users.list call.
func TestNotifyUser_UserListCachedAcrossCalls(t *testing.T) {
	var listCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "users.list"):
			listCalls++
			_, _ = w.Write([]byte(`{"ok":true,"members":[{"id":"U1","name":"bob"},{"id":"U2","name":"carlos"}],"response_metadata":{"next_cursor":""}}`))
		case strings.HasSuffix(r.URL.Path, "conversations.open"):
			_, _ = w.Write([]byte(`{"ok":true,"channel":{"id":"D1"}}`))
		default:
			_, _ = w.Write([]byte(`{"ok":true,"channel":"D1","ts":"1.2"}`))
		}
	}))
	t.Cleanup(srv.Close)
	c := slackTestClient(t, srv.URL, "#platform", "")

	if err := c.NotifyUser(context.Background(), "@bob", adapter.Notification{SpecID: "SPEC-1"}); err != nil {
		t.Fatalf("first NotifyUser: %v", err)
	}
	if err := c.NotifyUser(context.Background(), "@carlos", adapter.Notification{SpecID: "SPEC-1"}); err != nil {
		t.Fatalf("second NotifyUser: %v", err)
	}
	if listCalls != 1 {
		t.Errorf("users.list called %d times, want 1 (cached)", listCalls)
	}
}
