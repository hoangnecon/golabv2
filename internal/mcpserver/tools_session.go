package mcpserver

import (
	"context"
	"strings"
	"time"

	"github.com/hoangnecon/golabv2/internal/domain"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// SessionOnlyInput allows an optional per-call session_id override.
type SessionOnlyInput struct {
	SessionID string `json:"session_id,omitempty" jsonschema:"Optional locked session ID or gls_ session token override. Omit to use the connection's glp_/gls_ token."`
}

func (s *Server) colabGetSessionContext(ctx context.Context, req *mcp.CallToolRequest, input SessionOnlyInput) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	svcs, err := s.resolveServices(ctx, input.SessionID, false)
	if err != nil {
		return s.failTool(ctx, nil, err, time.Since(start).Milliseconds())
	}

	var ccu *domain.ComputeUnitsInfo
	if svcs.Telemetry != nil {
		ccu, _ = svcs.Telemetry.GetComputeUnits(ctx)
	}

	latency := time.Since(start).Milliseconds()
	return successResult(svcs.Target, map[string]any{
		"project":       svcs.Target.Project,
		"session":       svcs.Target.Session,
		"profile":       svcs.Target.Profile,
		"compute_units": ccu,
	}, latency, string(svcs.Target.Session.ConnectionState))
}

// ListAllSessionsInput lists all projects, profiles, and sessions without requiring a locked session.
type ListAllSessionsInput struct {
	ProjectID string `json:"project_id,omitempty" jsonschema:"Optional project UUID filter."`
}

func (s *Server) colabListAllSessions(ctx context.Context, req *mcp.CallToolRequest, input ListAllSessionsInput) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	if s.deps.WorkspaceSvc == nil {
		err := domain.NewAppError(domain.CodeInternalError, "workspace service is not initialized", "", 500)
		return s.failTool(ctx, nil, err, time.Since(start).Milliseconds())
	}

	snap, err := s.deps.WorkspaceSvc.GetSnapshot(ctx)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return s.failTool(ctx, nil, err, latency)
	}

	sessions := snap.Sessions
	filterProj := strings.TrimSpace(input.ProjectID)
	if filterProj != "" {
		filtered := make([]*domain.Session, 0, len(sessions))
		for _, sess := range sessions {
			if sess.ProjectID == filterProj {
				filtered = append(filtered, sess)
			}
		}
		sessions = filtered
	}

	return successResult(nil, map[string]any{
		"projects":       snap.Projects,
		"profiles":       snap.Profiles,
		"sessions":       sessions,
		"total_sessions": len(sessions),
	}, latency, "")
}

func (s *Server) colabOpenSessionTab(ctx context.Context, req *mcp.CallToolRequest, input SessionOnlyInput) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	svcs, err := s.resolveServices(ctx, input.SessionID, false)
	if err != nil {
		return s.failTool(ctx, nil, err, time.Since(start).Milliseconds())
	}
	if s.deps.SessionSvc == nil {
		err := domain.NewAppError(domain.CodeInternalError, "session service is not initialized", "", 500)
		return s.failTool(ctx, svcs.Target, err, time.Since(start).Milliseconds())
	}

	updated, err := s.deps.SessionSvc.OpenTab(ctx, svcs.Target.Session.ID)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return s.failTool(ctx, svcs.Target, err, latency)
	}
	return successResult(svcs.Target, map[string]any{
		"opened":  true,
		"session": updated,
	}, latency, string(updated.ConnectionState))
}

// GetAccountQuotaInput specifies an optional profile_id or session_id.
type GetAccountQuotaInput struct {
	SessionID string `json:"session_id,omitempty" jsonschema:"Optional locked session ID override."`
	ProfileID string `json:"profile_id,omitempty" jsonschema:"Optional Google Account profile ID. Defaults to the resolved session's account profile."`
}

func (s *Server) colabGetAccountQuota(ctx context.Context, req *mcp.CallToolRequest, input GetAccountQuotaInput) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	profileID := strings.TrimSpace(input.ProfileID)
	var target *domain.ResolvedTarget

	if profileID == "" {
		svcs, err := s.resolveServices(ctx, input.SessionID, false)
		if err != nil {
			return s.failTool(ctx, nil, err, time.Since(start).Milliseconds())
		}
		target = svcs.Target
		profileID = target.Profile.ID
	} else {
		setCallAuditIDs(ctx, "", "", profileID)
	}

	if s.deps.ProfileSvc == nil {
		err := domain.NewAppError(domain.CodeInternalError, "profile service is not initialized", "", 500)
		return s.failTool(ctx, target, err, time.Since(start).Milliseconds())
	}
	quota, err := s.deps.ProfileSvc.GetQuota(ctx, profileID)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return s.failTool(ctx, target, err, latency)
	}
	return successResult(target, quota, latency, "")
}
