package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aaronl1011/spec/internal/config"
)

// serveLines feeds raw JSON-RPC lines through Serve and returns the parsed
// responses in order. It drives the real transport, so malformed input
// exercises the protocol error paths exactly as a third-party agent would hit
// them.
func serveLines(t *testing.T, handler Handler, lines ...string) []response {
	t.Helper()
	in := strings.NewReader(strings.Join(lines, "\n") + "\n")
	var out bytes.Buffer
	if err := Serve(context.Background(), handler, in, &out, io.Discard); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	var resps []response
	for _, raw := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if raw == "" {
			continue
		}
		var r response
		if err := json.Unmarshal([]byte(raw), &r); err != nil {
			t.Fatalf("unmarshal response %q: %v", raw, err)
		}
		resps = append(resps, r)
	}
	return resps
}

// genericHandlerWithSpecs builds a GenericHandler over a temp specs dir seeded
// with a couple of specs plus a team config so pipeline-aware tools resolve.
func genericHandlerWithSpecs(t *testing.T) *GenericHandler {
	t.Helper()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "SPEC-001.md"),
		"---\nid: SPEC-001\ntitle: First\nstatus: draft\nauthor: Dev\ncycle: Cycle 0\n---\n\n## 1. Problem Statement\nA real problem.\n\n## Decision Log\n| # | Question / Decision | Options Considered | Decision Made | Rationale | Decided By | Date |\n|---|---|---|---|---|---|---|\n")
	mustWrite(t, filepath.Join(dir, "SPEC-002.md"),
		"---\nid: SPEC-002\ntitle: Second\nstatus: build\nauthor: Other\ncycle: Cycle 0\n---\n\n## TL;DR\nshort\n")

	team := &config.TeamConfig{}
	team.Pipeline = config.DefaultPipeline()
	rc := &config.ResolvedConfig{Team: team}
	return NewGenericHandler(rc, dir)
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestProtocol_InitializeHandshake asserts the initialize envelope over the
// real transport with the generic handler.
func TestProtocol_InitializeHandshake(t *testing.T) {
	h := genericHandlerWithSpecs(t)
	resps := serveLines(t, h, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{}}}`)
	if len(resps) != 1 {
		t.Fatalf("got %d responses, want 1", len(resps))
	}
	result := resps[0].Result.(map[string]interface{})
	if result["protocolVersion"] != "2024-11-05" {
		t.Errorf("protocolVersion = %v", result["protocolVersion"])
	}
	caps := result["capabilities"].(map[string]interface{})
	if _, ok := caps["tools"]; !ok {
		t.Error("capabilities missing tools")
	}
	if _, ok := caps["resources"]; !ok {
		t.Error("capabilities missing resources")
	}
}

// TestProtocol_ToolsListAndResourcesList drives discovery over the wire.
func TestProtocol_ToolsListAndResourcesList(t *testing.T) {
	h := genericHandlerWithSpecs(t)
	resps := serveLines(t, h,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":2,"method":"resources/list"}`,
	)
	if len(resps) != 2 {
		t.Fatalf("got %d responses, want 2", len(resps))
	}
	tools := resps[0].Result.(map[string]interface{})["tools"].([]interface{})
	if len(tools) == 0 {
		t.Error("expected tools in tools/list")
	}
	resources := resps[1].Result.(map[string]interface{})["resources"].([]interface{})
	// pipeline + dashboard + 2 specs.
	if len(resources) < 4 {
		t.Errorf("got %d resources, want >= 4", len(resources))
	}
}

// TestProtocol_MalformedToolArgs asserts a tool called with malformed
// json.RawMessage arguments returns a structured result/error and never closes
// the transport or panics (AC-5).
func TestProtocol_MalformedToolArgs(t *testing.T) {
	h := genericHandlerWithSpecs(t)
	// arguments is a string where the tool expects an object → decode fails
	// inside the tool, surfaced as a JSON-RPC error response.
	resps := serveLines(t, h,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"spec_read","arguments":"not-an-object"}}`,
		// A following well-formed request must still be served — the transport
		// stayed open.
		`{"jsonrpc":"2.0","id":2,"method":"ping"}`,
	)
	if len(resps) != 2 {
		t.Fatalf("got %d responses, want 2 (transport stayed open)", len(resps))
	}
	// The tool's inner json.Unmarshal fails on the malformed arguments; the
	// server surfaces it as a structured error *result* (isError:true), not a
	// transport-level crash.
	if resps[0].Error != nil {
		// Some tools bubble the decode error up as a JSON-RPC error — also fine.
	} else {
		result, ok := resps[0].Result.(map[string]interface{})
		if !ok || result["isError"] != true {
			t.Errorf("expected an error result for malformed tool args, got %+v", resps[0].Result)
		}
	}
	if resps[1].Error != nil {
		t.Errorf("follow-up ping errored: %v", resps[1].Error)
	}
}

// TestProtocol_MalformedToolCallParams asserts malformed params on tools/call
// itself (not the inner arguments) returns -32602 invalid params.
func TestProtocol_MalformedToolCallParams(t *testing.T) {
	h := genericHandlerWithSpecs(t)
	resps := serveLines(t, h, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":"bogus"}`)
	if resps[0].Error == nil || resps[0].Error.Code != -32602 {
		t.Fatalf("expected -32602 invalid params, got %+v", resps[0].Error)
	}
}

