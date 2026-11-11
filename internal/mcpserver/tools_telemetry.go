package mcpserver

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) colabGetHardwareTelemetry(ctx context.Context, req *mcp.CallToolRequest, input SessionOnlyInput) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	svcs, err := s.resolveServices(ctx, input.SessionID, false)
	if err != nil {
		return s.failTool(ctx, nil, err, time.Since(start).Milliseconds())
	}
	telem, err := svcs.Telemetry.GetHardwareTelemetry(ctx)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return s.failTool(ctx, svcs.Target, err, latency)
	}
	return successResult(svcs.Target, telem, latency, "")
}

func (s *Server) colabGetComputeUnits(ctx context.Context, req *mcp.CallToolRequest, input SessionOnlyInput) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	svcs, err := s.resolveServices(ctx, input.SessionID, false)
	if err != nil {
		return s.failTool(ctx, nil, err, time.Since(start).Milliseconds())
	}
	ccu, err := svcs.Telemetry.GetComputeUnits(ctx)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return s.failTool(ctx, svcs.Target, err, latency)
	}
	return successResult(svcs.Target, ccu, latency, "")
}

// CheckDriveInput configures ground-truth FUSE Drive verification.
type CheckDriveInput struct {
	SessionID  string `json:"session_id,omitempty" jsonschema:"Optional locked session ID override."`
	VerifyFUSE *bool  `json:"verify_fuse,omitempty" jsonschema:"When true (default), verifies /proc/mounts fuse.drive and MyDrive disk quota via sidecar-safe probe."`
}

func (s *Server) colabCheckDriveStatus(ctx context.Context, req *mcp.CallToolRequest, input CheckDriveInput) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	svcs, err := s.resolveServices(ctx, input.SessionID, false)
	if err != nil {
		return s.failTool(ctx, nil, err, time.Since(start).Milliseconds())
	}
	verify := true
	if input.VerifyFUSE != nil {
		verify = *input.VerifyFUSE
	}
	status, err := svcs.Telemetry.CheckDriveStatus(ctx, verify)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return s.failTool(ctx, svcs.Target, err, latency)
	}
	return successResult(svcs.Target, status, latency, "")
}

func (s *Server) colabMountDrive(ctx context.Context, req *mcp.CallToolRequest, input SessionOnlyInput) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	svcs, err := s.resolveServices(ctx, input.SessionID, true)
	if err != nil {
		return s.failTool(ctx, nil, err, time.Since(start).Milliseconds())
	}
	res, err := svcs.Telemetry.MountDrive(ctx)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		if res != nil {
			return s.failToolWithData(ctx, svcs.Target, res, err, latency)
		}
		return s.failTool(ctx, svcs.Target, err, latency)
	}
	return successResult(svcs.Target, res, latency, "")
}

func (s *Server) colabGetBuildInfo(ctx context.Context, req *mcp.CallToolRequest, input SessionOnlyInput) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	svcs, err := s.resolveServices(ctx, input.SessionID, false)
	if err != nil {
		return s.failTool(ctx, nil, err, time.Since(start).Milliseconds())
	}
	info, err := svcs.Telemetry.GetBuildInfo(ctx)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return s.failTool(ctx, svcs.Target, err, latency)
	}
	return successResult(svcs.Target, info, latency, "")
}
