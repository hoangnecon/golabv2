package kernel_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hoangnecon/golabv2/internal/domain"
	"github.com/hoangnecon/golabv2/internal/service/kernel"
)

type stubKernelDriver struct{}

func (stubKernelDriver) Close() error                                                    { return nil }
func (stubKernelDriver) ProbeTabState(ctx context.Context) (*domain.TabProbe, error)    { return nil, nil }
func (stubKernelDriver) GetComputeUnits(ctx context.Context) (*domain.ComputeUnitsInfo, error) {
	return nil, nil
}
func (stubKernelDriver) ListAccountVMSessions(ctx context.Context) ([]domain.ColabVMSession, error) {
	return nil, nil
}
func (stubKernelDriver) FetchKernelXHR(ctx context.Context, path string, opts domain.FetchOptions) (*domain.FetchResponse, error) {
	switch path {
	case "/api/sessions":
		return &domain.FetchResponse{Status: 200, OK: true, Body: json.RawMessage(`[{"id":"s1","name":"Exp.ipynb","kernel":{"id":"k1","name":"python3","execution_state":"idle"}}]`)}, nil
	case "/api/kernels":
		return &domain.FetchResponse{Status: 200, OK: true, Body: json.RawMessage(`[{"id":"k1","name":"python3","execution_state":"idle"}]`)}, nil
	case "/api/kernels/k1/restart":
		return &domain.FetchResponse{Status: 200, OK: true, Body: json.RawMessage(`{"id":"k1","name":"python3","execution_state":"starting"}`)}, nil
	default:
		return &domain.FetchResponse{Status: 204, OK: true}, nil
	}
}
func (stubKernelDriver) CallColabMcpTool(ctx context.Context, toolName string, args map[string]any) (json.RawMessage, error) {
	return nil, nil
}
func (stubKernelDriver) ConnectRuntime(ctx context.Context) error                              { return nil }
func (stubKernelDriver) DisconnectRuntime(ctx context.Context) error                           { return nil }
func (stubKernelDriver) InterruptKernel(ctx context.Context) error                             { return nil }
func (stubKernelDriver) RestartKernel(ctx context.Context) error                               { return nil }
func (stubKernelDriver) GetAvailableAccelerators(ctx context.Context) (*domain.RuntimeConfigReport, error) {
	return &domain.RuntimeConfigReport{HighRAM: true}, nil
}
func (stubKernelDriver) SetAccelerator(ctx context.Context, value string, highRAM bool) error { return nil }
func (stubKernelDriver) ExecPython(ctx context.Context, code string, preferSidecarWhenBusy bool) (*domain.PythonExecResult, error) {
	return &domain.PythonExecResult{Stdout: "2\n", UsedSidecar: preferSidecarWhenBusy}, nil
}
func (stubKernelDriver) GetHardwareTelemetry(ctx context.Context) (*domain.HardwareTelemetry, error) {
	return nil, nil
}
func (stubKernelDriver) GetBuildInfo(ctx context.Context) (*domain.BuildInfo, error) { return nil, nil }
func (stubKernelDriver) CheckDriveStatus(ctx context.Context, allowKernelVerify bool) (*domain.DriveStatus, error) {
	return nil, nil
}
func (stubKernelDriver) MountDrive(ctx context.Context) (*domain.DriveMountResult, error) {
	return nil, nil
}
func (stubKernelDriver) StartBackgroundJob(ctx context.Context, command string, cwd string) (*domain.BackgroundJob, error) {
	return nil, nil
}
func (stubKernelDriver) PollBackgroundJob(ctx context.Context, jobID string, tailLines int) (*domain.BackgroundJob, error) {
	return nil, nil
}
func (stubKernelDriver) KillBackgroundJob(ctx context.Context, jobID string) (*domain.BackgroundJob, error) {
	return nil, nil
}
func (stubKernelDriver) GetRunningCells(ctx context.Context) (*domain.RunningCellsReport, error) {
	return nil, nil
}
func (stubKernelDriver) SaveNotebook(ctx context.Context) error { return nil }
func (stubKernelDriver) RunAll(ctx context.Context) error       { return nil }
func (stubKernelDriver) GetProxyURL(ctx context.Context, port int) (*domain.ProxyEndpoint, error) {
	return nil, nil
}

func TestKernelServiceLifecycle(t *testing.T) {
	svc := kernel.NewService(stubKernelDriver{})
	ctx := context.Background()

	rep, err := svc.GetStatusReport(ctx)
	if err != nil || !rep.IsConnected || rep.Kernel == nil || rep.Kernel.ID != "k1" {
		t.Fatalf("GetStatusReport: %+v, err=%v", rep, err)
	}
	if _, err := svc.RestartKernel(ctx, ""); err != nil {
		t.Fatalf("RestartKernel: %v", err)
	}
	py, err := svc.ExecPython(ctx, "print(1+1)", true)
	if err != nil || !py.UsedSidecar || py.Stdout != "2\n" {
		t.Fatalf("ExecPython: %+v, err=%v", py, err)
	}
}