// TestProtocol_UnknownTool asserts an unknown tool name returns an error result.
func TestProtocol_UnknownTool(t *testing.T) {
	h := genericHandlerWithSpecs(t)
	resps := serveLines(t, h,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"no_such_tool","arguments":{}}}`)
	result := resps[0].Result.(map[string]interface{})
	if result["isError"] != true {
		t.Errorf("isError = %v, want true for unknown tool", result["isError"])
	}
}

// TestProtocol_ResourceNotFound asserts an unresolvable spec:// URI returns the
// -32602 resource-not-found error (AC-5).
func TestProtocol_ResourceNotFound(t *testing.T) {
	h := genericHandlerWithSpecs(t)
	resps := serveLines(t, h,
		`{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"spec://SPEC-999"}}`)
	if resps[0].Error == nil || resps[0].Error.Code != -32602 {
		t.Fatalf("expected -32602 resource not found, got %+v", resps[0].Error)
	}
}

// TestProtocol_ResourceRead_Pipeline reads the synthesized pipeline resource.
func TestProtocol_ResourceRead_Pipeline(t *testing.T) {
	h := genericHandlerWithSpecs(t)
	resps := serveLines(t, h,
		`{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"spec://pipeline"}}`)
	contents := resps[0].Result.(map[string]interface{})["contents"].([]interface{})
	text := contents[0].(map[string]interface{})["text"].(string)
	if !strings.Contains(text, "Pipeline Configuration") {
		t.Errorf("pipeline resource = %q, want a pipeline doc", text)
	}
}

// TestProtocol_ResourceRead_Dashboard reads the synthesized dashboard resource.
func TestProtocol_ResourceRead_Dashboard(t *testing.T) {
	h := genericHandlerWithSpecs(t)
	resps := serveLines(t, h,
		`{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"spec://dashboard"}}`)
	contents := resps[0].Result.(map[string]interface{})["contents"].([]interface{})
	text := contents[0].(map[string]interface{})["text"].(string)
	if !strings.Contains(text, "SPEC-001") || !strings.Contains(text, "SPEC-002") {
		t.Errorf("dashboard resource = %q, want both specs", text)
	}
}

// TestProtocol_ResourceRead_Spec reads a full spec resource by URI.
func TestProtocol_ResourceRead_Spec(t *testing.T) {
	h := genericHandlerWithSpecs(t)
	resps := serveLines(t, h,
		`{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"spec://SPEC-001"}}`)
	contents := resps[0].Result.(map[string]interface{})["contents"].([]interface{})
	text := contents[0].(map[string]interface{})["text"].(string)
	if !strings.Contains(text, "Problem Statement") {
		t.Errorf("spec resource = %q, want the spec body", text)
	}
}

// TestProtocol_ResourceRead_SpecSection reads a single section by URI.
func TestProtocol_ResourceRead_SpecSection(t *testing.T) {
	h := genericHandlerWithSpecs(t)
	resps := serveLines(t, h,
		`{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"spec://SPEC-001/section/problem_statement"}}`)
	if resps[0].Error != nil {
		t.Fatalf("section read errored: %v", resps[0].Error)
	}
	contents := resps[0].Result.(map[string]interface{})["contents"].([]interface{})
	text := contents[0].(map[string]interface{})["text"].(string)
	if !strings.Contains(text, "A real problem") {
		t.Errorf("section resource = %q, want the section body", text)
	}
}

// TestProtocol_ResourceRead_SectionNotFound asserts a missing section URI errors.
func TestProtocol_ResourceRead_SectionNotFound(t *testing.T) {
	h := genericHandlerWithSpecs(t)
	resps := serveLines(t, h,
		`{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"spec://SPEC-002/section/nonexistent"}}`)
	if resps[0].Error == nil {
		t.Fatal("expected an error for a missing section")
	}
}

// TestProtocol_MalformedResourceParams asserts -32602 for unparsable params.
func TestProtocol_MalformedResourceParams(t *testing.T) {
	h := genericHandlerWithSpecs(t)
	resps := serveLines(t, h, `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":42}`)
	if resps[0].Error == nil || resps[0].Error.Code != -32602 {
		t.Fatalf("expected -32602, got %+v", resps[0].Error)
	}
}

// TestProtocol_ParseError asserts a non-JSON line yields -32700 parse error and
// the transport stays open for the next valid request.
func TestProtocol_ParseError(t *testing.T) {
	h := genericHandlerWithSpecs(t)
	resps := serveLines(t, h,
		`this is not json`,
		`{"jsonrpc":"2.0","id":2,"method":"ping"}`)
	if len(resps) != 2 {
		t.Fatalf("got %d responses, want 2", len(resps))
	}
	if resps[0].Error == nil || resps[0].Error.Code != -32700 {
		t.Fatalf("expected -32700 parse error, got %+v", resps[0].Error)
	}
}
