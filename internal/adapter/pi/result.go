package pi

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"

	"github.com/aaronl1011/spec/internal/adapter"
)

// maxRawTail bounds how much trailing harness output InvokeResult.Raw keeps for
// debugging when structured parsing yields nothing useful.
const maxRawTail = 4096

// parseHeadlessStream consumes pi's `--mode json` event stream (one JSON object
// per line) and distils it into a session-level InvokeResult. It degrades
// gracefully: unrecognised lines are ignored, and a stream that yields no
// terminal event still returns a usable result with Raw populated.
//
// The contract pi emits is documented in docs/json.md: a `session` header line
// followed by AgentSessionEvent objects. We care about the terminal lifecycle
// events (agent_end, auto_retry_end, compaction_end) and any per-message token
// usage, not the per-token deltas.
func parseHeadlessStream(r io.Reader) adapter.InvokeResult {
	var res adapter.InvokeResult
	var tail strings.Builder

	scanner := bufio.NewScanner(r)
	// Agent output lines can be large (full message snapshots); raise the cap.
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	sawAgentEnd := false
	// Usage for the message currently in flight. A session spans many messages
	// whose costs sum, but each message's usage arrives as a cumulative snapshot
	// re-emitted on every event that touches it — so the snapshot is tracked
	// separately and folded into the session total once, at the message boundary.
	var msgUsage adapter.TokenUsage
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		appendTail(&tail, line)

		var ev streamEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			// Not a JSON event line (e.g. interleaved log output) — skip it.
			continue
		}

		setUsage(&msgUsage, ev.usage())
		if ev.Type == "message_end" {
			// Message complete: bank its final usage and reset for the next one.
			res.Tokens.Input += msgUsage.Input
			res.Tokens.Output += msgUsage.Output
			res.Tokens.Total += msgUsage.Total
			msgUsage = adapter.TokenUsage{}
		}

		switch ev.Type {
		case "agent_end":
			sawAgentEnd = true
			res.ExitReason = "completed"
			res.ErrorClass = ""
			res.ErrorMessage = ""
		case "auto_retry_end":
			if !ev.Success {
				res.ExitReason = "error"
				res.ErrorClass = "auto_retry_exhausted"
				if ev.FinalError != "" {
					res.ErrorMessage = ev.FinalError
				}
			}
		case "compaction_end":
			if ev.Aborted && !ev.WillRetry {
				res.ExitReason = "error"
				res.ErrorClass = "compaction_failed"
				if ev.ErrorMessage != "" {
					res.ErrorMessage = ev.ErrorMessage
				}
			}
		}
	}

	// A stream that ended mid-message (crash, cancellation) still reports what
	// the unfinished message cost.
	res.Tokens.Input += msgUsage.Input
	res.Tokens.Output += msgUsage.Output
	res.Tokens.Total += msgUsage.Total

	if !sawAgentEnd && res.ExitReason == "" {
		// The stream ended without a terminal event — surface the tail so the
		// run is still debuggable.
		res.Raw = strings.TrimSpace(tail.String())
	}
	return res
}

// streamEvent is a permissive view over the pi event stream. Only the fields we
// act on are decoded; everything else is ignored so format drift in unrelated
// fields never breaks parsing.
type streamEvent struct {
	Type string `json:"type"`

	// auto_retry_end
	Success    bool   `json:"success"`
	FinalError string `json:"finalError"`

	// compaction_end
	Aborted      bool   `json:"aborted"`
	WillRetry    bool   `json:"willRetry"`
	ErrorMessage string `json:"errorMessage"`

	// Token usage may ride on a message snapshot under a few shapes; decode the
	// common ones.
	Usage   *usageBlock `json:"usage"`
	Message *struct {
		Usage *usageBlock `json:"usage"`
	} `json:"message"`
}

// usageBlock matches the token-usage shapes pi/Anthropic emit. Field names vary
// across harness versions, so several aliases are accepted.
type usageBlock struct {
	Input        int `json:"input"`
	InputTokens  int `json:"inputTokens"`
	PromptTokens int `json:"prompt_tokens"`

	Output           int `json:"output"`
	OutputTokens     int `json:"outputTokens"`
	CompletionTokens int `json:"completion_tokens"`

	// Total and TotalTokens accept the spellings whose meaning is
	// input+output. pi's own camelCase `totalTokens` is deliberately NOT
	// aliased here: it also counts cache reads and writes, which are fixed
	// harness overhead rather than work attributable to a request (a two-token
	// prompt reports 650 because the cached system prompt dominates). Deriving
	// input+output instead keeps one meaning of "tokens" across every provider,
	// so the activity log's per-task figures stay comparable.
	Total       int `json:"total"`
	TotalTokens int `json:"total_tokens"`
}

// usage returns the most relevant usage block on the event, preferring a
// top-level block over a message-nested one.
func (e streamEvent) usage() *usageBlock {
	if e.Usage != nil {
		return e.Usage
	}
	if e.Message != nil {
		return e.Message.Usage
	}
	return nil
}

func (u *usageBlock) input() int  { return firstNonZero(u.Input, u.InputTokens, u.PromptTokens) }
func (u *usageBlock) output() int { return firstNonZero(u.Output, u.OutputTokens, u.CompletionTokens) }
func (u *usageBlock) total() int  { return firstNonZero(u.Total, u.TotalTokens) }

// setUsage replaces the running totals with a usage block's figures.
//
// Replace rather than accumulate: pi reports usage as a running snapshot of the
// current message, re-emitted on every event that touches it (message_start,
// each message_update, message_end, turn_end), so summing them multiplies the
// real cost by the number of events. Since the snapshot is cumulative, the last
// one seen for a message is already the total.
//
// A zero-valued block is ignored: early snapshots are emitted before the
// provider reports usage, and letting one overwrite a real figure would
// discard it.
func setUsage(dst *adapter.TokenUsage, u *usageBlock) {
	if u == nil {
		return
	}
	in, out, total := u.input(), u.output(), u.total()
	if in == 0 && out == 0 && total == 0 {
		return
	}
	dst.Input = in
	dst.Output = out
	if total > 0 {
		dst.Total = total
	} else {
		dst.Total = in + out
	}
}

func firstNonZero(vals ...int) int {
	for _, v := range vals {
		if v != 0 {
			return v
		}
	}
	return 0
}

// appendTail keeps a bounded tail of recent output lines for InvokeResult.Raw.
func appendTail(tail *strings.Builder, line string) {
	tail.WriteString(line)
	tail.WriteByte('\n')
	if tail.Len() <= maxRawTail {
		return
	}
	// Trim from the front to keep only the most recent maxRawTail bytes.
	s := tail.String()
	trimmed := s[len(s)-maxRawTail:]
	tail.Reset()
	tail.WriteString(trimmed)
}
