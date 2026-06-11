package noop

import (
	"context"
	"testing"
	"time"

	"github.com/aaronl1011/spec/internal/adapter"
)

// The noop adapters back every unconfigured integration. Their contract is
// strict: every method returns empty/zero values and a nil error, and none
// ever panics or blocks (AGENTS.md adapter-isolation rules). These tests pin
// that contract across all categories.

func TestComms_AllNoops(t *testing.T) {
	ctx := context.Background()
	var c Comms
	if err := c.Notify(ctx, adapter.Notification{SpecID: "SPEC-1"}); err != nil {
		t.Errorf("Notify = %v, want nil", err)
	}
	if err := c.PostStandup(ctx, adapter.StandupReport{}); err != nil {
		t.Errorf("PostStandup = %v, want nil", err)
	}
	mentions, err := c.FetchMentions(ctx, time.Now())
	if mentions != nil || err != nil {
		t.Errorf("FetchMentions = (%v, %v), want (nil, nil)", mentions, err)
	}
}

func TestPM_AllNoops(t *testing.T) {
	ctx := context.Background()
	var p PM
	if epic, err := p.FindEpic(ctx, "SPEC-1"); epic != "" || err != nil {
		t.Errorf("FindEpic = (%q, %v), want (\"\", nil)", epic, err)
	}
	if epic, err := p.CreateEpic(ctx, adapter.SpecMeta{}); epic != "" || err != nil {
		t.Errorf("CreateEpic = (%q, %v), want (\"\", nil)", epic, err)
	}
	if err := p.LinkEpic(ctx, "E-1", "SPEC-1", "url"); err != nil {
		t.Errorf("LinkEpic = %v, want nil", err)
	}
	if err := p.UpdateStatus(ctx, "E-1", "done"); err != nil {
		t.Errorf("UpdateStatus = %v, want nil", err)
	}
	if upd, err := p.FetchUpdates(ctx, "E-1"); upd != nil || err != nil {
		t.Errorf("FetchUpdates = (%v, %v), want (nil, nil)", upd, err)
	}
	if links, err := p.SyncStories(ctx, "E-1", nil); links != nil || err != nil {
		t.Errorf("SyncStories = (%v, %v), want (nil, nil)", links, err)
	}
	if err := p.Validate(ctx); err != nil {
		t.Errorf("Validate = %v, want nil", err)
	}
}

func TestDocs_AllNoops(t *testing.T) {
	ctx := context.Background()
	var d Docs
	if secs, err := d.FetchSections(ctx, "SPEC-1"); secs != nil || err != nil {
		t.Errorf("FetchSections = (%v, %v), want (nil, nil)", secs, err)
	}
	if err := d.PushFull(ctx, "SPEC-1", "content"); err != nil {
		t.Errorf("PushFull = %v, want nil", err)
	}
	if url, err := d.PageURL(ctx, "SPEC-1"); url != "" || err != nil {
		t.Errorf("PageURL = (%q, %v), want (\"\", nil)", url, err)
	}
}

func TestRepo_AllNoops(t *testing.T) {
	ctx := context.Background()
	var r Repo
	if prs, err := r.ListPRs(ctx, []string{"repo"}, "SPEC-1"); prs != nil || err != nil {
		t.Errorf("ListPRs = (%v, %v), want (nil, nil)", prs, err)
	}
	if detail, err := r.PRStatus(ctx, "repo", 1); detail != nil || err != nil {
		t.Errorf("PRStatus = (%v, %v), want (nil, nil)", detail, err)
	}
	if err := r.SetPRDescription(ctx, "repo", 1, "body"); err != nil {
		t.Errorf("SetPRDescription = %v, want nil", err)
	}
	if prs, err := r.RequestedReviews(ctx, "user"); prs != nil || err != nil {
		t.Errorf("RequestedReviews = (%v, %v), want (nil, nil)", prs, err)
	}
}

func TestAgent_AllNoops(t *testing.T) {
	ctx := context.Background()
	var a Agent
	res, err := a.Invoke(ctx, adapter.InvokeRequest{})
	if err != nil {
		t.Errorf("Invoke err = %v, want nil", err)
	}
	if res == nil {
		t.Error("Invoke result = nil, want a non-nil empty result")
	}
	caps := a.Capabilities()
	if caps.MCP || caps.SystemPrompt {
		t.Errorf("Capabilities = %+v, want all false", caps)
	}
}

func TestDeploy_AllNoops(t *testing.T) {
	ctx := context.Background()
	var d Deploy
	if run, err := d.Trigger(ctx, []string{"repo"}, "prod"); run != nil || err != nil {
		t.Errorf("Trigger = (%v, %v), want (nil, nil)", run, err)
	}
	if st, err := d.Status(ctx, nil); st != nil || err != nil {
		t.Errorf("Status = (%v, %v), want (nil, nil)", st, err)
	}
}

func TestAI_AllNoops(t *testing.T) {
	out, err := AI{}.Complete(context.Background(), "prompt", "system")
	if out != "" || err != nil {
		t.Errorf("Complete = (%q, %v), want (\"\", nil)", out, err)
	}
}

// TestNoopsImplementInterfaces is a compile-time assertion that every noop
// satisfies its adapter interface — a broken signature fails the build here.
func TestNoopsImplementInterfaces(t *testing.T) {
	var (
		_ adapter.CommsAdapter  = Comms{}
		_ adapter.PMAdapter     = PM{}
		_ adapter.DocsAdapter   = Docs{}
		_ adapter.RepoAdapter   = Repo{}
		_ adapter.AgentAdapter  = Agent{}
		_ adapter.DeployAdapter = Deploy{}
		_ adapter.AIAdapter     = AI{}
	)
}
