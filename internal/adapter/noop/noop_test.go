package noop

import (
	"context"
	"testing"
)

// TestRepo_DraftPRsAreSafeNoops verifies the noop repo adapter returns empty
// values and nil errors for the draft-PR surface — an unconfigured GitHub never
// panics or blocks a build.
func TestRepo_DraftPRsAreSafeNoops(t *testing.T) {
	ctx := context.Background()
	var r Repo

	num, url, err := r.OpenDraftPR(ctx, "repo", "head", "base", "title", "body")
	if num != 0 || url != "" || err != nil {
		t.Errorf("OpenDraftPR = (%d, %q, %v), want (0, \"\", nil)", num, url, err)
	}
	if err := r.SetPRBase(ctx, "repo", 7, "main"); err != nil {
		t.Errorf("SetPRBase = %v, want nil", err)
	}
}
