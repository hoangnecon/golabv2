package domain

import (
	"context"
	"time"
)

// PermissionLevel represents granular Google Drive / Colab permissions for an account on a notebook.
type PermissionLevel string

const (
	PermissionOwner        PermissionLevel = "owner"
	PermissionEditor       PermissionLevel = "editor"
	PermissionCommenter    PermissionLevel = "commenter"
	PermissionViewOnly     PermissionLevel = "view_only"
	PermissionDenied       PermissionLevel = "denied"
	PermissionAuthRequired PermissionLevel = "auth_required"
	PermissionUnknown      PermissionLevel = "unknown"
)

// CanWrite returns true only when the account has write permission on the notebook.
func (p PermissionLevel) CanWrite() bool {
	return p == PermissionOwner || p == PermissionEditor
}

// ConnectionState represents the layered state of a Session from Chrome process down to Python kernel.
type ConnectionState string

const (
	StateBrowserStopped      ConnectionState = "browser_stopped"
	StateTabClosed           ConnectionState = "tab_closed"
	StateAuthRequired        ConnectionState = "auth_required"
	StatePermissionDenied    ConnectionState = "permission_denied"
	StateRuntimeDisconnected ConnectionState = "runtime_disconnected"
	StateConnecting          ConnectionState = "connecting"
	StateIdle                ConnectionState = "idle"
	StateBusy                ConnectionState = "busy"
)

// IsKernelConnected reports whether the Colab VM runtime is attached and ready for XHR/kernel operations.
func (s ConnectionState) IsKernelConnected() bool {
	return s == StateIdle || s == StateBusy
}

// Session represents a (Project, Account Profile) binding.
type Session struct {
	ID              string          `json:"id"`
	ProjectID       string          `json:"project_id"`
	ProfileID       string          `json:"profile_id"`
	Name            string          `json:"name"`
	NotebookID      string          `json:"notebook_id"`
	NotebookURL     string          `json:"notebook_url"`
	SessionToken    string          `json:"session_token"`
	TabID           string          `json:"tab_id"`
	IsLocked        bool            `json:"is_locked"`
	Permission      PermissionLevel `json:"permission"`
	ConnectionState ConnectionState `json:"connection_state"`
	CreatedAt       time.Time       `json:"created_at"`
	LastAttachedAt  *time.Time      `json:"last_attached_at,omitempty"`
}

// NotebookIdentity captures user & Drive permission metadata extracted from window.colab.global.notebookModel.
type NotebookIdentity struct {
	EmailAddress         string          `json:"email_address"`
	DisplayName          string          `json:"display_name"`
	IsAuthenticated      bool            `json:"is_authenticated"`
	DriveFileID          string          `json:"drive_file_id,omitempty"`
	Title                string          `json:"title"`
	Role                 string          `json:"role"`
	Permission           PermissionLevel `json:"permission"`
	Editable             bool            `json:"editable"`
	Saveable             bool            `json:"saveable"`
	IsSoleAuthored       bool            `json:"is_sole_authored"`
	HasOtherWriters      bool            `json:"has_other_writers"`
	HasMergeConflict     bool            `json:"has_merge_conflict"`
	HasUnsavedEdits      bool            `json:"has_unsaved_edits"`
	AcceleratorName      string          `json:"accelerator_name,omitempty"`
	RuntimeUptimeSeconds int64           `json:"runtime_uptime_seconds,omitempty"`
	RuntimeShape         string          `json:"runtime_shape,omitempty"`
}

// ObservationSnapshot is a complete read-only snapshot of a session for the Web UI and SSE stream.
type ObservationSnapshot struct {
	SessionID         string              `json:"session_id"`
	ProjectID         string              `json:"project_id"`
	ProfileID         string              `json:"profile_id"`
	IsLocked          bool                `json:"is_locked"`
	ConnectionState   ConnectionState     `json:"connection_state"`
	Permission        PermissionLevel     `json:"permission"`
	NotebookMetadata  *NotebookIdentity   `json:"notebook_metadata,omitempty"`
	ComputeUnits      *ComputeUnitsInfo   `json:"compute_units,omitempty"`
	HardwareTelemetry *HardwareTelemetry  `json:"hardware_telemetry,omitempty"`
	DriveStatus       *DriveStatus        `json:"drive_status,omitempty"`
	CellsOutline      *NotebookOutline    `json:"cells_outline,omitempty"`
	RunningCells      *RunningCellsReport `json:"running_cells,omitempty"`
	ProxyInfo         *ProxyEndpoint      `json:"proxy_info,omitempty"`
	ObservedAt        time.Time           `json:"observed_at"`
}

// SessionRepository defines persistence operations for Sessions.
type SessionRepository interface {
	CreateSession(ctx context.Context, sess *Session) error
	GetSession(ctx context.Context, id string) (*Session, error)
	GetSessionByPair(ctx context.Context, projectID, profileID string) (*Session, error)
	GetSessionByToken(ctx context.Context, token string) (*Session, error)
	ListSessions(ctx context.Context, projectID string) ([]*Session, error)
	UpdateSessionState(ctx context.Context, id string, tabID string, perm PermissionLevel, state ConnectionState) error
	DeleteSession(ctx context.Context, id string) error
}
