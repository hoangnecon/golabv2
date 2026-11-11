package telemetry

import (
	"context"

	"github.com/hoangnecon/golabv2/internal/domain"
)

// Service provides hardware telemetry, Compute Units, Build Info, and two-path Google Drive FUSE mount.
type Service struct {
	drv domain.RuntimeDriver
}

func NewService(drv domain.RuntimeDriver) *Service {
	return &Service{drv: drv}
}

func (s *Service) GetHardwareTelemetry(ctx context.Context) (*HardwareTelemetry, error) {
	return s.drv.GetHardwareTelemetry(ctx)
}

func (s *Service) GetComputeUnits(ctx context.Context) (*ComputeUnitsInfo, error) {
	return s.drv.GetComputeUnits(ctx)
}

func (s *Service) GetBuildInfo(ctx context.Context) (*BuildInfo, error) {
	return s.drv.GetBuildInfo(ctx)
}

func (s *Service) CheckDriveStatus(ctx context.Context, verifyFUSE bool) (*DriveStatus, error) {
	return s.drv.CheckDriveStatus(ctx, verifyFUSE)
}

func (s *Service) MountDrive(ctx context.Context) (*DriveMountResult, error) {
	return s.drv.MountDrive(ctx)
}
