package domain

import (
	"context"
	"time"
)

// EventType enumerates real-time typed SSE workspace events.
type EventType string

const (
	EventWorkspaceSnapshot EventType = "workspace.snapshot"
	EventProfileUpdated    EventType = "profile.updated"
	EventProfileDeleted    EventType = "profile.deleted"
	EventProjectUpdated    EventType = "project.updated"
	EventProjectDeleted    EventType = "project.deleted"
	EventSessionUpdated    EventType = "session.updated"
	EventSessionDeleted    EventType = "session.deleted"
	EventTelemetryUpdated  EventType = "telemetry.updated"
	EventActivityAppended  EventType = "activity.appended"
)

// WorkspaceEvent is pushed over GET /api/v1/events SSE stream.
type WorkspaceEvent struct {
	Type      EventType `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Data      any       `json:"data"`
}

// EventBus broadcasts typed WorkspaceEvents to SSE subscribers.
type EventBus interface {
	Publish(evt WorkspaceEvent)
	Subscribe(ctx context.Context) <-chan WorkspaceEvent
}

// ResolvedTarget is the validated session/project/profile/driver bundle resolved for an MCP tool call.
type ResolvedTarget struct {
	Project *Project
	Session *Session
	Profile *Profile
	Driver  RuntimeDriver
}

// SessionCoordinator manages per-project exclusive locks, tab attachment, token routing, and live observation.
type SessionCoordinator interface {
	AcquireLock(ctx context.Context, sessionID string, forceSwitch bool, openTab bool) (*Session, error)
	ReleaseLock(ctx context.Context, projectID string) error
	ReleaseSessionLock(ctx context.Context, sessionID string) error
	OpenSessionTab(ctx context.Context, sessionID string) (*Session, error)
	DetachSession(ctx context.Context, sessionID string, closeTab bool) error
	ResolveMCPTarget(ctx context.Context, connToken string, explicitSessionID string, requireWrite bool) (*ResolvedTarget, error)
	ObserveSession(ctx context.Context, sessionID string) (*ObservationSnapshot, error)
	RefreshAccountQuota(ctx context.Context, profileID string) (*AccountQuota, error)
	StartReconciler(ctx context.Context)
	Close() error
}

// Store unifies all persistent SQLite WAL repositories and atomic lock transactions.
type Store interface {
	Close() error

	// Profiles
	CreateProfile(ctx context.Context, p *Profile) error
	GetProfile(ctx context.Context, id string) (*Profile, error)
	ListProfiles(ctx context.Context) ([]*Profile, error)
	UpdateProfile(ctx context.Context, p *Profile) error
	UpdateProfileRuntimeTelemetry(ctx context.Context, id string, email, displayName string, tier ProfileTier, balance, burnRate float64, eligibleGPUs, eligibleTPUs []string) error
	SetProfileChromePID(ctx context.Context, id string, pid int, cdpPort int) error
	DeleteProfile(ctx context.Context, id string) error
	GetNextAvailableCDPPort(ctx context.Context, startPort int) (int, error)

	// Projects
	CreateProject(ctx context.Context, p *Project) error
	GetProject(ctx context.Context, id string) (*Project, error)
	GetProjectByNotebookID(ctx context.Context, notebookID string) (*Project, error)
	GetProjectByToken(ctx context.Context, token string) (*Project, error)
	ListProjects(ctx context.Context) ([]*Project, error)
	UpdateProject(ctx context.Context, p *Project) error
	RotateProjectToken(ctx context.Context, id string) (string, error)
	DeleteProject(ctx context.Context, id string) error

	// Sessions & Per-Project Exclusive Lock
	CreateSession(ctx context.Context, sess *Session) error
	GetSession(ctx context.Context, id string) (*Session, error)
	GetSessionByPair(ctx context.Context, projectID, profileID string) (*Session, error)
	GetSessionByToken(ctx context.Context, token string) (*Session, error)
	ListSessions(ctx context.Context, projectID string) ([]*Session, error)
	UpdateSessionState(ctx context.Context, id string, tabID string, perm PermissionLevel, state ConnectionState) error
	RotateSessionToken(ctx context.Context, id string) (string, error)
	DeleteSession(ctx context.Context, id string) error

	AcquireProjectLock(ctx context.Context, sessionID string, forceSwitch bool) (prevLocked *Session, updatedProject *Project, lockedSession *Session, err error)
	ReleaseProjectLock(ctx context.Context, projectID string) error
	ListLockedSessions(ctx context.Context) ([]*Session, error)

	// Activity Audit Logs
	AppendActivity(ctx context.Context, rec *ActivityRecord) (*ActivityLog, error)
	ListActivity(ctx context.Context, filter ActivityFilter) (items []*ActivityLog, nextCursor int64, err error)
	GetActivity(ctx context.Context, id int64) (*ActivityDetail, error)
	CountActivity(ctx context.Context) (int64, error)
}
