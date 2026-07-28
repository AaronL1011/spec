package agentcheck

import "github.com/aaronl1011/spec/internal/adapter"

// Report is the diagnostic result of a preflight Check. It carries everything
// the CLI and TUI need to render a preflight, including a partial result on
// failure: a failed step sets FailedStep and Err while leaving the rest of the
// fields populated up to where the check stopped.
type Report struct {
	Provider      string               `json:"provider"`
	Binary        string               `json:"binary,omitempty"`
	Endpoint      string               `json:"endpoint,omitempty"`
	Capabilities  adapter.Capabilities `json:"capabilities"`
	Model         string               `json:"model,omitempty"`
	LatencyMS     int64                `json:"latency_ms,omitempty"`
	Tokens        int                  `json:"tokens,omitempty"`
	InertSettings []string             `json:"inert_settings,omitempty"`
	FailedStep    string               `json:"failed_step,omitempty"`
	Err           string               `json:"error,omitempty"`
}
