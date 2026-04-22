package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
)

// stubHandler is a test double for Handler.
type stubHandler struct {
	resources []Resource
	tools     []Tool
}

func (h *stubHandler) ListResources() []Resource { return h.resources }

func (h *stubHandler) GetResource(uri string) (*Resource, error) {
	for i := range h.resources {
		if h.resources[i].URI == uri {
			return &h.resources[i], nil
		}
	}
	return nil, fmt.Errorf("not found: %s", uri)
}

func (h *stubHandler) ListTools() []Tool { return h.tools }

func (h *stubHandler) CallTool(name string, args json.RawMessage) (*ToolResult, error) {
	if name == "fail_tool" {
		return nil, fmt.Errorf("tool failed")
	}
	return &ToolResult{Success: true, Message: "called " + name}, nil
}

func newStub() *stubHandler {
	return &stubHandler{
		resources: []Resource{
			{URI: "spec://test/full", Name: "Test spec", Content: "# Test\nHello world"},
		},
		tools: []Tool{
			{
				Name:        "spec_status",
				Description: "Check status",
				InputSchema: map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
				},
			},
		},
	}
}

// rpc sends a JSON-RPC request and returns the parsed response.
func rpc(t *testing.T, handler Handler, method string, id interface{}, params interface{}) response {
	t.Helper()

	msg := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
	}
	if id != nil {
		msg["id"] = id
	}
	if params != nil {
		msg["params"] = params
	}
	line, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	line = append(line, '\n')

	in := bytes.NewReader(line)
	var out bytes.Buffer
	log := io.Discard

	if err := Serve(context.Background(), handler, in, &out, log); err != nil {
		t.Fatalf("Serve: %v", err)
	}

	// Notifications produce no output
	if id == nil {
		if out.Len() > 0 {
			t.Fatalf("expected no response for notification, got: %s", out.String())
		}
		return response{}
	}

	var resp response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v (raw: %s)", err, out.String())
	}
	return resp
}

func TestInitialize(t *testing.T) {
	resp := rpc(t, newStub(), "initialize", 1, map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "test", "version": "1.0"},
	})

	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	result := resp.Result.(map[string]interface{})
	if result["protocolVersion"] != "2024-11-05" {
		t.Errorf("protocolVersion = %v, want 2024-11-05", result["protocolVersion"])
	}
	info := result["serverInfo"].(map[string]interface{})
	if info["name"] != "spec" {
		t.Errorf("serverInfo.name = %v, want spec", info["name"])
	}
}

func TestToolsList(t *testing.T) {
	resp := rpc(t, newStub(), "tools/list", 1, nil)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	result := resp.Result.(map[string]interface{})
	tools := result["tools"].([]interface{})
	if len(tools) != 1 {
		t.Fatalf("got %d tools, want 1", len(tools))
	}
	tool := tools[0].(map[string]interface{})
	if tool["name"] != "spec_status" {
		t.Errorf("tool name = %v, want spec_status", tool["name"])
	}
}

func TestToolsCall_Success(t *testing.T) {
	resp := rpc(t, newStub(), "tools/call", 1, map[string]interface{}{
		"name":      "spec_status",
		"arguments": map[string]interface{}{},
	})

	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	result := resp.Result.(map[string]interface{})
	content := result["content"].([]interface{})
	text := content[0].(map[string]interface{})["text"].(string)
	if text != "called spec_status" {
		t.Errorf("text = %q, want %q", text, "called spec_status")
	}
	if result["isError"] != false {
		t.Errorf("isError = %v, want false", result["isError"])
	}
}

func TestToolsCall_Error(t *testing.T) {
	resp := rpc(t, newStub(), "tools/call", 1, map[string]interface{}{
		"name":      "fail_tool",
		"arguments": map[string]interface{}{},
	})

	if resp.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: %s", resp.Error.Message)
	}

	result := resp.Result.(map[string]interface{})
	if result["isError"] != true {
		t.Errorf("isError = %v, want true", result["isError"])
	}
	content := result["content"].([]interface{})
	text := content[0].(map[string]interface{})["text"].(string)
	if !strings.Contains(text, "tool failed") {
		t.Errorf("text = %q, want it to contain 'tool failed'", text)
	}
}

func TestResourcesList(t *testing.T) {
	resp := rpc(t, newStub(), "resources/list", 1, nil)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	result := resp.Result.(map[string]interface{})
	resources := result["resources"].([]interface{})
	if len(resources) != 1 {
		t.Fatalf("got %d resources, want 1", len(resources))
	}
	r := resources[0].(map[string]interface{})
	if r["uri"] != "spec://test/full" {
		t.Errorf("uri = %v, want spec://test/full", r["uri"])
	}
	if r["mimeType"] != "text/markdown" {
		t.Errorf("mimeType = %v, want text/markdown", r["mimeType"])
	}
}

func TestResourcesRead_Found(t *testing.T) {
	resp := rpc(t, newStub(), "resources/read", 1, map[string]interface{}{
		"uri": "spec://test/full",
	})

	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	result := resp.Result.(map[string]interface{})
	contents := result["contents"].([]interface{})
	c := contents[0].(map[string]interface{})
	if c["text"] != "# Test\nHello world" {
		t.Errorf("text = %q, want %q", c["text"], "# Test\nHello world")
	}
}

func TestResourcesRead_NotFound(t *testing.T) {
	resp := rpc(t, newStub(), "resources/read", 1, map[string]interface{}{
		"uri": "spec://nonexistent",
	})

	if resp.Error == nil {
		t.Fatal("expected error for nonexistent resource")
	}
	if resp.Error.Code != -32602 {
		t.Errorf("error code = %d, want -32602", resp.Error.Code)
	}
}

func TestMethodNotFound(t *testing.T) {
	resp := rpc(t, newStub(), "bogus/method", 1, nil)

	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("error code = %d, want -32601", resp.Error.Code)
	}
}

func TestNotification_NoResponse(t *testing.T) {
	// Notification: no id field → no response
	rpc(t, newStub(), "notifications/initialized", nil, nil)
	// rpc helper asserts no output for nil id
}

func TestPing(t *testing.T) {
	resp := rpc(t, newStub(), "ping", 1, nil)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
	// ping returns empty object
	result := resp.Result.(map[string]interface{})
	if len(result) != 0 {
		t.Errorf("ping result should be empty, got %v", result)
	}
}

func TestMultipleMessages(t *testing.T) {
	// Send initialize + tools/list in one stream
	var input bytes.Buffer
	for i, method := range []string{"initialize", "tools/list"} {
		msg := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      i + 1,
			"method":  method,
		}
		if method == "initialize" {
			msg["params"] = map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]interface{}{},
			}
		}
		line, _ := json.Marshal(msg)
		input.Write(line)
		input.WriteByte('\n')
	}

	var out bytes.Buffer
	if err := Serve(context.Background(), newStub(), &input, &out, io.Discard); err != nil {
		t.Fatalf("Serve: %v", err)
	}

	// Should get two newline-delimited responses
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d responses, want 2:\n%s", len(lines), out.String())
	}

	var resp1, resp2 response
	json.Unmarshal([]byte(lines[0]), &resp1)
	json.Unmarshal([]byte(lines[1]), &resp2)

	// First is initialize
	r1 := resp1.Result.(map[string]interface{})
	if r1["protocolVersion"] != "2024-11-05" {
		t.Errorf("resp1: expected initialize response")
	}

	// Second is tools/list
	r2 := resp2.Result.(map[string]interface{})
	if _, ok := r2["tools"]; !ok {
		t.Errorf("resp2: expected tools/list response")
	}
}
