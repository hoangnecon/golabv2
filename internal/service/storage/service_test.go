package storage_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/hoangnecon/golabv2/internal/domain"
	"github.com/hoangnecon/golabv2/internal/service/storage"
)

type stubDriver struct {
	driveStatus *domain.DriveStatus
	xhrHandler  func(path string, opts domain.FetchOptions) (*domain.FetchResponse, error)
}

func (d *stubDriver) Close() error                                                    { return nil }
func (d *stubDriver) ProbeTabState(ctx context.Context) (*domain.TabProbe, error)    { return nil, nil }
func (d *stubDriver) GetComputeUnits(ctx context.Context) (*domain.ComputeUnitsInfo, error) {
	return nil, nil
}
func (d *stubDriver) ListAccountVMSessions(ctx context.Context) ([]domain.ColabVMSession, error) {
	return nil, nil
}
func (d *stubDriver) FetchKernelXHR(ctx context.Context, path string, opts domain.FetchOptions) (*domain.FetchResponse, error) {
	if d.xhrHandler != nil {
		return d.xhrHandler(path, opts)
	}
	return &domain.FetchResponse{Status: 200, OK: true, Body: json.RawMessage(`{}`)}, nil
}
func (d *stubDriver) CallColabMcpTool(ctx context.Context, toolName string, args map[string]any) (json.RawMessage, error) {
	return nil, nil
}
func (d *stubDriver) ConnectRuntime(ctx context.Context) error                              { return nil }
func (d *stubDriver) DisconnectRuntime(ctx context.Context) error                           { return nil }
func (d *stubDriver) InterruptKernel(ctx context.Context) error                             { return nil }
func (d *stubDriver) RestartKernel(ctx context.Context) error                               { return nil }
func (d *stubDriver) GetAvailableAccelerators(ctx context.Context) (*domain.RuntimeConfigReport, error) {
	return nil, nil
}
func (d *stubDriver) SetAccelerator(ctx context.Context, value string, highRAM bool) error { return nil }
func (d *stubDriver) ExecPython(ctx context.Context, code string, preferSidecarWhenBusy bool) (*domain.PythonExecResult, error) {
	return nil, nil
}
func (d *stubDriver) GetHardwareTelemetry(ctx context.Context) (*domain.HardwareTelemetry, error) {
	return nil, nil
}
func (d *stubDriver) GetBuildInfo(ctx context.Context) (*domain.BuildInfo, error) { return nil, nil }
func (d *stubDriver) CheckDriveStatus(ctx context.Context, allowKernelVerify bool) (*domain.DriveStatus, error) {
	return d.driveStatus, nil
}
func (d *stubDriver) MountDrive(ctx context.Context) (*domain.DriveMountResult, error) {
	return nil, nil
}
func (d *stubDriver) StartBackgroundJob(ctx context.Context, command string, cwd string) (*domain.BackgroundJob, error) {
	return nil, nil
}
func (d *stubDriver) PollBackgroundJob(ctx context.Context, jobID string, tailLines int) (*domain.BackgroundJob, error) {
	return nil, nil
}
func (d *stubDriver) KillBackgroundJob(ctx context.Context, jobID string) (*domain.BackgroundJob, error) {
	return nil, nil
}
func (d *stubDriver) GetRunningCells(ctx context.Context) (*domain.RunningCellsReport, error) {
	return nil, nil
}
func (d *stubDriver) SaveNotebook(ctx context.Context) error { return nil }
func (d *stubDriver) RunAll(ctx context.Context) error       { return nil }
func (d *stubDriver) GetProxyURL(ctx context.Context, port int) (*domain.ProxyEndpoint, error) {
	return nil, nil
}

func TestStorageDriveGuardrailsAndStatPath(t *testing.T) {
	ctx := context.Background()

	// 1. Unmounted Google Drive blocks ReadFile on content/drive/... with GOLAB_DRIVE_UNMOUNTED
	drvUnmounted := &stubDriver{
		driveStatus: &domain.DriveStatus{Mounted: false, Status: "unmounted", Message: "not mounted"},
	}
	svc := storage.NewService(drvUnmounted)
	_, err := svc.ReadFile(ctx, "/content/drive/MyDrive/model.pt", "text")
	var appErr *domain.AppError
	if !errors.As(err, &appErr) || appErr.Code != domain.CodeDriveUnmounted {
		t.Fatalf("expected GOLAB_DRIVE_UNMOUNTED, got %v", err)
	}

	// 2. Local shadow directory (/content/drive created via os.makedirs) blocks WriteFile with GOLAB_DRIVE_LOCAL_SHADOW_DIR
	drvShadow := &stubDriver{
		driveStatus: &domain.DriveStatus{Mounted: false, Status: "corrupted_local_dir", Message: "shadow local folder"},
	}
	svcShadow := storage.NewService(drvShadow)
	_, err = svcShadow.WriteFile(ctx, "content/drive/MyDrive/ckpt.pt", "weights", "text")
	if !errors.As(err, &appErr) || appErr.Code != domain.CodeDriveLocalShadowDir {
		t.Fatalf("expected GOLAB_DRIVE_LOCAL_SHADOW_DIR, got %v", err)
	}

	// 3. StatPath on VM SSD works without touching Drive mount
	drvSSD := &stubDriver{
		xhrHandler: func(path string, opts domain.FetchOptions) (*domain.FetchResponse, error) {
			return &domain.FetchResponse{
				Status: 200,
				OK:     true,
				Body:   json.RawMessage(`{"name":"sample_data","path":"content/sample_data","type":"directory","writable":true}`),
			}, nil
		},
	}
	svcSSD := storage.NewService(drvSSD)
	st, err := svcSSD.StatPath(ctx, "/content/sample_data")
	if err != nil {
		t.Fatalf("StatPath: %v", err)
	}
	if !st.Exists || st.Type != "directory" || !st.Writable {
		t.Fatalf("unexpected StatPath: %+v", st)
	}
}
