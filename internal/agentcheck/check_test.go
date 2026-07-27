package agentcheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aaronl1011/spec/internal/adapter"
	"github.com/aaronl1011/spec/internal/adapter/openaicompat"
	"github.com/aaronl1011/spec/internal/config"
)

// The preflight's job is to turn misconfiguration into a named failing step
// rather than a mid-draft mystery, so these assert which step fails and that the
// round-trip is measured.

func TestCheckGenerate_MeasuresRoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"model": "stub-model-1",
			"choices": [{"message": {"content": "ok"}}],
			"usage": {"total_tokens": 7}
		}`))
	}))
	defer srv.Close()

	client, err := openaicompat.New(openaicompat.Options{BaseURL: srv.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}

	var report Report
	if err := checkGenerate(client, &report); err != nil {
		t.Fatalf("checkGenerate: %v", err)
	}
	if report.Model != "stub-model-1" {
		t.Errorf("model = %q, want the model that actually answered", report.Model)
	}
	if report.Tokens != 7 {
		t.Errorf("tokens = %d, want 7", report.Tokens)
	}
	// Latency is the evidence a future API fast path would need, so it must be
	// recorded rather than merely observed.
	if report.LatencyMS < 0 {
		t.Errorf("latency = %d, want a measured value", report.LatencyMS)
	}
}

// An empty response is a failure with a diagnosis, not a silent pass.
func TestCheckGenerate_EmptyResponseFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":""}}]}`))
	}))
	defer srv.Close()

	client, err := openaicompat.New(openaicompat.Options{BaseURL: srv.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}

	var report Report
	err = checkGenerate(client, &report)
	if err == nil {
		t.Fatal("an empty completion should fail the check")
	}
	if !strings.Contains(err.Error(), "no text") {
		t.Errorf("error should say the provider returned nothing, got %q", err)
	}
}

func TestCheckReachable(t *testing.T) {
	tests := []struct {
		name      string
		cfg       config.ProviderConfig
		wantErr   string
		wantField func(Report) string
	}{
		{
			name: "missing harness binary names the fix",
			cfg: config.ProviderConfig{
				Provider: "pi",
				Extra:    map[string]string{"command": "definitely-not-installed-xyz"},
			},
			wantErr: "not found in PATH",
		},
		{
			name:    "anthropic without a token",
			cfg:     config.ProviderConfig{Provider: "anthropic", Extra: map[string]string{}},
			wantErr: "no token configured",
		},
		{
			name: "completion endpoint is recorded",
			cfg: config.ProviderConfig{
				Provider: "openai-compatible",
				Extra:    map[string]string{},
				Generate: config.GenerateConfig{BaseURL: "http://localhost:1234/v1"},
			},
			wantField: func(r Report) string { return r.Endpoint },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var report Report
			err := checkReachable(tt.cfg, &report)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected an error containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %q, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantField != nil && tt.wantField(report) == "" {
				t.Error("expected the report to record the endpoint or binary")
			}
		})
	}
}

// A token cap on a harness provider silently does nothing, so the check must say
// so rather than let it look effective.
func TestInertSettings(t *testing.T) {
	harness := config.ProviderConfig{
		Provider: "pi",
		Generate: config.GenerateConfig{MaxTokens: 2048, BaseURL: "http://x/v1", Token: "t"},
	}
	inert := InertSettings(harness)
	if len(inert) != 3 {
		t.Fatalf("expected max_tokens, base_url and token to be reported inert, got %v", inert)
	}
	for _, want := range []string{"max_tokens", "base_url", "token"} {
		found := false
		for _, s := range inert {
			if strings.Contains(s, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("inert list should mention %q, got %v", want, inert)
		}
	}

	// The same settings are meaningful for a completion API, so they must not be
	// flagged there.
	api := config.ProviderConfig{
		Provider: "anthropic",
		Generate: config.GenerateConfig{MaxTokens: 2048},
	}
	if got := InertSettings(api); len(got) != 0 {
		t.Errorf("max_tokens is honoured by completion APIs, should not be flagged: %v", got)
	}
}

// Detection drives the zero-typing setup path, so it must only report binaries
// that exist.
func TestDetected_OnlyReportsInstalled(t *testing.T) {
	found := Detected()
	for _, provider := range found {
		var command string
		for _, h := range KnownHarnesses {
			if h.Provider == provider {
				command = h.Command
			}
		}
		if command == "" {
			t.Errorf("detected unknown provider %q", provider)
		}
	}
	// The hint is only useful when something was found.
	hint := DetectedHint()
	if len(found) == 0 && hint != "" {
		t.Errorf("hint should be empty when nothing is installed, got %q", hint)
	}
	if len(found) > 0 && !strings.Contains(hint, found[0]) {
		t.Errorf("hint %q should name the detected harness %q", hint, found[0])
	}
}

// The probe must be contained exactly like production calls: same flags, same
// empty working directory. A check that ran with more privilege than real usage
// would prove the wrong thing.
func TestCheck_ProbeUsesProductionContainment(t *testing.T) {
	// The containment argv is asserted per harness in the adapter packages; this
	// pins that the check goes through the same Generate path rather than a
	// bespoke invocation.
	var recorded adapter.GenerateRequest
	stub := &recordingAgent{caps: adapter.Capabilities{Generate: true}, text: "ok", record: &recorded}

	if err := checkGenerate(stub, &Report{}); err != nil {
		t.Fatalf("checkGenerate: %v", err)
	}
	if recorded.Task == "" {
		t.Error("the probe should go through a declared task, not an ad-hoc request")
	}
	// A tiny cap keeps the probe cheap on a metered provider.
	if recorded.MaxTokens == 0 || recorded.MaxTokens > 64 {
		t.Errorf("probe MaxTokens = %d, want a small cap", recorded.MaxTokens)
	}
}

// recordingAgent captures the request it was given, so the probe's shape can be
// asserted without a provider.
type recordingAgent struct {
	caps   adapter.Capabilities
	text   string
	record *adapter.GenerateRequest
}

func (r *recordingAgent) Invoke(ctx context.Context, req adapter.InvokeRequest) (*adapter.InvokeResult, error) {
	return nil, adapter.ErrNotSupported
}

func (r *recordingAgent) Generate(ctx context.Context, req adapter.GenerateRequest) (*adapter.GenerateResult, error) {
	*r.record = req
	return &adapter.GenerateResult{Text: r.text, Model: "stub"}, nil
}

func (r *recordingAgent) Capabilities() adapter.Capabilities { return r.caps }
