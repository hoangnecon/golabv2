package telemetry_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hoangnecon/golabv2/internal/domain"
	"github.com/hoangnecon/golabv2/internal/service/telemetry"
)

type stubTelemDriver struct{}

func (stubTelemDriver) Close() error                                                 { return nil }
func (stubTelemDriver) ProbeTabState(ctx context.Context) (*domain.TabProbe, error) { return nil, nil }
func (stubTelemDriver) GetComputeUnits(ctx context.Context) (*domain.ComputeUnitsInfo, error) {
	return &domain.ComputeUnitsInfo{Balance: 15.25, FormattedBalance: "15.25", Tier: domain.TierPro}, nil
}
func (stubTelemDriver) ListAccountVMSessions(ctx context.Context) ([]domain.ColabVMSession, error) {
	return nil, nil
}
func (stubTelemDriver) FetchKernelXHR(ctx context.Context, path string, opts domain.FetchOptions) (*domain.FetchResponse, error) {
	return nil, nil
}
func (stubTelemDriver) CallColabMcpTool(ctx context.Context, toolName string, args map[string]any) (json.RawMessage, error) {
	return nil, nil
}
func (stubTelemDriver) ConnectRuntime(ctx context.Context) error                              { return nil }
func (stubTelemDriver) DisconnectRuntime(ctx context.Context) error                           { return nil }
func (stubTelemDriver) InterruptKernel(ctx context.Context) error                             { return nil }
func (stubTelemDriver) RestartKernel(ctx context.Context) error                               { return nil }
func (stubTelemDriver) GetAvailableAccelerators(ctx context.Context) (*domain.RuntimeConfigReport, error) {
	return nil, nil
}
func (stubTelemDriver) SetAccelerator(ctx context.Context, value string, highRAM bool) error { return nil }
func (stubTelemDriver) ExecPython(ctx context.Context, code string, preferSidecarWhenBusy bool) (*domain.PythonExecResult, error) {
	return nil, nil
}
func (stubTelemDriver) GetHardwareTelemetry(ctx context.Context) (*domain.HardwareTelemetry, error) {
	return &domain.HardwareTelemetry{RAM: domain.RAMTelemetry{TotalGB: 12.7}}, nil
}
func (stubTelemDriver) GetBuildInfo(ctx context.Context) (*domain.BuildInfo, error) {
	return &domain.BuildInfo{ReleaseTag: "2026-11"}, nil
}
func (stubTelemDriver) CheckDriveStatus(ctx context.Context, allowKernelVerify bool) (*domain.DriveStatus, error) {
	return &domain.DriveStatus{Mounted: true, IsFUSEVerified: allowKernelVerify}, nil
}
func (stubTelemDriver) MountDrive(ctx context.Context) (*domain.DriveMountResult, error) {
	return &domain.DriveMountResult{Mounted: true, Status: "mounted"}, nil
}
func (stubTelemDriver) StartBackgroundJob(ctx context.Context, command string, cwd string) (*domain.BackgroundJob, error) {
	return nil, nil
}
func (stubTelemDriver) PollBackgroundJob(ctx context.Context, jobID string, tailLines int) (*domain.BackgroundJob, error) {
	return nil, nil
}
func (stubTelemDriver) KillBackgroundJob(ctx context.Context, jobID string) (*domain.BackgroundJob, error) {
	return nil, nil
}
func (stubTelemDriver) GetRunningCells(ctx context.Context) (*domain.RunningCellsReport, error) {
	return nil, nil
}
func (stubTelemDriver) SaveNotebook(ctx context.Context) error { return nil }
func (stubTelemDriver) RunAll(ctx context.Context) error       { return nil }
func (stubTelemDriver) GetProxyURL(ctx context.Context, port int) (*domain.ProxyEndpoint, error) {
	return nil, nil
}

func TestTelemetryService(t *testing.T) {
	svc := telemetry.NewService(stubTelemDriver{})
	ctx := context.Background()

	ccu, err := svc.GetComputeUnits(ctx)
	if err != nil || ccu.FormattedBalance != "15.25" {
		t.Fatalf("GetComputeUnits: %+v, err=%v", ccu, err)
	}
	ds, err := svc.CheckDriveStatus(ctx, true)
	if err != nil || !ds.Mounted || !ds.IsFUSEVerified {
		t.Fatalf("CheckDriveStatus: %+v, err=%v", ds, err)
	}
}
