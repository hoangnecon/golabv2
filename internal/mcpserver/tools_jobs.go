package mcpserver

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// StartBackgroundJobInput launches an out-of-band detached shell job via sidecar kernel.
type StartBackgroundJobInput struct {
	SessionID string `json:"session_id,omitempty" jsonschema:"Optional locked session ID override."`
	Command   string `json:"command" jsonschema:"Bash command line to run in the background on the Colab VM (prints to /tmp/.golab_jobs/<id>.log). Required."`
	Cwd       string `json:"cwd,omitempty" jsonschema:"Working directory on the Colab VM (default '/content')."`
}

func (s *Server) colabStartBackgroundJob(ctx context.Context, req *mcp.CallToolRequest, input StartBackgroundJobInput) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	svcs, err := s.resolveServices(ctx, input.SessionID, false)
	if err != nil {
		return s.failTool(ctx, nil, err, time.Since(start).Milliseconds())
	}
	job, err := svcs.Jobs.StartJob(ctx, input.Command, input.Cwd)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return s.failTool(ctx, svcs.Target, err, latency)
	}
	return successResult(svcs.Target, job, latency, "")
}

// PollBackgroundJobInput polls a running or finished background job.
type PollBackgroundJobInput struct {
	SessionID string `json:"session_id,omitempty" jsonschema:"Optional locked session ID override."`
	JobID     string `json:"job_id" jsonschema:"Background job ID returned by colab_start_background_job. Required."`
	TailLines int    `json:"tail_lines,omitempty" jsonschema:"Number of trailing log lines to return (default 80)."`
}

func (s *Server) colabPollBackgroundJob(ctx context.Context, req *mcp.CallToolRequest, input PollBackgroundJobInput) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	svcs, err := s.resolveServices(ctx, input.SessionID, false)
	if err != nil {
		return s.failTool(ctx, nil, err, time.Since(start).Milliseconds())
	}
	job, err := svcs.Jobs.PollJob(ctx, input.JobID, input.TailLines)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return s.failTool(ctx, svcs.Target, err, latency)
	}
	return successResult(svcs.Target, job, latency, "")
}

// KillBackgroundJobInput terminates a background job process group.
type KillBackgroundJobInput struct {
	SessionID string `json:"session_id,omitempty" jsonschema:"Optional locked session ID override."`
	JobID     string `json:"job_id" jsonschema:"Background job ID to terminate. Required."`
}

func (s *Server) colabKillBackgroundJob(ctx context.Context, req *mcp.CallToolRequest, input KillBackgroundJobInput) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	svcs, err := s.resolveServices(ctx, input.SessionID, false)
	if err != nil {
		return s.failTool(ctx, nil, err, time.Since(start).Milliseconds())
	}
	job, err := svcs.Jobs.KillJob(ctx, input.JobID)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return s.failTool(ctx, svcs.Target, err, latency)
	}
	return successResult(svcs.Target, job, latency, "")
}
