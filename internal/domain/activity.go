package domain

import (
	"context"
	"encoding/json"
	"time"
)

// ActivityLog records an MCP tool invocation for audit streaming and filtering.
type ActivityLog struct {
	ID           int64     `json:"id"`
	SessionID    string    `json:"session_id"`
	ProjectID    string    `json:"project_id"`
	ProfileID    string    `json:"profile_id"`
	TokenPrefix  string    `json:"token_prefix"`
	ToolName     string    `json:"tool_name"`
	Status       string    `json:"status"` // "success" or "error"
	ErrorCode    string    `json:"error_code"`
	ErrorMessage string    `json:"error_message"`
	LatencyMs    int64     `json:"latency_ms"`
	HasPayload   bool      `json:"has_payload"`
	CreatedAt    time.Time `json:"created_at"`
}

// Payload is a captured tool request or response JSON body.
type Payload struct {
	Body      json.RawMessage `json:"body"`
	Bytes     int             `json:"bytes"`
	Truncated bool            `json:"truncated"`
}

// ActivityRecord is a tool call to persist together with its captured payloads.
type ActivityRecord struct {
	SessionID    string
	ProjectID    string
	ProfileID    string
	TokenPrefix  string
	ToolName     string
	Status       string
	ErrorCode    string
	ErrorMessage string
	LatencyMs    int64
	Request      *Payload
	Response     *Payload
	CreatedAt    time.Time
}

// ActivityDetail is one tool call with its captured request and response payloads.
type ActivityDetail struct {
	ActivityLog
	Request  *Payload `json:"request"`
	Response *Payload `json:"response"`
}

// ActivityFilter specifies cursor pagination and faceted filters for audit logs.
type ActivityFilter struct {
	Cursor    int64  `json:"cursor,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	ProfileID string `json:"profile_id,omitempty"`
	Status    string `json:"status,omitempty"`
	ToolName  string `json:"tool_name,omitempty"`
}

// ActivityRepository defines audit logging persistence.
type ActivityRepository interface {
	AppendActivity(ctx context.Context, rec *ActivityRecord) (*ActivityLog, error)
	ListActivity(ctx context.Context, filter ActivityFilter) (items []*ActivityLog, nextCursor int64, err error)
	GetActivity(ctx context.Context, id int64) (*ActivityDetail, error)
	CountActivity(ctx context.Context) (int64, error)
}
