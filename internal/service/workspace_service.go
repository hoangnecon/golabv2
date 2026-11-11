package service

import (
	"context"
	"time"

	"github.com/hoangnecon/golabv2/internal/domain"
)

// WorkspaceService produces atomic bootstrap snapshots for GET /api/v1/workspace and SSE initial frames.
type WorkspaceService struct {
	store         domain.Store
	browser       domain.BrowserManager
	daemonVersion string
	mcpHTTPURL    string
}

func NewWorkspaceService(store domain.Store, browser domain.BrowserManager, daemonVersion string, mcpHTTPURL string) *WorkspaceService {
	return &WorkspaceService{
		store:         store,
		browser:       browser,
		daemonVersion: daemonVersion,
		mcpHTTPURL:    mcpHTTPURL,
	}
}

func (s *WorkspaceService) GetSnapshot(ctx context.Context) (*domain.WorkspaceSnapshot, error) {
	profiles, err := s.store.ListProfiles(ctx)
	if err != nil {
		return nil, err
	}
	if s.browser != nil {
		for _, p := range profiles {
			p.IsRunning = s.browser.IsProfileRunning(ctx, p.CDPPort)
		}
	}

	projects, err := s.store.ListProjects(ctx)
	if err != nil {
		return nil, err
	}

	sessions, err := s.store.ListSessions(ctx, "")
	if err != nil {
		return nil, err
	}

	recent, _, err := s.store.ListActivity(ctx, domain.ActivityFilter{Limit: 30})
	if err != nil {
		return nil, err
	}

	count, _ := s.store.CountActivity(ctx)

	if profiles == nil {
		profiles = []*domain.Profile{}
	}
	if projects == nil {
		projects = []*domain.Project{}
	}
	if sessions == nil {
		sessions = []*domain.Session{}
	}
	if recent == nil {
		recent = []*domain.ActivityLog{}
	}

	return &domain.WorkspaceSnapshot{
		Profiles:        profiles,
		Projects:        projects,
		Sessions:        sessions,
		RecentActivity:  recent,
		RecentLogsCount: count,
		DaemonVersion:   s.daemonVersion,
		MCPHTTPURL:      s.mcpHTTPURL,
		Timestamp:       time.Now().UTC(),
	}, nil
}
