package pi

import (
	"strings"
	"testing"
)

func TestParseHeadlessStream_CompletedRun(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"session","version":3,"id":"abc","cwd":"/tmp"}`,
		`{"type":"agent_start"}`,
		`{"type":"turn_start"}`,
		`{"type":"message_end","message":{"role":"assistant","usage":{"inputTokens":120,"outputTokens":45}}}`,
		`{"type":"agent_end","messages":[]}`,
	}, "\n")

	res := parseHeadlessStream(strings.NewReader(stream))
	if res.ExitReason != "completed" {
		t.Errorf("ExitReason = %q, want completed", res.ExitReason)
	}
	if res.ErrorClass != "" {
		t.Errorf("ErrorClass = %q, want empty", res.ErrorClass)
	}
	if res.Tokens.Input != 120 || res.Tokens.Output != 45 || res.Tokens.Total != 165 {
		t.Errorf("Tokens = %+v, want in120/out45/total165", res.Tokens)
	}
	if res.Raw != "" {
		t.Errorf("Raw = %q, want empty on clean completion", res.Raw)
	}
}

func TestParseHeadlessStream_AutoRetryExhausted(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"agent_start"}`,
		`{"type":"auto_retry_start","attempt":1,"maxAttempts":3,"delayMs":1000,"errorMessage":"rate limit"}`,
		`{"type":"auto_retry_end","success":false,"attempt":3,"finalError":"rate limit: 429"}`,
	}, "\n")

	res := parseHeadlessStream(strings.NewReader(stream))
	if res.ExitReason != "error" {
		t.Errorf("ExitReason = %q, want error", res.ExitReason)
	}
	if res.ErrorClass != "auto_retry_exhausted" {
		t.Errorf("ErrorClass = %q, want auto_retry_exhausted", res.ErrorClass)
	}
	if res.ErrorMessage != "rate limit: 429" {
		t.Errorf("ErrorMessage = %q", res.ErrorMessage)
	}
}

func TestParseHeadlessStream_CompactionFailed(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"agent_start"}`,
		`{"type":"compaction_start","reason":"overflow"}`,
		`{"type":"compaction_end","reason":"overflow","aborted":true,"willRetry":false,"errorMessage":"context too large"}`,
	}, "\n")

	res := parseHeadlessStream(strings.NewReader(stream))
	if res.ErrorClass != "compaction_failed" {
		t.Errorf("ErrorClass = %q, want compaction_failed", res.ErrorClass)
	}
	if res.ErrorMessage != "context too large" {
		t.Errorf("ErrorMessage = %q", res.ErrorMessage)
	}
}

func TestParseHeadlessStream_RetryThenSuccessIsClean(t *testing.T) {
	// A failed-then-succeeded retry must not leave the run marked as an error:
	// the terminal agent_end wins.
	stream := strings.Join([]string{
		`{"type":"auto_retry_end","success":true,"attempt":2}`,
		`{"type":"agent_end","messages":[]}`,
	}, "\n")

	res := parseHeadlessStream(strings.NewReader(stream))
	if res.ExitReason != "completed" || res.ErrorClass != "" {
		t.Errorf("res = %+v, want a clean completion", res)
	}
}

func TestParseHeadlessStream_DegradesOnGarbage(t *testing.T) {
	stream := "this is not json\nneither is this\n"
	res := parseHeadlessStream(strings.NewReader(stream))
	if res.ExitReason != "" || res.ErrorClass != "" {
		t.Errorf("res = %+v, want zero-value structured fields", res)
	}
	if !strings.Contains(res.Raw, "not json") {
		t.Errorf("Raw = %q, want the unparsed tail retained for debugging", res.Raw)
	}
}

func TestParseHeadlessStream_TokenAliasesAndTotal(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"message_end","usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}}`,
		`{"type":"message_end","message":{"usage":{"input":5,"output":7}}}`,
		`{"type":"agent_end","messages":[]}`,
	}, "\n")

	res := parseHeadlessStream(strings.NewReader(stream))
	// First: total reported as 30. Second: no total → derived 12.
	if res.Tokens.Input != 15 || res.Tokens.Output != 27 || res.Tokens.Total != 42 {
		t.Errorf("Tokens = %+v, want in15/out27/total42", res.Tokens)
	}
}

func TestAppendTail_BoundsLength(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 5000; i++ {
		appendTail(&b, "0123456789")
	}
	if b.Len() > maxRawTail+16 {
		t.Errorf("tail length = %d, want bounded near %d", b.Len(), maxRawTail)
	}
}
