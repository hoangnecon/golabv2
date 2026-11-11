package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hoangnecon/golabv2/internal/domain"
)

// Service controls Colab runtime & kernel lifecycle through domain.RuntimeDriver.
type Service struct {
	drv domain.RuntimeDriver
}

func NewService(drv domain.RuntimeDriver) *Service {
	return &Service{drv: drv}
}

func (s *Service) ListKernels(ctx context.Context) ([]KernelDetail, error) {
	resp, err := s.drv.FetchKernelXHR(ctx, "/api/kernels", domain.FetchOptions{Method: "GET"})
	if err != nil {
		return nil, fmt.Errorf("list kernels: %w", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("list kernels failed with status %d: %s", resp.Status, resp.RawText)
	}
	var kernels []KernelDetail
	if err := json.Unmarshal(resp.Body, &kernels); err != nil {
		return nil, fmt.Errorf("unmarshal kernels: %w", err)
	}
	return kernels, nil
}

func (s *Service) ListSessions(ctx context.Context) ([]SessionDetail, error) {
	resp, err := s.drv.FetchKernelXHR(ctx, "/api/sessions", domain.FetchOptions{Method: "GET"})
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("list sessions failed with status %d: %s", resp.Status, resp.RawText)
	}
	var sessions []SessionDetail
	if err := json.Unmarshal(resp.Body, &sessions); err != nil {
		return nil, fmt.Errorf("unmarshal sessions: %w", err)
	}
	return sessions, nil
}

func (s *Service) GetStatusReport(ctx context.Context) (*KernelStatusReport, error) {
	sessions, err := s.ListSessions(ctx)
	if err != nil {
		// If runtime is disconnected, return clean disconnected status report rather than opaque error
		var appErr *domain.AppError
		if errors.As(err, &appErr) && appErr.Code == domain.CodeKernelDisconnected {
			return &KernelStatusReport{
				IsConnected:  false,
				AllKernels:   []KernelDetail{},
				TotalKernels: 0,
			}, nil
		}
		return nil, err
	}
	kernels, err := s.ListKernels(ctx)
	if err != nil {
		return nil, err
	}
	report := &KernelStatusReport{
		IsConnected:  len(sessions) > 0 || len(kernels) > 0,
		AllKernels:   kernels,
		TotalKernels: len(kernels),
	}
	if len(sessions) > 0 {
		report.ActiveSession = &sessions[0]
		report.Kernel = sessions[0].Kernel
	} else if len(kernels) > 0 {
		report.Kernel = &kernels[0]
	}
	return report, nil
}

func (s *Service) InterruptKernel(ctx context.Context, kernelID string) error {
	if kernelID == "" {
		if err := s.drv.InterruptKernel(ctx); err == nil {
			return nil
		}
		rep, err := s.GetStatusReport(ctx)
		if err != nil {
			return err
		}
		if rep.Kernel == nil || rep.Kernel.ID == "" {
			return errors.New("no active kernel found to interrupt")
		}
		kernelID = rep.Kernel.ID
	}
	apiPath := fmt.Sprintf("/api/kernels/%s/interrupt", kernelID)
	resp, err := s.drv.FetchKernelXHR(ctx, apiPath, domain.FetchOptions{Method: "POST"})
	if err != nil {
		return fmt.Errorf("interrupt kernel %s: %w", kernelID, err)
	}
	if resp.Status != 204 && !resp.OK {
		return fmt.Errorf("interrupt failed with status %d: %s", resp.Status, resp.RawText)
	}
	return nil
}

func (s *Service) RestartKernel(ctx context.Context, kernelID string) (*KernelDetail, error) {
	if kernelID == "" {
		if err := s.drv.RestartKernel(ctx); err == nil {
			if rep, errRep := s.GetStatusReport(ctx); errRep == nil && rep.Kernel != nil {
				return rep.Kernel, nil
			}
			return &KernelDetail{Name: "python3", ExecutionState: "starting"}, nil
		}
		rep, err := s.GetStatusReport(ctx)
		if err != nil {
			return nil, err
		}
		if rep.Kernel == nil || rep.Kernel.ID == "" {
			return nil, errors.New("no active kernel found to restart")
		}
		kernelID = rep.Kernel.ID
	}
	apiPath := fmt.Sprintf("/api/kernels/%s/restart", kernelID)
	resp, err := s.drv.FetchKernelXHR(ctx, apiPath, domain.FetchOptions{Method: "POST"})
	if err != nil {
		return nil, fmt.Errorf("restart kernel %s: %w", kernelID, err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("restart failed with status %d: %s", resp.Status, resp.RawText)
	}
	var detail KernelDetail
	if err := json.Unmarshal(resp.Body, &detail); err != nil {
		return nil, fmt.Errorf("unmarshal restart response: %w", err)
	}
	return &detail, nil
}

func (s *Service) ConnectRuntime(ctx context.Context) error {
	return s.drv.ConnectRuntime(ctx)
}

func (s *Service) DisconnectRuntime(ctx context.Context) error {
	return s.drv.DisconnectRuntime(ctx)
}

func (s *Service) GetAvailableAccelerators(ctx context.Context) (*RuntimeConfigReport, error) {
	return s.drv.GetAvailableAccelerators(ctx)
}

func (s *Service) SetAccelerator(ctx context.Context, value string, highRAM bool) error {
	return s.drv.SetAccelerator(ctx, value, highRAM)
}

func (s *Service) ExecPython(ctx context.Context, code string, preferSidecarWhenBusy bool) (*PythonExecResult, error) {
	return s.drv.ExecPython(ctx, code, preferSidecarWhenBusy)
}
