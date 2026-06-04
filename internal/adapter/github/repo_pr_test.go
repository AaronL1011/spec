package github

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	gh "github.com/google/go-github/v62/github"
)

// fakeRT is a fake http.RoundTripper capturing the last request and returning a
// canned response, so adapter tests never touch the network.
type fakeRT struct {
	method string
	path   string
	body   string
	status int
	resp   string
}

func (f *fakeRT) RoundTrip(req *http.Request) (*http.Response, error) {
	f.method = req.Method
	f.path = req.URL.Path
	if req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		f.body = string(b)
	}
	status := f.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(f.resp)),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

func clientWith(ft *fakeRT) *RepoClient {
	return &RepoClient{client: gh.NewClient(&http.Client{Transport: ft}), owner: "o"}
}

func TestOpenDraftPR_CreatesDraftWithBase(t *testing.T) {
	ft := &fakeRT{status: http.StatusCreated, resp: `{"number":42,"html_url":"https://github.com/o/r/pull/42"}`}
	c := clientWith(ft)

	num, url, err := c.OpenDraftPR(context.Background(), "r", "spec-1/step-2-child", "spec-1/step-1-root", "Title", "Body")
	if err != nil {
		t.Fatalf("OpenDraftPR: %v", err)
	}
	if num != 42 || url != "https://github.com/o/r/pull/42" {
		t.Errorf("got (%d, %q), want (42, .../pull/42)", num, url)
	}
	if ft.method != http.MethodPost || !strings.Contains(ft.path, "/repos/o/r/pulls") {
		t.Errorf("request = %s %s, want POST /repos/o/r/pulls", ft.method, ft.path)
	}
	for _, want := range []string{`"draft":true`, `"head":"spec-1/step-2-child"`, `"base":"spec-1/step-1-root"`} {
		if !strings.Contains(ft.body, want) {
			t.Errorf("request body %s missing %q", ft.body, want)
		}
	}
}

func TestSetPRBase_RetargetsBase(t *testing.T) {
	ft := &fakeRT{status: http.StatusOK, resp: `{"number":42}`}
	c := clientWith(ft)

	if err := c.SetPRBase(context.Background(), "r", 42, "main"); err != nil {
		t.Fatalf("SetPRBase: %v", err)
	}
	if ft.method != http.MethodPatch || !strings.Contains(ft.path, "/repos/o/r/pulls/42") {
		t.Errorf("request = %s %s, want PATCH /repos/o/r/pulls/42", ft.method, ft.path)
	}
	if !strings.Contains(ft.body, "main") {
		t.Errorf("request body %s should set base to main", ft.body)
	}
}

func TestOpenDraftPR_ErrorIsActionable(t *testing.T) {
	ft := &fakeRT{status: http.StatusForbidden, resp: `{"message":"Resource not accessible"}`}
	c := clientWith(ft)
	_, _, err := c.OpenDraftPR(context.Background(), "r", "h", "b", "t", "")
	if err == nil || !strings.Contains(err.Error(), "Pull Requests write access") {
		t.Fatalf("expected actionable error, got %v", err)
	}
}
