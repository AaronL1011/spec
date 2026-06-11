package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

// callTool drives a tools/call over the transport and returns the text content
// plus the isError flag from the structured result.
func callTool(t *testing.T, h Handler, name string, args map[string]interface{}) (string, bool) {
	t.Helper()
	params := map[string]interface{}{"name": name}
	if args != nil {
		params["arguments"] = args
	}
	pb, _ := json.Marshal(params)
	line := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":` + string(pb) + `}`
	resps := serveLines(t, h, line)
	if resps[0].Error != nil {
		t.Fatalf("tools/call %s returned JSON-RPC error: %+v", name, resps[0].Error)
	}
	result := resps[0].Result.(map[string]interface{})
	content := result["content"].([]interface{})
	text := content[0].(map[string]interface{})["text"].(string)
	return text, result["isError"] == true
}

func TestTool_Read_FullAndSection(t *testing.T) {
	h := genericHandlerWithSpecs(t)

	full, isErr := callTool(t, h, "spec_read", map[string]interface{}{"id": "SPEC-001"})
	if isErr || !strings.Contains(full, "Problem Statement") {
		t.Errorf("spec_read full = %q (isErr=%v)", full, isErr)
	}

	sec, isErr := callTool(t, h, "spec_read", map[string]interface{}{"id": "SPEC-001", "section": "problem_statement"})
	if isErr || !strings.Contains(sec, "A real problem") {
		t.Errorf("spec_read section = %q (isErr=%v)", sec, isErr)
	}

	missing, isErr := callTool(t, h, "spec_read", map[string]interface{}{"id": "SPEC-404"})
	if !isErr || !strings.Contains(missing, "not found") {
		t.Errorf("spec_read missing = %q (isErr=%v), want a not-found error result", missing, isErr)
	}
}

func TestTool_Status(t *testing.T) {
	h := genericHandlerWithSpecs(t)
	out, isErr := callTool(t, h, "spec_status", map[string]interface{}{"id": "SPEC-001"})
	if isErr {
		t.Fatalf("spec_status errored: %s", out)
	}
	for _, want := range []string{"SPEC-001", "Status: draft", "Stage Owner:"} {
		if !strings.Contains(out, want) {
			t.Errorf("spec_status output missing %q:\n%s", want, out)
		}
	}
}

func TestTool_Pipeline(t *testing.T) {
	h := genericHandlerWithSpecs(t)
	out, isErr := callTool(t, h, "spec_pipeline", nil)
	if isErr || !strings.Contains(out, "Stages:") {
		t.Errorf("spec_pipeline = %q (isErr=%v)", out, isErr)
	}
}

func TestTool_Validate(t *testing.T) {
	h := genericHandlerWithSpecs(t)
	// SPEC-001 is draft with a non-empty problem_statement; the draft→tl-review
	// gate should pass.
	out, _ := callTool(t, h, "spec_validate", map[string]interface{}{"id": "SPEC-001"})
	if !strings.Contains(out, "SPEC-001") || !strings.Contains(out, "→") {
		t.Errorf("spec_validate = %q, want a validation report", out)
	}
}

func TestTool_DecideAndResolve(t *testing.T) {
	h := genericHandlerWithSpecs(t)

	out, isErr := callTool(t, h, "spec_decide", map[string]interface{}{"id": "SPEC-001", "question": "Which cache?"})
	if isErr || !strings.Contains(out, "recorded") {
		t.Fatalf("spec_decide = %q (isErr=%v)", out, isErr)
	}

	out, isErr = callTool(t, h, "spec_decide_resolve", map[string]interface{}{
		"id": "SPEC-001", "number": 1, "decision": "Redis", "rationale": "shared state",
	})
	if isErr || !strings.Contains(out, "resolved") {
		t.Errorf("spec_decide_resolve = %q (isErr=%v)", out, isErr)
	}
}

func TestTool_Status_MissingSpec(t *testing.T) {
	h := genericHandlerWithSpecs(t)
	out, isErr := callTool(t, h, "spec_status", map[string]interface{}{"id": "SPEC-999"})
	if !isErr || !strings.Contains(out, "not found") {
		t.Errorf("spec_status missing = %q (isErr=%v), want not-found error result", out, isErr)
	}
}

func TestTool_Validate_NoSpec(t *testing.T) {
	h := genericHandlerWithSpecs(t)
	out, isErr := callTool(t, h, "spec_validate", map[string]interface{}{"id": "SPEC-999"})
	if !isErr || !strings.Contains(out, "not found") {
		t.Errorf("spec_validate missing = %q (isErr=%v)", out, isErr)
	}
}
