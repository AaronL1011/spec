// Package mcp implements the MCP (Model Context Protocol) stdio transport.
// It serves spec context and tools to MCP-compatible coding agents over
// JSON-RPC 2.0 on stdin/stdout.
package mcp

import "encoding/json"

// request is an incoming JSON-RPC 2.0 message.
type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// response is an outgoing JSON-RPC 2.0 message.
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// rpcError is a JSON-RPC 2.0 error object.
type rpcError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// isNotification returns true if the message has no id (JSON-RPC notification).
func (r *request) isNotification() bool {
	return len(r.ID) == 0 || string(r.ID) == "null"
}
