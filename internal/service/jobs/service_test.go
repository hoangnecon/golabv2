package jobs_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hoangnecon/golabv2/internal/domain"
	"github.com/hoangnecon/golabv2/internal/service/jobs"
)

type stubJobsDriver struct{}

func (stubJobsDriver) Close() error                                                    { return nil }
func (stubJobsDriver) ProbeTabState(ctx context.Context) (*domain.TabProbe, error)    { return nil, nil }
func (stubJobsDriver) GetComputeUnits(ctx context.Context) (*domain.ComputeUnitsInfo, error) {
	return nil, nil
}
func (stubJobsDriver) ListAccountVMSessions(ctx context.Context) ([]domain.ColabVMSession, error) {
	return nil, nil
}
func (stubJobsDriver) FetchKernelXHR(ctx context.Context, path string, opts domain.FetchOptions) (*domain.FetchResponse, error) {
	return nil, nil
}
func (stubJobsDriver) CallColabMcpTool(ctx context.Context, toolName string, args map[string]any) (json.RawMessage, error) {
	return nil, nil
}
func (stubJobsDriver) ConnectRuntime(ctx context.Context) error                              { return nil }
func (stubJobsDriver) DisconnectRuntime(ctx context.Context) error                           { return nil }
func (stubJobsDriver) InterruptKernel(ctx context.Context) error                             { return nil }
func (stubJobsDriver) RestartKernel(ctx context.Context) error                               { return nil }
func (stubJobsDriver) GetAvailableAccelerators(ctx context.Context) (*domain.RuntimeConfigReport, error) {
	return nil, nil
}
func (stubJobsDriver) SetAccelerator(ctx context.Context, value string, highRAM bool) error { return nil }
func (stubJobsDriver) ExecPython(ctx context.Context, code string, preferSidecarWhenBusy bool) (*domain.PythonExecResult, error) {
	return nil, nil
}
func (stubJobsDriver) GetHardwareTelemetry(ctx context.Context) (*domain.HardwareTelemetry, error) {
	return nil, nil
}
func (stubJobsDriver) GetBuildInfo(ctx context.Context) (*domain.BuildInfo, error) { return nil, nil }
func (stubJobsDriver) CheckDriveStatus(ctx context.Context, allowKernelVerify bool) (*domain.DriveStatus, error) {
	return nil, nil
}
func (stubJobsDriver) MountDrive(ctx context.Context) (*domain.DriveMountResult, error) {
	return nil, nil
}
func (stubJobsDriver) StartBackgroundJob(ctx context.Context, command string, cwd string) (*domain.BackgroundJob, error) {
	return &domain.BackgroundJob{JobID: "job_42", Command: command, Running: true, UsedSidecar: true}, nil
}
func (stubJobsDriver) PollBackgroundJob(ctx context.Context, jobID string, tailLines int) (*domain.BackgroundJob, error) {
	code := 0
	return &domain.BackgroundJob{JobID: jobID, Running: false, ExitCode: &code, LogTail: "finished\n"}, nil
}
func (stubJobsDriver) KillBackgroundJob(ctx context.Context, jobID string) (*domain.BackgroundJob, error) {
	return &domain.BackgroundJob{JobID: jobID, Running: false}, nil
}
func (stubJobsDriver) GetRunningCells(ctx context.Context) (*domain.RunningCellsReport, error) {
	return nil, nil
}
func (stubJobsDriver) SaveNotebook(ctx context.Context) error { return nil }
func (stubJobsDriver) RunAll(ctx context.Context) error       { return nil }
func (stubJobsDriver) GetProxyURL(ctx context.Context, port int) (*domain.ProxyEndpoint, error) {
	return nil, nil
}

func TestJobsServiceStartPollKill(t *testing.T) {
	svc := jobs.NewService(stubJobsDriver{})
	ctx := context.Background()

	j1, err := svc.StartJob(ctx, "nvidia-smi", "/content")
	if err != nil || j1.JobID != "job_42" || !j1.Running {
		t.Fatalf("StartJob: %+v, err=%v", j1, err)
	}
	j2, err := svc.PollJob(ctx, "job_42", 20)
	if err != nil || j2.Running || j2.ExitCode == nil || *j2.ExitCode != 0 {
		t.Fatalf("PollJob: %+v, err=%v", j2, err)
	}
	j3, err := svc.KillJob(ctx, "job_42")
	if err != nil || j3.Running {
		t.Fatalf("KillJob: %+v, err=%v", j3, err)
	}
}
