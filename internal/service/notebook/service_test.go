package notebook_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/hoangnecon/golabv2/internal/domain"
	"github.com/hoangnecon/golabv2/internal/service/notebook"
)

type stubNotebookDriver struct {
	cells []domain.Cell
}

func (d *stubNotebookDriver) Close() error                                                    { return nil }
func (d *stubNotebookDriver) ProbeTabState(ctx context.Context) (*domain.TabProbe, error)    { return nil, nil }
func (d *stubNotebookDriver) GetComputeUnits(ctx context.Context) (*domain.ComputeUnitsInfo, error) {
	return nil, nil
}
func (d *stubNotebookDriver) ListAccountVMSessions(ctx context.Context) ([]domain.ColabVMSession, error) {
	return nil, nil
}
func (d *stubNotebookDriver) FetchKernelXHR(ctx context.Context, path string, opts domain.FetchOptions) (*domain.FetchResponse, error) {
	return nil, nil
}
func (d *stubNotebookDriver) CallColabMcpTool(ctx context.Context, toolName string, args map[string]any) (json.RawMessage, error) {
	if toolName == "get_cells" {
		b, _ := json.Marshal(map[string]any{"cells": d.cells})
		return b, nil
	}
	return json.RawMessage(`{"newCellId":"c_new"}`), nil
}
func (d *stubNotebookDriver) ConnectRuntime(ctx context.Context) error                              { return nil }
func (d *stubNotebookDriver) DisconnectRuntime(ctx context.Context) error                           { return nil }
func (d *stubNotebookDriver) InterruptKernel(ctx context.Context) error                             { return nil }
func (d *stubNotebookDriver) RestartKernel(ctx context.Context) error                               { return nil }
func (d *stubNotebookDriver) GetAvailableAccelerators(ctx context.Context) (*domain.RuntimeConfigReport, error) {
	return nil, nil
}
func (d *stubNotebookDriver) SetAccelerator(ctx context.Context, value string, highRAM bool) error { return nil }
func (d *stubNotebookDriver) ExecPython(ctx context.Context, code string, preferSidecarWhenBusy bool) (*domain.PythonExecResult, error) {
	return nil, nil
}
func (d *stubNotebookDriver) GetHardwareTelemetry(ctx context.Context) (*domain.HardwareTelemetry, error) {
	return nil, nil
}
func (d *stubNotebookDriver) GetBuildInfo(ctx context.Context) (*domain.BuildInfo, error) { return nil, nil }
func (d *stubNotebookDriver) CheckDriveStatus(ctx context.Context, allowKernelVerify bool) (*domain.DriveStatus, error) {
	return nil, nil
}
func (d *stubNotebookDriver) MountDrive(ctx context.Context) (*domain.DriveMountResult, error) {
	return nil, nil
}
func (d *stubNotebookDriver) StartBackgroundJob(ctx context.Context, command string, cwd string) (*domain.BackgroundJob, error) {
	return nil, nil
}
func (d *stubNotebookDriver) PollBackgroundJob(ctx context.Context, jobID string, tailLines int) (*domain.BackgroundJob, error) {
	return nil, nil
}
func (d *stubNotebookDriver) KillBackgroundJob(ctx context.Context, jobID string) (*domain.BackgroundJob, error) {
	return nil, nil
}
func (d *stubNotebookDriver) GetRunningCells(ctx context.Context) (*domain.RunningCellsReport, error) {
	return &domain.RunningCellsReport{}, nil
}
func (d *stubNotebookDriver) SaveNotebook(ctx context.Context) error { return nil }
func (d *stubNotebookDriver) RunAll(ctx context.Context) error       { return nil }
func (d *stubNotebookDriver) GetProxyURL(ctx context.Context, port int) (*domain.ProxyEndpoint, error) {
	return nil, nil
}

func TestSearchCellsAndGetCellOutputExportImages(t *testing.T) {
	pngBytes := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	pngB64 := base64.StdEncoding.EncodeToString(pngBytes)

	drv := &stubNotebookDriver{
		cells: []domain.Cell{
			{
				ID:       "cell_train",
				CellType: "code",
				Source:   []string{"import torch\n", "lr = 3e-4\n", "print('epoch 1 loss=0.42')\n"},
				Outputs: []map[string]any{
					{
						"output_type": "stream",
						"name":        "stdout",
						"text":        "epoch 1 loss=0.42\n",
					},
					{
						"output_type": "display_data",
						"data": map[string]any{
							"image/png": pngB64,
						},
					},
				},
			},
		},
	}

	svc := notebook.NewService(drv)
	ctx := context.Background()

	matches, err := svc.SearchCells(ctx, "3e-4")
	if err != nil {
		t.Fatalf("SearchCells: %v", err)
	}
	if len(matches) != 1 || matches[0].CellID != "cell_train" || matches[0].LineNumber != 2 {
		t.Fatalf("unexpected SearchCells matches: %+v", matches)
	}

	outDir := filepath.Join(t.TempDir(), "exported_imgs")
	out, err := svc.GetCellOutput(ctx, "cell_train", outDir)
	if err != nil {
		t.Fatalf("GetCellOutput: %v", err)
	}
	if out.Stdout != "epoch 1 loss=0.42\n" || len(out.SavedImages) != 1 {
		t.Fatalf("unexpected GetCellOutput: %+v", out)
	}
	if _, err := os.Stat(out.SavedImages[0]); err != nil {
		t.Fatalf("expected exported PNG on disk at %s: %v", out.SavedImages[0], err)
	}
}
