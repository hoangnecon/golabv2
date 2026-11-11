package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hoangnecon/golabv2/internal/domain"
)

// ProjectService handles canonical Notebook Projects, project tokens, and initial owner session creation.
type ProjectService struct {
	store domain.Store
	coord domain.SessionCoordinator
	bus   domain.EventBus
}

func NewProjectService(store domain.Store, coord domain.SessionCoordinator, bus domain.EventBus) *ProjectService {
	return &ProjectService{
		store: store,
		coord: coord,
		bus:   bus,
	}
}

func (s *ProjectService) publish(evtType domain.EventType, data any) {
	if s.bus == nil {
		return
	}
	s.bus.Publish(domain.WorkspaceEvent{
		Type:      evtType,
		Timestamp: time.Now().UTC(),
		Data:      data,
	})
}

func (s *ProjectService) CreateProject(ctx context.Context, name, rawNotebookURL, ownerProfileID string, autoLock bool) (*domain.Project, *domain.Session, error) {
	name = strings.TrimSpace(name)
	rawNotebookURL = strings.TrimSpace(rawNotebookURL)
	ownerProfileID = strings.TrimSpace(ownerProfileID)
	if ownerProfileID == "" {
		ownerProfileID = "default"
	}

	nbID, canonicalURL, err := domain.NormalizeNotebookURL(rawNotebookURL)
	if err != nil {
		return nil, nil, err
	}
	if name == "" {
		parts := strings.Split(nbID, "/")
		name = "Notebook " + parts[len(parts)-1]
	}

	ownerProf, err := s.store.GetProfile(ctx, ownerProfileID)
	if err != nil {
		return nil, nil, domain.NewAppError(domain.CodeNotFound, fmt.Sprintf("owner profile %q not found", ownerProfileID), "", http.StatusNotFound)
	}

	tok, err := domain.GenerateToken("glp_")
	if err != nil {
		return nil, nil, err
	}

	now := time.Now().UTC()
	p := &domain.Project{
		ID:             uuid.New().String(),
		Name:           name,
		NotebookID:     nbID,
		NotebookURL:    canonicalURL,
		OwnerProfileID: ownerProf.ID,
		ProjectToken:   tok,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := s.store.CreateProject(ctx, p); err != nil {
		return nil, nil, err
	}

	// Auto-create initial (Project, Owner Account) Session binding
	sessTok, err := domain.GenerateToken("gls_")
	if err != nil {
		return p, nil, err
	}
	ownerSess := &domain.Session{
		ID:              uuid.New().String(),
		ProjectID:       p.ID,
		ProfileID:       ownerProf.ID,
		Name:            fmt.Sprintf("%s — %s", p.Name, ownerProf.Name),
		NotebookID:      p.NotebookID,
		NotebookURL:     p.NotebookURL,
		SessionToken:    sessTok,
		Permission:      domain.PermissionUnknown,
		ConnectionState: domain.StateBrowserStopped,
		CreatedAt:       now,
	}
	if err := s.store.CreateSession(ctx, ownerSess); err != nil {
		return p, nil, err
	}

	if autoLock && s.coord != nil {
		if locked, errL := s.coord.AcquireLock(ctx, ownerSess.ID, false, true); errL == nil && locked != nil {
			ownerSess = locked
			if refreshed, errP := s.store.GetProject(ctx, p.ID); errP == nil && refreshed != nil {
				p = refreshed
			}
		}
	}

	s.publish(domain.EventProjectUpdated, p)
	s.publish(domain.EventSessionUpdated, ownerSess)
	return p, ownerSess, nil
}

func (s *ProjectService) GetProject(ctx context.Context, id string) (*domain.Project, error) {
	return s.store.GetProject(ctx, id)
}

func (s *ProjectService) ListProjects(ctx context.Context) ([]*domain.Project, error) {
	return s.store.ListProjects(ctx)
}

type ProjectPatch struct {
	Name           *string `json:"name,omitempty"`
	OwnerProfileID *string `json:"owner_profile_id,omitempty"`
}

func (s *ProjectService) UpdateProject(ctx context.Context, id string, patch ProjectPatch) (*domain.Project, error) {
	p, err := s.store.GetProject(ctx, id)
	if err != nil {
		return nil, err
	}
	if patch.Name != nil {
		trimmed := strings.TrimSpace(*patch.Name)
		if trimmed == "" {
			return nil, domain.NewAppError(domain.CodeInvalidInput, "project name cannot be empty", "", http.StatusBadRequest)
		}
		p.Name = trimmed
	}
	if patch.OwnerProfileID != nil && strings.TrimSpace(*patch.OwnerProfileID) != "" {
		if _, err := s.store.GetProfile(ctx, strings.TrimSpace(*patch.OwnerProfileID)); err != nil {
			return nil, err
		}
		p.OwnerProfileID = strings.TrimSpace(*patch.OwnerProfileID)
	}
	p.UpdatedAt = time.Now().UTC()
	if err := s.store.UpdateProject(ctx, p); err != nil {
		return nil, err
	}
	s.publish(domain.EventProjectUpdated, p)
	return p, nil
}

func (s *ProjectService) RotateToken(ctx context.Context, id string) (*domain.Project, error) {
	if _, err := s.store.RotateProjectToken(ctx, id); err != nil {
		return nil, err
	}
	p, err := s.store.GetProject(ctx, id)
	if err != nil {
		return nil, err
	}
	s.publish(domain.EventProjectUpdated, p)
	return p, nil
}

func (s *ProjectService) UnlockProject(ctx context.Context, id string) (*domain.Project, error) {
	if s.coord != nil {
		if err := s.coord.ReleaseLock(ctx, id); err != nil {
			return nil, err
		}
	} else if err := s.store.ReleaseProjectLock(ctx, id); err != nil {
		return nil, err
	}
	return s.store.GetProject(ctx, id)
}

func (s *ProjectService) DeleteProject(ctx context.Context, id string, force bool) error {
	p, err := s.store.GetProject(ctx, id)
	if err != nil {
		return err
	}
	if p.LockedSessionID != "" {
		if !force {
			return domain.NewAppError(
				domain.CodeConflict,
				"project currently has an active locked session",
				"Unlock the project first or pass force=true.",
				http.StatusConflict,
			)
		}
		if s.coord != nil {
			_ = s.coord.ReleaseLock(ctx, id)
		} else {
			_ = s.store.ReleaseProjectLock(ctx, id)
		}
	}

	sessions, _ := s.store.ListSessions(ctx, id)
	for _, sess := range sessions {
		if s.coord != nil {
			_ = s.coord.DetachSession(ctx, sess.ID, false)
		}
	}

	if err := s.store.DeleteProject(ctx, id); err != nil {
		return err
	}
	for _, sess := range sessions {
		s.publish(domain.EventSessionDeleted, map[string]string{"id": sess.ID, "project_id": id})
	}
	s.publish(domain.EventProjectDeleted, map[string]string{"id": id})
	return nil
}
