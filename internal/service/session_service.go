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

// SessionService governs (Project, Account) Session bindings, Per-Project Locks, tokens, and read-only observation.
type SessionService struct {
	store domain.Store
	coord domain.SessionCoordinator
	bus   domain.EventBus
}

func NewSessionService(store domain.Store, coord domain.SessionCoordinator, bus domain.EventBus) *SessionService {
	return &SessionService{
		store: store,
		coord: coord,
		bus:   bus,
	}
}

func (s *SessionService) publish(evtType domain.EventType, data any) {
	if s.bus == nil {
		return
	}
	s.bus.Publish(domain.WorkspaceEvent{
		Type:      evtType,
		Timestamp: time.Now().UTC(),
		Data:      data,
	})
}

func (s *SessionService) ListSessions(ctx context.Context, projectID string) ([]*domain.Session, error) {
	return s.store.ListSessions(ctx, strings.TrimSpace(projectID))
}

func (s *SessionService) GetSession(ctx context.Context, id string) (*domain.Session, error) {
	return s.store.GetSession(ctx, id)
}

func (s *SessionService) CreateSession(ctx context.Context, projectID, profileID, name string, autoLock bool) (*domain.Session, error) {
	projectID = strings.TrimSpace(projectID)
	profileID = strings.TrimSpace(profileID)
	name = strings.TrimSpace(name)

	if projectID == "" || profileID == "" {
		return nil, domain.NewAppError(domain.CodeInvalidInput, "project_id and profile_id are required", "", http.StatusBadRequest)
	}

	proj, err := s.store.GetProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	prof, err := s.store.GetProfile(ctx, profileID)
	if err != nil {
		return nil, err
	}

	if existing, errE := s.store.GetSessionByPair(ctx, projectID, profileID); errE == nil && existing != nil {
		return nil, domain.NewAppError(
			domain.CodeConflict,
			fmt.Sprintf("account %q is already attached to project %q", prof.Name, proj.Name),
			"Use the existing session row in the Project Lock Matrix.",
			http.StatusConflict,
		)
	}

	if name == "" {
		name = fmt.Sprintf("%s — %s", proj.Name, prof.Name)
	}
	tok, err := domain.GenerateToken("gls_")
	if err != nil {
		return nil, err
	}

	sess := &domain.Session{
		ID:              uuid.New().String(),
		ProjectID:       proj.ID,
		ProfileID:       prof.ID,
		Name:            name,
		NotebookID:      proj.NotebookID,
		NotebookURL:     proj.NotebookURL,
		SessionToken:    tok,
		Permission:      domain.PermissionUnknown,
		ConnectionState: domain.StateBrowserStopped,
		CreatedAt:       time.Now().UTC(),
	}
	if err := s.store.CreateSession(ctx, sess); err != nil {
		return nil, err
	}

	if autoLock && s.coord != nil {
		locked, errL := s.coord.AcquireLock(ctx, sess.ID, false, true)
		if errL != nil {
			return sess, errL
		}
		return locked, nil
	}

	s.publish(domain.EventSessionUpdated, sess)
	return sess, nil
}

func (s *SessionService) LockSession(ctx context.Context, sessionID string, forceSwitch bool, openTab bool) (*domain.Session, error) {
	if s.coord == nil {
		return nil, domain.NewAppError(domain.CodeInternalError, "session coordinator unavailable", "", http.StatusInternalServerError)
	}
	return s.coord.AcquireLock(ctx, sessionID, forceSwitch, openTab)
}

func (s *SessionService) UnlockSession(ctx context.Context, sessionID string) (*domain.Session, error) {
	if s.coord != nil {
		if err := s.coord.ReleaseSessionLock(ctx, sessionID); err != nil {
			return nil, err
		}
	}
	return s.store.GetSession(ctx, sessionID)
}

func (s *SessionService) OpenTab(ctx context.Context, sessionID string) (*domain.Session, error) {
	if s.coord == nil {
		return nil, domain.NewAppError(domain.CodeInternalError, "session coordinator unavailable", "", http.StatusInternalServerError)
	}
	return s.coord.OpenSessionTab(ctx, sessionID)
}

func (s *SessionService) RotateToken(ctx context.Context, sessionID string) (*domain.Session, error) {
	if _, err := s.store.RotateSessionToken(ctx, sessionID); err != nil {
		return nil, err
	}
	sess, err := s.store.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	s.publish(domain.EventSessionUpdated, sess)
	return sess, nil
}

func (s *SessionService) ObserveSession(ctx context.Context, sessionID string) (*domain.ObservationSnapshot, error) {
	if s.coord == nil {
		return nil, domain.NewAppError(domain.CodeInternalError, "session coordinator unavailable", "", http.StatusInternalServerError)
	}
	return s.coord.ObserveSession(ctx, sessionID)
}

func (s *SessionService) DeleteSession(ctx context.Context, sessionID string, closeTab bool) error {
	sess, err := s.store.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}
	if sess.IsLocked {
		if s.coord != nil {
			_ = s.coord.ReleaseSessionLock(ctx, sessionID)
		} else {
			_ = s.store.ReleaseProjectLock(ctx, sess.ProjectID)
		}
	}
	if s.coord != nil {
		_ = s.coord.DetachSession(ctx, sessionID, closeTab)
	}
	if err := s.store.DeleteSession(ctx, sessionID); err != nil {
		return err
	}
	if proj, errP := s.store.GetProject(ctx, sess.ProjectID); errP == nil && proj != nil {
		s.publish(domain.EventProjectUpdated, proj)
	}
	s.publish(domain.EventSessionDeleted, map[string]string{"id": sessionID, "project_id": sess.ProjectID})
	return nil
}
