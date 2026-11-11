package mcpserver

import (
	"encoding/json"
	"time"

	"github.com/hoangnecon/golabv2/internal/domain"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// EnvelopeError represents a structured error block inside ResponseEnvelope.
type EnvelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

// ResponseMetadata tracks execution, target governance, and runtime state for agent context.
type ResponseMetadata struct {
	LatencyMs   int64  `json:"latency_ms"`
	ProjectID   string `json:"project_id,omitempty"`
	SessionID   string `json:"session_id,omitempty"`
	ProfileID   string `json:"profile_id,omitempty"`
	KernelState string `json:"kernel_state,omitempty"`
	Permission  string `json:"permission,omitempty"`
	Timestamp   string `json:"timestamp"`
}

// ResponseEnvelope is the standardized payload returned by all 43 GoLab v2 MCP tools.
type ResponseEnvelope struct {
	Status   string           `json:"status"` // "success" or "error"
	Data     any              `json:"data,omitempty"`
	Error    *EnvelopeError   `json:"error,omitempty"`
	Metadata ResponseMetadata `json:"metadata"`
}

func targetMetadata(t *domain.ResolvedTarget, latencyMs int64, kernelStateOverride string) ResponseMetadata {
	meta := ResponseMetadata{
		LatencyMs: latencyMs,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if t != nil {
		if t.Project != nil {
			meta.ProjectID = t.Project.ID
		}
		if t.Session != nil {
			meta.SessionID = t.Session.ID
			meta.KernelState = string(t.Session.ConnectionState)
			meta.Permission = string(t.Session.Permission)
		}
		if t.Profile != nil {
			meta.ProfileID = t.Profile.ID
		}
	}
	if kernelStateOverride != "" {
		meta.KernelState = kernelStateOverride
	}
	return meta
}

func successResult(t *domain.ResolvedTarget, data any, latencyMs int64, kernelState string) (*mcp.CallToolResult, any, error) {
	envelope := ResponseEnvelope{
		Status:   "success",
		Data:     data,
		Metadata: targetMetadata(t, latencyMs, kernelState),
	}

	dataBytes, _ := json.MarshalIndent(envelope, "", "  ")
	var structured map[string]any
	_ = json.Unmarshal(dataBytes, &structured)

	return &mcp.CallToolResult{
		Content:           []mcp.Content{&mcp.TextContent{Text: string(dataBytes)}},
		StructuredContent: structured,
	}, nil, nil
}

func errorResult(t *domain.ResolvedTarget, err error, latencyMs int64) (*mcp.CallToolResult, any, error) {
	return errorResultWithData(t, nil, err, latencyMs)
}

func errorResultWithData(t *domain.ResolvedTarget, data any, err error, latencyMs int64) (*mcp.CallToolResult, any, error) {
	appErr := domain.AsAppError(err)
	if appErr == nil {
		appErr = domain.NewAppError(domain.CodeInternalError, "unknown error", "", 500)
	}

	envelope := ResponseEnvelope{
		Status: "error",
		Data:   data,
		Error: &EnvelopeError{
			Code:    appErr.Code,
			Message: appErr.Message,
			Hint:    appErr.Hint,
		},
		Metadata: targetMetadata(t, latencyMs, ""),
	}

	dataBytes, _ := json.MarshalIndent(envelope, "", "  ")
	var structured map[string]any
	_ = json.Unmarshal(dataBytes, &structured)

	return &mcp.CallToolResult{
		Content:           []mcp.Content{&mcp.TextContent{Text: string(dataBytes)}},
		StructuredContent: structured,
		IsError:           true,
	}, nil, nil
}
