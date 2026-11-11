package mcpserver

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ListDirectoryInput specifies arguments for directory inspection.
type ListDirectoryInput struct {
	SessionID string `json:"session_id,omitempty" jsonschema:"Optional locked session ID override."`
	Path      string `json:"path,omitempty" jsonschema:"Directory path to list (e.g. 'content', 'content/drive/MyDrive', or empty for root)."`
}

func (s *Server) colabListDirectory(ctx context.Context, req *mcp.CallToolRequest, input ListDirectoryInput) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	svcs, err := s.resolveServices(ctx, input.SessionID, false)
	if err != nil {
		return s.failTool(ctx, nil, err, time.Since(start).Milliseconds())
	}
	res, err := svcs.Storage.ListDirectory(ctx, input.Path)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return s.failTool(ctx, svcs.Target, err, latency)
	}
	return successResult(svcs.Target, res, latency, "")
}

// StatPathInput inspects path metadata without downloading file contents.
type StatPathInput struct {
	SessionID string `json:"session_id,omitempty" jsonschema:"Optional locked session ID override."`
	Path      string `json:"path" jsonschema:"Path to inspect on Colab VM SSD or mounted Google Drive (e.g. 'content/drive/MyDrive/ckpt.pt'). Required."`
}

func (s *Server) colabStatPath(ctx context.Context, req *mcp.CallToolRequest, input StatPathInput) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	svcs, err := s.resolveServices(ctx, input.SessionID, false)
	if err != nil {
		return s.failTool(ctx, nil, err, time.Since(start).Milliseconds())
	}
	res, err := svcs.Storage.StatPath(ctx, input.Path)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return s.failTool(ctx, svcs.Target, err, latency)
	}
	return successResult(svcs.Target, res, latency, "")
}

// ReadFileInput specifies arguments for reading a file.
type ReadFileInput struct {
	SessionID string `json:"session_id,omitempty" jsonschema:"Optional locked session ID override."`
	Path      string `json:"path" jsonschema:"Path to file on Colab (e.g. 'content/dataset.csv' or 'content/drive/MyDrive/model.py'). Required."`
	Format    string `json:"format,omitempty" jsonschema:"Encoding format: 'text' (default) or 'base64' for binary files."`
}

func (s *Server) colabReadFile(ctx context.Context, req *mcp.CallToolRequest, input ReadFileInput) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	svcs, err := s.resolveServices(ctx, input.SessionID, false)
	if err != nil {
		return s.failTool(ctx, nil, err, time.Since(start).Milliseconds())
	}
	res, err := svcs.Storage.ReadFile(ctx, input.Path, input.Format)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return s.failTool(ctx, svcs.Target, err, latency)
	}
	return successResult(svcs.Target, res, latency, "")
}

// WriteFileInput specifies arguments for writing/creating a file.
type WriteFileInput struct {
	SessionID string `json:"session_id,omitempty" jsonschema:"Optional locked session ID override."`
	Path      string `json:"path" jsonschema:"Destination file path on Colab VM SSD or mounted Google Drive. Required."`
	Content   string `json:"content" jsonschema:"File content ('text' or 'base64' encoded). Required."`
	Format    string `json:"format,omitempty" jsonschema:"Content encoding: 'text' (default) or 'base64'."`
}

func (s *Server) colabWriteFile(ctx context.Context, req *mcp.CallToolRequest, input WriteFileInput) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	svcs, err := s.resolveServices(ctx, input.SessionID, false)
	if err != nil {
		return s.failTool(ctx, nil, err, time.Since(start).Milliseconds())
	}
	res, err := svcs.Storage.WriteFile(ctx, input.Path, input.Content, input.Format)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return s.failTool(ctx, svcs.Target, err, latency)
	}
	return successResult(svcs.Target, res, latency, "")
}

// CreateDirectoryInput specifies a directory to create.
type CreateDirectoryInput struct {
	SessionID string `json:"session_id,omitempty" jsonschema:"Optional locked session ID override."`
	Path      string `json:"path" jsonschema:"Directory path to create (e.g. 'content/checkpoints'). Required."`
}

func (s *Server) colabCreateDirectory(ctx context.Context, req *mcp.CallToolRequest, input CreateDirectoryInput) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	svcs, err := s.resolveServices(ctx, input.SessionID, false)
	if err != nil {
		return s.failTool(ctx, nil, err, time.Since(start).Milliseconds())
	}
	err = svcs.Storage.CreateDirectory(ctx, input.Path)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return s.failTool(ctx, svcs.Target, err, latency)
	}
	return successResult(svcs.Target, map[string]any{"created": true, "path": input.Path}, latency, "")
}

// MoveFileInput specifies paths for renaming or moving files.
type MoveFileInput struct {
	SessionID string `json:"session_id,omitempty" jsonschema:"Optional locked session ID override."`
	OldPath   string `json:"old_path" jsonschema:"Existing file or directory path. Required."`
	NewPath   string `json:"new_path" jsonschema:"Destination file or directory path. Required."`
}

func (s *Server) colabMoveFile(ctx context.Context, req *mcp.CallToolRequest, input MoveFileInput) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	svcs, err := s.resolveServices(ctx, input.SessionID, false)
	if err != nil {
		return s.failTool(ctx, nil, err, time.Since(start).Milliseconds())
	}
	err = svcs.Storage.MoveFile(ctx, input.OldPath, input.NewPath)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return s.failTool(ctx, svcs.Target, err, latency)
	}
	return successResult(svcs.Target, map[string]any{"moved": true, "old_path": input.OldPath, "new_path": input.NewPath}, latency, "")
}

// DeleteFileInput specifies file/directory to delete.
type DeleteFileInput struct {
	SessionID string `json:"session_id,omitempty" jsonschema:"Optional locked session ID override."`
	Path      string `json:"path" jsonschema:"Path to file or directory to delete. Required."`
}

func (s *Server) colabDeleteFile(ctx context.Context, req *mcp.CallToolRequest, input DeleteFileInput) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	svcs, err := s.resolveServices(ctx, input.SessionID, false)
	if err != nil {
		return s.failTool(ctx, nil, err, time.Since(start).Milliseconds())
	}
	err = svcs.Storage.DeleteFile(ctx, input.Path)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return s.failTool(ctx, svcs.Target, err, latency)
	}
	return successResult(svcs.Target, map[string]any{"deleted": true, "path": input.Path}, latency, "")
}
