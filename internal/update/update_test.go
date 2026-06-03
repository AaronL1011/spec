package update

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

// fakeSource is a test double for releaseSource.
type fakeSource struct {
	latest Release
	byTag  map[string]Release
	err    error
}

func (f fakeSource) Latest(context.Context) (Release, error) {
	if f.err != nil {
		return Release{}, f.err
	}
	return f.latest, nil
}

func (f fakeSource) ByTag(_ context.Context, tag string) (Release, error) {
	if f.err != nil {
		return Release{}, f.err
	}
	rel, ok := f.byTag[tag]
	if !ok {
		return Release{}, errors.New("not found")
	}
	return rel, nil
}

func newTestUpdater(src releaseSource) *Updater {
	return &Updater{source: src, modulePath: defaultModulePath, formula: defaultFormula}
}

func TestPlan_LatestUpdateAvailable(t *testing.T) {
	t.Setenv("GOBIN", "")
	t.Setenv("GOPATH", "")
	u := newTestUpdater(fakeSource{latest: Release{Tag: "v2.0.0"}})

	plan, err := u.Plan(context.Background(), Options{
		CurrentVersion: "v1.0.0",
		ExecPath:       filepath.Join(t.TempDir(), "spec"),
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if !plan.UpdateAvailable {
		t.Error("expected update available")
	}
	if plan.LatestVersion != "v2.0.0" {
		t.Errorf("LatestVersion = %q, want v2.0.0", plan.LatestVersion)
	}
	if plan.Method != MethodBinary {
		t.Errorf("Method = %q, want %q", plan.Method, MethodBinary)
	}
}

func TestPlan_AlreadyLatest(t *testing.T) {
	u := newTestUpdater(fakeSource{latest: Release{Tag: "v1.0.0"}})
	plan, err := u.Plan(context.Background(), Options{
		CurrentVersion: "v1.0.0",
		ExecPath:       filepath.Join(t.TempDir(), "spec"),
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.UpdateAvailable {
		t.Error("expected no update available")
	}
}

func TestPlan_PinnedTag(t *testing.T) {
	u := newTestUpdater(fakeSource{
		byTag: map[string]Release{"v1.5.0": {Tag: "v1.5.0"}},
	})
	plan, err := u.Plan(context.Background(), Options{
		CurrentVersion: "v1.0.0",
		ExecPath:       filepath.Join(t.TempDir(), "spec"),
		TargetVersion:  "v1.5.0",
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.LatestVersion != "v1.5.0" {
		t.Errorf("LatestVersion = %q, want v1.5.0", plan.LatestVersion)
	}
}

func TestPlan_FetchError(t *testing.T) {
	u := newTestUpdater(fakeSource{err: errors.New("network down")})
	_, err := u.Plan(context.Background(), Options{
		CurrentVersion: "v1.0.0",
		ExecPath:       filepath.Join(t.TempDir(), "spec"),
	})
	if err == nil {
		t.Fatal("expected error from failing source")
	}
}

func TestApply_NoopWhenUpToDate(t *testing.T) {
	u := newTestUpdater(fakeSource{latest: Release{Tag: "v1.0.0"}})
	plan := &Plan{Method: MethodBinary, UpdateAvailable: false, opts: Options{Force: false}}
	if err := u.Apply(context.Background(), plan, nil, nil); err != nil {
		t.Errorf("Apply should be a no-op when up to date: %v", err)
	}
}

func TestModuleRef(t *testing.T) {
	u := newTestUpdater(fakeSource{})

	// With no resolved tag at all, fall back to @latest.
	if got := u.moduleRef(&Plan{}); got != defaultModulePath+"@latest" {
		t.Errorf("moduleRef = %q, want @latest", got)
	}

	// The resolved latest tag must be pinned exactly so go install fetches the
	// same version the GitHub release API reported, not whatever the module
	// proxy considers @latest.
	resolved := &Plan{LatestVersion: "v0.15.0"}
	if got := u.moduleRef(resolved); got != defaultModulePath+"@v0.15.0" {
		t.Errorf("moduleRef = %q, want @v0.15.0", got)
	}

	// An explicitly requested target version wins over the resolved latest.
	pinned := &Plan{LatestVersion: "v0.15.0", opts: Options{TargetVersion: "v1.2.0"}}
	if got := u.moduleRef(pinned); got != defaultModulePath+"@v1.2.0" {
		t.Errorf("moduleRef = %q, want @v1.2.0", got)
	}
}
