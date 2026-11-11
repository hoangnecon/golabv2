package proxy_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hoangnecon/golabv2/internal/domain"
	"github.com/hoangnecon/golabv2/internal/service/proxy"
)

type stubProxyDriver struct{}

func (stubProxyDriver) Close() error                                                 { return nil }
func (stubProxyDriver) ProbeTabState(ctx context.Context) (*domain.TabProbe, error) { return nil, nil }
func (stubProxyDriver) GetComputeUnits(ctx context.Context) (*domain.ComputeUnitsInfo, error) {
	return nil, nil
}
func (stubProxyDriver) ListAccountVMSessions(ctx context.Context) ([]domain.ColabVMSession, error) {
	return nil, nil
}
func (stubProxyDriver) FetchKernelXHR(ctx context.Context, path string, opts domain.FetchOptions) (*domain.FetchResponse, error) {
	return &domain.FetchResponse{Status: 200, OK: true, Body: json.RawMessage(`{"proxied":` + `"` + path + `"}`)}, nil
}
func (stubProxyDriver) CallColabMcpTool(ctx context.Context, toolName string, args map[string]any) (json.RawMessage, error) {
	return nil, nil
}
func (stubProxyDriver) ConnectRuntime(ctx context.Context) error                              { return nil }
func (stubProxyDriver) DisconnectRuntime(ctx context.Context) error                           { return nil }
func (stubProxyDriver) InterruptKernel(ctx context.Context) error                             { return nil }
func (stubProxyDriver) RestartKernel(ctx context.Context) error                               { return nil }
func (stubProxyDriver) GetAvailableAccelerators(ctx context.Context) (*domain.RuntimeConfigReport, error) {
	return nil, nil
}
func (stubProxyDriver) SetAccelerator(ctx context.Context, value string, highRAM bool) error { return nil }
func (stubProxyDriver) ExecPython(ctx context.Context, code string, preferSidecarWhenBusy bool) (*domain.PythonExecResult, error) {
	return nil, nil
}
func (stubProxyDriver) GetHardwareTelemetry(ctx context.Context) (*domain.HardwareTelemetry, error) {
	return nil, nil
}
func (stubProxyDriver) GetBuildInfo(ctx context.Context) (*domain.BuildInfo, error) { return nil, nil }
func (stubProxyDriver) CheckDriveStatus(ctx context.Context, allowKernelVerify bool) (*domain.DriveStatus, error) {
	return nil, nil
}
func (stubProxyDriver) MountDrive(ctx context.Context) (*domain.DriveMountResult, error) {
	return nil, nil
}
func (stubProxyDriver) StartBackgroundJob(ctx context.Context, command string, cwd string) (*domain.BackgroundJob, error) {
	return nil, nil
}
func (stubProxyDriver) PollBackgroundJob(ctx context.Context, jobID string, tailLines int) (*domain.BackgroundJob, error) {
	return nil, nil
}
func (stubProxyDriver) KillBackgroundJob(ctx context.Context, jobID string) (*domain.BackgroundJob, error) {
	return nil, nil
}
func (stubProxyDriver) GetRunningCells(ctx context.Context) (*domain.RunningCellsReport, error) {
	return nil, nil
}
func (stubProxyDriver) SaveNotebook(ctx context.Context) error { return nil }
func (stubProxyDriver) RunAll(ctx context.Context) error       { return nil }
func (stubProxyDriver) GetProxyURL(ctx context.Context, port int) (*domain.ProxyEndpoint, error) {
	return &domain.ProxyEndpoint{Port: port, URL: "https://6006-m-s.colab.dev", Token: "jwt"}, nil
}

func TestProxyService(t *testing.T) {
	svc := proxy.NewService(stubProxyDriver{})
	ctx := context.Background()

	resp, err := svc.ProxyForward(ctx, 6006, "/healthz", "GET", nil, "")
	if err != nil || !resp.OK {
		t.Fatalf("ProxyForward: %+v, err=%v", resp, err)
	}
	ep, err := svc.GetProxyURL(ctx, 6006)
	if err != nil || ep.Port != 6006 || ep.Token != "jwt" {
		t.Fatalf("GetProxyURL: %+v, err=%v", ep, err)
	}
}
