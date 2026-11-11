package service

import (
	"context"
	"time"

	"github.com/hoangnecon/golabv2/internal/domain"
)

// ActivityService persists 100% of MCP tool calls (including pre-session/lock/permission errors) and broadcasts SSE events.
type ActivityService struct {
	store domain.Store
	bus   domain.EventBus
}

func NewActivityService(store domain.Store, bus domain.EventBus) *ActivityService {
	return &ActivityService{
		store: store,
		bus:   bus,
	}
}

func (s *ActivityService) Append(ctx context.Context, rec *domain.ActivityRecord) (*domain.ActivityLog, error) {
	entry, err := s.store.AppendActivity(ctx, rec)
	if err != nil {
		return nil, err
	}
	if s.bus != nil && entry != nil {
		s.bus.Publish(domain.WorkspaceEvent{
			Type:      domain.EventActivityAppended,
			Timestamp: time.Now().UTC(),
			Data:      entry,
		})
	}
	return entry, nil
}

func (s *ActivityService) List(ctx context.Context, filter domain.ActivityFilter) ([]*domain.ActivityLog, int64, error) {
	return s.store.ListActivity(ctx, filter)
}

func (s *ActivityService) Get(ctx context.Context, id int64) (*domain.ActivityDetail, error) {
	return s.store.GetActivity(ctx, id)
}
