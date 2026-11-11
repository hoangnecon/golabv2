package mcpserver

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ProxyForwardInput specifies reverse proxy target inside Colab VM.
type ProxyForwardInput struct {
	SessionID string            `json:"session_id,omitempty" jsonschema:"Optional locked session ID override."`
	Port      int               `json:"port" jsonschema:"Internal port on the Colab VM to route request to (e.g. 7860, 8000, 6006). Required."`
	Subpath   string            `json:"subpath,omitempty" jsonschema:"Subpath to request (e.g. 'api/status' or empty for root)."`
	Method    string            `json:"method,omitempty" jsonschema:"HTTP method ('GET', 'POST', 'PUT', 'DELETE'). Default 'GET'."`
	Headers   map[string]string `json:"headers,omitempty" jsonschema:"Optional HTTP request headers."`
	Body      string            `json:"body,omitempty" jsonschema:"Optional HTTP request body payload."`
}

func (s *Server) colabProxyForward(ctx context.Context, req *mcp.CallToolRequest, input ProxyForwardInput) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	svcs, err := s.resolveServices(ctx, input.SessionID, false)
	if err != nil {
		return s.failTool(ctx, nil, err, time.Since(start).Milliseconds())
	}
	resp, err := svcs.Proxy.ProxyForward(ctx, input.Port, input.Subpath, input.Method, input.Headers, input.Body)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return s.failTool(ctx, svcs.Target, err, latency)
	}
	return successResult(svcs.Target, resp, latency, "")
}

// GetProxyURLInput retrieves the external proxy URL and token for a Colab VM port.
type GetProxyURLInput struct {
	SessionID string `json:"session_id,omitempty" jsonschema:"Optional locked session ID override."`
	Port      int    `json:"port" jsonschema:"Port listening inside the Colab VM (1-65535). Required."`
}

func (s *Server) colabGetProxyURL(ctx context.Context, req *mcp.CallToolRequest, input GetProxyURLInput) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	svcs, err := s.resolveServices(ctx, input.SessionID, false)
	if err != nil {
		return s.failTool(ctx, nil, err, time.Since(start).Milliseconds())
	}
	ep, err := svcs.Proxy.GetProxyURL(ctx, input.Port)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return s.failTool(ctx, svcs.Target, err, latency)
	}
	return successResult(svcs.Target, ep, latency, "")
}
