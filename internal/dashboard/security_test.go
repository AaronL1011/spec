package dashboard

import (
	"strings"
	"testing"
	"time"

	"github.com/aaronl1011/spec/internal/adapter"
	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/urgency"
)

func TestSecurityDashboardItems_SurfaceWindow(t *testing.T) {
	now := time.Now()
	var cfg config.SecurityConfig // defaults: critical 1d, high 1w, low 30d, surface 24h

	alerts := []adapter.SecurityAlert{
		// Critical, 20h old (1d SLA) → ~4h left → within 24h → surfaces.
		{Number: 1, Title: "crit soon", Severity: adapter.SeverityCritical, CreatedAt: now.Add(-20 * time.Hour)},
		// Low, 1h old (30d SLA) → ~30d left → not within 24h → hidden.
		{Number: 2, Title: "low far", Severity: adapter.SeverityLow, CreatedAt: now.Add(-1 * time.Hour)},
		// High, 10d old (1w SLA) → overdue → surfaces.
		{Number: 3, Title: "overdue", Severity: adapter.SeverityHigh, CreatedAt: now.Add(-10 * 24 * time.Hour)},
		// Unknown severity → no SLA window → skipped.
		{Number: 4, Title: "unknown", Severity: adapter.SeverityUnknown, CreatedAt: now.Add(-100 * 24 * time.Hour)},
	}

	items := securityDashboardItems(alerts, cfg, urgency.EaseIn, now)
	if len(items) != 2 {
		t.Fatalf("surfaced %d items, want 2 (crit soon + overdue); got %+v", len(items), items)
	}

	titles := items[0].Title + "|" + items[1].Title
	if !strings.Contains(titles, "crit soon") || !strings.Contains(titles, "overdue") {
		t.Errorf("surfaced titles = %q, want 'crit soon' and 'overdue'", titles)
	}
	for _, it := range items {
		if it.Title == "low far" || it.Title == "unknown" {
			t.Errorf("%q should not surface on the dashboard", it.Title)
		}
		if it.StaleFraction <= 0 {
			t.Errorf("%q should carry a deadline gradient, got fraction %v", it.Title, it.StaleFraction)
		}
	}
}

func TestSecurityDashboardItems_KeepsPerManifest(t *testing.T) {
	now := time.Now()
	var cfg config.SecurityConfig
	// The same advisory reported against three manifests surfaces as three
	// rows — the dashboard mirrors the Security tab's per-manifest detail.
	created := now.Add(-40 * 24 * time.Hour) // low 30d SLA → overdue
	alerts := []adapter.SecurityAlert{
		{Number: 1, Title: "esbuild dev server file read", Identifier: "GHSA-g7r4", Severity: adapter.SeverityLow, Package: "esbuild", Manifest: "a/package-lock.json", CreatedAt: created},
		{Number: 2, Title: "esbuild dev server file read", Identifier: "GHSA-g7r4", Severity: adapter.SeverityLow, Package: "esbuild", Manifest: "b/package-lock.json", CreatedAt: created},
		{Number: 3, Title: "esbuild dev server file read", Identifier: "GHSA-g7r4", Severity: adapter.SeverityLow, Package: "esbuild", Manifest: "c/package-lock.json", CreatedAt: created},
	}
	items := securityDashboardItems(alerts, cfg, urgency.EaseIn, now)
	if len(items) != 3 {
		t.Fatalf("got %d dashboard rows, want 3 (one per manifest, no dedupe)", len(items))
	}
}

func TestSecurityDetail_OverdueVsCountdown(t *testing.T) {
	a := adapter.SecurityAlert{Package: "lodash", Repo: "web"}

	if got := securityDetail(a, 5*time.Hour); !strings.Contains(got, "lodash") || !strings.Contains(got, "left") {
		t.Errorf("countdown detail = %q, want package + '... left'", got)
	}
	if got := securityDetail(a, -2*time.Hour); !strings.Contains(got, "overdue") {
		t.Errorf("overdue detail = %q, want 'overdue'", got)
	}
}
