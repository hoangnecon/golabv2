package jobs

import (
	"context"

	"github.com/hoangnecon/golabv2/internal/domain"
)

// Service manages out-of-band shell/Python background jobs via sidecar kernel (/tmp/.golab_jobs/).
type Service struct {
	drv domain.RuntimeDriver
}

func NewService(drv domain.RuntimeDriver) *Service {
	return &Service{drv: drv}
}

func (s *Service) StartJob(ctx context.Context, command string, cwd string) (*domain.BackgroundJob, error) {
	return s.drv.StartBackgroundJob(ctx, command, cwd)
}

func (s *Service) PollJob(ctx context.Context, jobID string, tailLines int) (*domain.BackgroundJob, error) {
	return s.drv.PollBackgroundJob(ctx, jobID, tailLines)
}

func (s *Service) KillJob(ctx context.Context, jobID string) (*domain.BackgroundJob, error) {
	return s.drv.KillBackgroundJob(ctx, jobID)
}
