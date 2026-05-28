package workflow

import (
	"context"
	"fmt"

	"github.com/aaronl1011/spec/internal/adapter"
	"github.com/aaronl1011/spec/internal/pipeline"
)

// EjectInput describes a request to block (eject) a spec.
type EjectInput struct {
	SpecID   string
	SpecPath string
	Reason   string
}

// EjectResult is the render-ready outcome of an eject.
type EjectResult struct {
	SpecID        string `json:"spec_id"`
	PreviousStage string `json:"previous_stage"`
	Reason        string `json:"reason"`

	CommitMsg string `json:"-"`
}

// Eject transitions a spec to blocked status, records a blocker, notifies the
// team (best-effort), and logs activity. Returns an error if already blocked.
func Eject(ctx context.Context, d Deps, in EjectInput) (*EjectResult, error) {
	ctx = ensureCtx(ctx)

	meta, err := readMeta(in.SpecPath)
	if err != nil {
		return nil, err
	}
	if meta.Status == pipeline.StatusBlocked {
		return nil, fmt.Errorf("%s is already blocked — use 'spec resume %s' to unblock", in.SpecID, in.SpecID)
	}

	result, err := pipeline.Eject(in.SpecPath, meta, in.Reason, d.user())
	if err != nil {
		return nil, err
	}

	res := &EjectResult{
		SpecID:        in.SpecID,
		PreviousStage: result.PreviousStage,
		Reason:        in.Reason,
	}

	if d.Config.HasIntegration("comms") {
		msg := fmt.Sprintf("🚫 [%s] BLOCKED from %s | Reason: %s | By: %s", in.SpecID, result.PreviousStage, in.Reason, d.user())
		if err := d.Registry.Comms().Notify(ctx, adapter.Notification{SpecID: in.SpecID, Title: meta.Title, Message: msg}); err != nil {
			// Notification failure is non-fatal; surfaced via activity only.
			_ = err
		}
	}

	metaJSON := fmt.Sprintf(`{"from_stage":%q,"reason":%q}`, result.PreviousStage, in.Reason)
	d.logActivity(in.SpecID, "eject", fmt.Sprintf("blocked from %s", result.PreviousStage), metaJSON)

	res.CommitMsg = fmt.Sprintf("fix: eject %s — %s", in.SpecID, in.Reason)
	return res, nil
}

// ResumeInput describes a request to unblock a spec.
type ResumeInput struct {
	SpecID      string
	SpecPath    string
	ResumeStage string // explicit target; "" means detect from escape-hatch log
}

// ResumeResult is the render-ready outcome of a resume.
type ResumeResult struct {
	SpecID      string `json:"spec_id"`
	ResumeStage string `json:"resume_stage"`

	CommitMsg string `json:"-"`
}

// Resume returns a blocked spec to a prior stage. When ResumeStage is empty the
// caller is expected to have detected the pre-block stage; an empty stage here
// is an error.
func Resume(ctx context.Context, d Deps, in ResumeInput) (*ResumeResult, error) {
	meta, err := readMeta(in.SpecPath)
	if err != nil {
		return nil, err
	}
	if meta.Status != pipeline.StatusBlocked {
		return nil, fmt.Errorf("%s is not blocked (status: %s) — 'spec resume' only works on blocked specs", in.SpecID, meta.Status)
	}
	if in.ResumeStage == "" {
		return nil, fmt.Errorf("could not detect pre-block stage — use --stage to specify")
	}

	if err := pipeline.Resume(in.SpecPath, meta, in.ResumeStage); err != nil {
		return nil, err
	}

	d.logActivity(in.SpecID, "resume", fmt.Sprintf("resumed to %s", in.ResumeStage), fmt.Sprintf(`{"to_stage":%q}`, in.ResumeStage))

	return &ResumeResult{
		SpecID:      in.SpecID,
		ResumeStage: in.ResumeStage,
		CommitMsg:   fmt.Sprintf("fix: resume %s to %s", in.SpecID, in.ResumeStage),
	}, nil
}
