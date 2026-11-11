package mcpserver

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) colabGetKernelStatus(ctx context.Context, req *mcp.CallToolRequest, input SessionOnlyInput) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	svcs, err := s.resolveServices(ctx, input.SessionID, false)
	if err != nil {
		return s.failTool(ctx, nil, err, time.Since(start).Milliseconds())
	}
	rep, err := svcs.Kernel.GetStatusReport(ctx)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return s.failTool(ctx, svcs.Target, err, latency)
	}
	state := string(svcs.Target.Session.ConnectionState)
	if rep.Kernel != nil && rep.Kernel.ExecutionState != "" {
		state = rep.Kernel.ExecutionState
	}
	return successResult(svcs.Target, rep, latency, state)
}

func (s *Server) colabConnectRuntime(ctx context.Context, req *mcp.CallToolRequest, input SessionOnlyInput) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	svcs, err := s.resolveServices(ctx, input.SessionID, false)
	if err != nil {
		return s.failTool(ctx, nil, err, time.Since(start).Milliseconds())
	}
	err = svcs.Kernel.ConnectRuntime(ctx)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return s.failTool(ctx, svcs.Target, err, latency)
	}
	return successResult(svcs.Target, map[string]any{
		"connected": true,
		"message":   "Colab hosted runtime connected and VM allocated.",
	}, latency, "idle")
}

func (s *Server) colabDisconnectRuntime(ctx context.Context, req *mcp.CallToolRequest, input SessionOnlyInput) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	svcs, err := s.resolveServices(ctx, input.SessionID, false)
	if err != nil {
		return s.failTool(ctx, nil, err, time.Since(start).Milliseconds())
	}
	err = svcs.Kernel.DisconnectRuntime(ctx)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return s.failTool(ctx, svcs.Target, err, latency)
	}
	return successResult(svcs.Target, map[string]any{
		"disconnected": true,
		"message":      "Colab VM runtime unassigned and released.",
	}, latency, "runtime_disconnected")
}

// KernelTargetInput specifies an optional target kernel ID.
type KernelTargetInput struct {
	SessionID string `json:"session_id,omitempty" jsonschema:"Optional locked session ID override."`
	KernelID  string `json:"kernel_id,omitempty" jsonschema:"Optional specific kernel ID. Leave empty to target the primary notebook kernel."`
}

func (s *Server) colabInterruptKernel(ctx context.Context, req *mcp.CallToolRequest, input KernelTargetInput) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	svcs, err := s.resolveServices(ctx, input.SessionID, false)
	if err != nil {
		return s.failTool(ctx, nil, err, time.Since(start).Milliseconds())
	}
	err = svcs.Kernel.InterruptKernel(ctx, input.KernelID)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return s.failTool(ctx, svcs.Target, err, latency)
	}
	return successResult(svcs.Target, map[string]any{
		"interrupted": true,
		"kernel_id":   input.KernelID,
		"message":     "SIGINT interrupt signal dispatched. Variables in Python RAM preserved.",
	}, latency, "idle")
}

func (s *Server) colabRestartKernel(ctx context.Context, req *mcp.CallToolRequest, input KernelTargetInput) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	svcs, err := s.resolveServices(ctx, input.SessionID, false)
	if err != nil {
		return s.failTool(ctx, nil, err, time.Since(start).Milliseconds())
	}
	detail, err := svcs.Kernel.RestartKernel(ctx, input.KernelID)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return s.failTool(ctx, svcs.Target, err, latency)
	}
	return successResult(svcs.Target, map[string]any{
		"restarted": true,
		"kernel":    detail,
		"message":   "Kernel restarted and GPU VRAM cleared.",
	}, latency, detail.ExecutionState)
}

func (s *Server) colabGetAvailableAccelerators(ctx context.Context, req *mcp.CallToolRequest, input SessionOnlyInput) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	svcs, err := s.resolveServices(ctx, input.SessionID, false)
	if err != nil {
		return s.failTool(ctx, nil, err, time.Since(start).Milliseconds())
	}
	report, err := svcs.Kernel.GetAvailableAccelerators(ctx)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return s.failTool(ctx, svcs.Target, err, latency)
	}
	return successResult(svcs.Target, report, latency, "")
}

// SetAcceleratorInput configures the runtime hardware accelerator.
type SetAcceleratorInput struct {
	SessionID   string `json:"session_id,omitempty" jsonschema:"Optional locked session ID override."`
	Accelerator string `json:"accelerator" jsonschema:"Target accelerator (e.g. 'T4', 'L4', 'A100', 'NONE'). Required."`
	HighRAM     bool   `json:"high_ram,omitempty" jsonschema:"Enable High-RAM runtime shape when supported."`
}

func (s *Server) colabSetAccelerator(ctx context.Context, req *mcp.CallToolRequest, input SetAcceleratorInput) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	svcs, err := s.resolveServices(ctx, input.SessionID, true)
	if err != nil {
		return s.failTool(ctx, nil, err, time.Since(start).Milliseconds())
	}
	err = svcs.Kernel.SetAccelerator(ctx, input.Accelerator, input.HighRAM)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return s.failTool(ctx, svcs.Target, err, latency)
	}
	return successResult(svcs.Target, map[string]any{
		"success":     true,
		"accelerator": input.Accelerator,
		"high_ram":    input.HighRAM,
		"message":     "Hardware accelerator configuration applied.",
	}, latency, "")
}

// ExecPythonInput runs out-of-band Python without adding notebook cells.
type ExecPythonInput struct {
	SessionID             string `json:"session_id,omitempty" jsonschema:"Optional locked session ID override."`
	Code                  string `json:"code" jsonschema:"Python source code to execute silently on the Colab VM. Required."`
	PreferSidecarWhenBusy *bool  `json:"prefer_sidecar_when_busy,omitempty" jsonschema:"When true (default), spawns/reuses a sidecar Python kernel via POST /api/kernels if the primary notebook kernel is busy."`
}

func (s *Server) colabExecPython(ctx context.Context, req *mcp.CallToolRequest, input ExecPythonInput) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	svcs, err := s.resolveServices(ctx, input.SessionID, false)
	if err != nil {
		return s.failTool(ctx, nil, err, time.Since(start).Milliseconds())
	}
	preferSidecar := true
	if input.PreferSidecarWhenBusy != nil {
		preferSidecar = *input.PreferSidecarWhenBusy
	}
	res, err := svcs.Kernel.ExecPython(ctx, input.Code, preferSidecar)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return s.failTool(ctx, svcs.Target, err, latency)
	}
	return successResult(svcs.Target, res, latency, "")
}
