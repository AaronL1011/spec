package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/llm"
	"github.com/aaronl1011/spec/internal/store"
)

// Generation telemetry rides the existing activity table rather than new
// storage. Recording task id, duration, token usage, and outcome (including how
// many retries a reviewer needed) is what turns "headless completions might be
// slow" from an opinion into a measurement — the evidence an API fast path
// would need before it is worth adding.
//
// Every call is best-effort: a missing or unwritable database must never fail a
// draft the user already accepted.

// generationTelemetry is the metadata recorded for one review.
type generationTelemetry struct {
	Task       string `json:"task"`
	Outcome    string `json:"outcome"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	Retries    int    `json:"retries,omitempty"`
	Model      string `json:"model,omitempty"`
	Tokens     int    `json:"tokens,omitempty"`
	Provider   string `json:"provider,omitempty"`
}

// recordGeneration logs one completed review to the activity log.
//
// It is attributed to the human actor kind deliberately: a spec-initiated draft
// is something a person asked for and accepted, unlike an authoring-port write
// an agent performed on its own.
func recordGeneration(rc *config.ResolvedConfig, specID string, out *llm.Outcome, taskID string) {
	if out == nil {
		return
	}

	db, err := openDB()
	if err != nil {
		return
	}
	defer func() { _ = db.Close() }()

	tel := generationTelemetry{
		Task:     taskID,
		Outcome:  string(out.Action),
		Retries:  out.RetryCount(),
		Provider: rc.EffectiveAgentConfig().Provider,
	}
	// Report the cost of the attempt the reviewer acted on, which is the one
	// that mattered.
	if len(out.Attempts) > 0 {
		last := out.Attempts[len(out.Attempts)-1]
		if last.Result != nil {
			tel.DurationMS = last.Result.Duration.Milliseconds()
			tel.Model = last.Result.Model
			tel.Tokens = last.Result.Tokens.Total
		}
	}

	metaJSON, err := json.Marshal(tel)
	if err != nil {
		return
	}

	summary := fmt.Sprintf("agent draft %s: %s", taskID, out.Action)
	if tel.Retries > 0 {
		summary += fmt.Sprintf(" after %d retries", tel.Retries)
	}

	_ = db.ActivityLogAs(specID, "agent_generate", summary, string(metaJSON), rc.UserName(), store.ActorHuman)
}
