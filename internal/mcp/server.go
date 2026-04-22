package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// Resource represents content served by the MCP server.
type Resource struct {
	URI     string
	Name    string
	Content string
}

// Tool represents a tool the agent can call.
type Tool struct {
	Name        string
	Description string
	InputSchema map[string]interface{}
}

// ToolResult represents the outcome of a tool call.
type ToolResult struct {
	Success bool
	Message string
}

// Handler defines the domain operations the MCP server delegates to.
// Implemented by a thin adapter over build.MCPServer in cmd/.
type Handler interface {
	ListResources() []Resource
	GetResource(uri string) (*Resource, error)
	ListTools() []Tool
	CallTool(name string, args json.RawMessage) (*ToolResult, error)
}

// server holds protocol state and the domain handler.
type server struct {
	handler Handler
	out     io.Writer
	mu      sync.Mutex
	log     io.Writer // stderr for diagnostics
}

// Serve runs the MCP server, reading JSON-RPC from in and writing to out.
// Diagnostic logging goes to logOut (typically os.Stderr). Blocks until
// ctx is cancelled or in reaches EOF.
func Serve(ctx context.Context, handler Handler, in io.Reader, out io.Writer, logOut io.Writer) error {
	s := &server{handler: handler, out: out, log: logOut}
	fmt.Fprintf(s.log, "spec mcp: server starting\n")

	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			s.writeError(nil, -32700, "parse error", err.Error())
			continue
		}

		if req.isNotification() {
			continue
		}

		s.dispatch(&req)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading input: %w", err)
	}
	return nil
}

func (s *server) dispatch(req *request) {
	switch req.Method {
	case "initialize":
		s.handleInitialize(req)
	case "ping":
		s.writeResult(req.ID, map[string]interface{}{})
	case "tools/list":
		s.handleToolsList(req)
	case "tools/call":
		s.handleToolsCall(req)
	case "resources/list":
		s.handleResourcesList(req)
	case "resources/read":
		s.handleResourcesRead(req)
	default:
		s.writeError(req.ID, -32601, "method not found", req.Method)
	}
}

func (s *server) handleInitialize(req *request) {
	s.writeResult(req.ID, map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]interface{}{
			"tools":     map[string]interface{}{},
			"resources": map[string]interface{}{},
		},
		"serverInfo": map[string]interface{}{
			"name":    "spec",
			"version": "0.3.0",
		},
	})
}

func (s *server) handleToolsList(req *request) {
	tools := s.handler.ListTools()
	out := make([]map[string]interface{}, len(tools))
	for i, t := range tools {
		out[i] = map[string]interface{}{
			"name":        t.Name,
			"description": t.Description,
			"inputSchema": t.InputSchema,
		}
	}
	s.writeResult(req.ID, map[string]interface{}{"tools": out})
}

func (s *server) handleToolsCall(req *request) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.writeError(req.ID, -32602, "invalid params", err.Error())
		return
	}

	result, err := s.handler.CallTool(params.Name, params.Arguments)
	if err != nil {
		s.writeResult(req.ID, map[string]interface{}{
			"content": []map[string]interface{}{
				{"type": "text", "text": fmt.Sprintf("error: %v", err)},
			},
			"isError": true,
		})
		return
	}

	s.writeResult(req.ID, map[string]interface{}{
		"content": []map[string]interface{}{
			{"type": "text", "text": result.Message},
		},
		"isError": !result.Success,
	})
}

func (s *server) handleResourcesList(req *request) {
	resources := s.handler.ListResources()
	out := make([]map[string]interface{}, len(resources))
	for i, r := range resources {
		out[i] = map[string]interface{}{
			"uri":      r.URI,
			"name":     r.Name,
			"mimeType": "text/markdown",
		}
	}
	s.writeResult(req.ID, map[string]interface{}{"resources": out})
}

func (s *server) handleResourcesRead(req *request) {
	var params struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.writeError(req.ID, -32602, "invalid params", err.Error())
		return
	}

	resource, err := s.handler.GetResource(params.URI)
	if err != nil {
		s.writeError(req.ID, -32602, "resource not found", err.Error())
		return
	}

	s.writeResult(req.ID, map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"uri":      resource.URI,
				"mimeType": "text/markdown",
				"text":     resource.Content,
			},
		},
	})
}

// writeResult sends a successful JSON-RPC response.
func (s *server) writeResult(id json.RawMessage, result interface{}) {
	s.write(response{JSONRPC: "2.0", ID: id, Result: result})
}

// writeError sends a JSON-RPC error response.
func (s *server) writeError(id json.RawMessage, code int, message, data string) {
	resp := response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcError{Code: code, Message: message},
	}
	if data != "" {
		resp.Error.Data = data
	}
	s.write(resp)
}

func (s *server) write(resp response) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.Marshal(resp)
	if err != nil {
		fmt.Fprintf(s.log, "spec mcp: marshal error: %v\n", err)
		return
	}
	data = append(data, '\n')
	if _, err := s.out.Write(data); err != nil {
		fmt.Fprintf(s.log, "spec mcp: write error: %v\n", err)
	}
}
