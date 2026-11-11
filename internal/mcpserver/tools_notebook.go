package mcpserver

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) colabGetNotebookOutline(ctx context.Context, req *mcp.CallToolRequest, input SessionOnlyInput) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	svcs, err := s.resolveServices(ctx, input.SessionID, false)
	if err != nil {
		return s.failTool(ctx, nil, err, time.Since(start).Milliseconds())
	}
	outline, err := svcs.Notebook.GetNotebookOutline(ctx)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return s.failTool(ctx, svcs.Target, err, latency)
	}
	return successResult(svcs.Target, outline, latency, "")
}

// GetCellsInput specifies cell index range.
type GetCellsInput struct {
	SessionID      string `json:"session_id,omitempty" jsonschema:"Optional locked session ID override."`
	StartIndex     int    `json:"start_index,omitempty" jsonschema:"Start cell index (0-based inclusive). Default 0."`
	EndIndex       int    `json:"end_index,omitempty" jsonschema:"End cell index (exclusive). Default 500."`
	IncludeOutputs bool   `json:"include_outputs,omitempty" jsonschema:"Include executed cell outputs. Default false."`
}

func (s *Server) colabGetCells(ctx context.Context, req *mcp.CallToolRequest, input GetCellsInput) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	svcs, err := s.resolveServices(ctx, input.SessionID, false)
	if err != nil {
		return s.failTool(ctx, nil, err, time.Since(start).Milliseconds())
	}
	cells, err := svcs.Notebook.GetCells(ctx, input.StartIndex, input.EndIndex, input.IncludeOutputs)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return s.failTool(ctx, svcs.Target, err, latency)
	}
	return successResult(svcs.Target, cells, latency, "")
}

// CellTargetInput targets a single cell by cell_id.
type CellTargetInput struct {
	SessionID string `json:"session_id,omitempty" jsonschema:"Optional locked session ID override."`
	CellID    string `json:"cell_id" jsonschema:"Notebook cell ID. Required."`
}

func (s *Server) colabGetCellWithLines(ctx context.Context, req *mcp.CallToolRequest, input CellTargetInput) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	svcs, err := s.resolveServices(ctx, input.SessionID, false)
	if err != nil {
		return s.failTool(ctx, nil, err, time.Since(start).Milliseconds())
	}
	res, err := svcs.Notebook.GetCellWithLines(ctx, input.CellID)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return s.failTool(ctx, svcs.Target, err, latency)
	}
	return successResult(svcs.Target, res, latency, "")
}

// SearchCellsInput searches across notebook cell sources.
type SearchCellsInput struct {
	SessionID string `json:"session_id,omitempty" jsonschema:"Optional locked session ID override."`
	Query     string `json:"query" jsonschema:"Case-insensitive substring to search across all notebook cells. Required."`
}

func (s *Server) colabSearchCells(ctx context.Context, req *mcp.CallToolRequest, input SearchCellsInput) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	svcs, err := s.resolveServices(ctx, input.SessionID, false)
	if err != nil {
		return s.failTool(ctx, nil, err, time.Since(start).Milliseconds())
	}
	matches, err := svcs.Notebook.SearchCells(ctx, input.Query)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return s.failTool(ctx, svcs.Target, err, latency)
	}
	return successResult(svcs.Target, map[string]any{
		"query":   input.Query,
		"matches": matches,
		"count":   len(matches),
	}, latency, "")
}

// AddCellInput inserts a new code or markdown cell.
type AddCellInput struct {
	SessionID string `json:"session_id,omitempty" jsonschema:"Optional locked session ID override."`
	Index     int    `json:"index" jsonschema:"0-based cell insertion index. Required."`
	CellType  string `json:"cell_type" jsonschema:"Cell type: 'code' or 'markdown'. Required."`
	Content   string `json:"content" jsonschema:"Initial cell source content. Required."`
}

func (s *Server) colabAddCell(ctx context.Context, req *mcp.CallToolRequest, input AddCellInput) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	svcs, err := s.resolveServices(ctx, input.SessionID, true)
	if err != nil {
		return s.failTool(ctx, nil, err, time.Since(start).Milliseconds())
	}
	cellID, err := svcs.Notebook.AddCell(ctx, input.CellType, input.Index, input.Content)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return s.failTool(ctx, svcs.Target, err, latency)
	}
	return successResult(svcs.Target, map[string]any{
		"cell_id": cellID,
		"index":   input.Index,
		"type":    input.CellType,
	}, latency, "")
}

// UpdateCellInput replaces the full source of a cell.
type UpdateCellInput struct {
	SessionID string `json:"session_id,omitempty" jsonschema:"Optional locked session ID override."`
	CellID    string `json:"cell_id" jsonschema:"Notebook cell ID. Required."`
	Content   string `json:"content" jsonschema:"Complete replacement cell content. Required."`
}

func (s *Server) colabUpdateCell(ctx context.Context, req *mcp.CallToolRequest, input UpdateCellInput) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	svcs, err := s.resolveServices(ctx, input.SessionID, true)
	if err != nil {
		return s.failTool(ctx, nil, err, time.Since(start).Milliseconds())
	}
	err = svcs.Notebook.UpdateCell(ctx, input.CellID, input.Content)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return s.failTool(ctx, svcs.Target, err, latency)
	}
	return successResult(svcs.Target, map[string]any{"updated": true, "cell_id": input.CellID}, latency, "")
}

// EditCellLinesInput replaces a 1-based inclusive line range inside a cell.
type EditCellLinesInput struct {
	SessionID  string `json:"session_id,omitempty" jsonschema:"Optional locked session ID override."`
	CellID     string `json:"cell_id" jsonschema:"Notebook cell ID. Required."`
	StartLine  int    `json:"start_line" jsonschema:"First line to replace (1-based inclusive). Required."`
	EndLine    int    `json:"end_line" jsonschema:"Last line to replace (1-based inclusive). Required."`
	NewContent string `json:"new_content" jsonschema:"Replacement source text for the specified line range. Required."`
}

func (s *Server) colabEditCellLines(ctx context.Context, req *mcp.CallToolRequest, input EditCellLinesInput) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	svcs, err := s.resolveServices(ctx, input.SessionID, true)
	if err != nil {
		return s.failTool(ctx, nil, err, time.Since(start).Milliseconds())
	}
	res, err := svcs.Notebook.EditCellLines(ctx, input.CellID, input.StartLine, input.EndLine, input.NewContent)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return s.failTool(ctx, svcs.Target, err, latency)
	}
	return successResult(svcs.Target, res, latency, "")
}

// MoveCellInput moves a cell to a new 0-based index.
type MoveCellInput struct {
	SessionID string `json:"session_id,omitempty" jsonschema:"Optional locked session ID override."`
	CellID    string `json:"cell_id" jsonschema:"Notebook cell ID. Required."`
	NewIndex  int    `json:"new_index" jsonschema:"Destination 0-based cell index. Required."`
}

func (s *Server) colabMoveCell(ctx context.Context, req *mcp.CallToolRequest, input MoveCellInput) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	svcs, err := s.resolveServices(ctx, input.SessionID, true)
	if err != nil {
		return s.failTool(ctx, nil, err, time.Since(start).Milliseconds())
	}
	err = svcs.Notebook.MoveCell(ctx, input.CellID, input.NewIndex)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return s.failTool(ctx, svcs.Target, err, latency)
	}
	return successResult(svcs.Target, map[string]any{"moved": true, "cell_id": input.CellID, "new_index": input.NewIndex}, latency, "")
}

func (s *Server) colabDeleteCell(ctx context.Context, req *mcp.CallToolRequest, input CellTargetInput) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	svcs, err := s.resolveServices(ctx, input.SessionID, true)
	if err != nil {
		return s.failTool(ctx, nil, err, time.Since(start).Milliseconds())
	}
	err = svcs.Notebook.DeleteCell(ctx, input.CellID)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return s.failTool(ctx, svcs.Target, err, latency)
	}
	return successResult(svcs.Target, map[string]any{"deleted": true, "cell_id": input.CellID}, latency, "")
}

func (s *Server) colabRunCodeCell(ctx context.Context, req *mcp.CallToolRequest, input CellTargetInput) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	svcs, err := s.resolveServices(ctx, input.SessionID, false)
	if err != nil {
		return s.failTool(ctx, nil, err, time.Since(start).Milliseconds())
	}
	rawOutput, err := svcs.Notebook.RunCodeCell(ctx, input.CellID)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return s.failTool(ctx, svcs.Target, err, latency)
	}
	return successResult(svcs.Target, rawOutput, latency, "")
}

func (s *Server) colabRunAllCells(ctx context.Context, req *mcp.CallToolRequest, input SessionOnlyInput) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	svcs, err := s.resolveServices(ctx, input.SessionID, false)
	if err != nil {
		return s.failTool(ctx, nil, err, time.Since(start).Milliseconds())
	}
	err = svcs.Notebook.RunAll(ctx)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return s.failTool(ctx, svcs.Target, err, latency)
	}
	return successResult(svcs.Target, map[string]any{
		"triggered": true,
		"message":   "All notebook cells scheduled for sequential execution.",
	}, latency, "busy")
}

// GetRunningCellsInput inspects live running cells.
type GetRunningCellsInput struct {
	SessionID  string `json:"session_id,omitempty" jsonschema:"Optional locked session ID override."`
	OnlyActive bool   `json:"only_active,omitempty" jsonschema:"When true, returns only currently executing cells."`
}

func (s *Server) colabGetRunningCells(ctx context.Context, req *mcp.CallToolRequest, input GetRunningCellsInput) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	svcs, err := s.resolveServices(ctx, input.SessionID, false)
	if err != nil {
		return s.failTool(ctx, nil, err, time.Since(start).Milliseconds())
	}
	report, err := svcs.Notebook.GetRunningCells(ctx, input.OnlyActive)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return s.failTool(ctx, svcs.Target, err, latency)
	}
	return successResult(svcs.Target, report, latency, "")
}

// GetCellOutputInput extracts structured outputs for a cell.
type GetCellOutputInput struct {
	SessionID     string `json:"session_id,omitempty" jsonschema:"Optional locked session ID override."`
	CellID        string `json:"cell_id" jsonschema:"Notebook cell ID. Required."`
	SaveImagesDir string `json:"save_images_dir,omitempty" jsonschema:"Optional local directory path to decode and save PNG plots produced by the cell."`
}

func (s *Server) colabGetCellOutput(ctx context.Context, req *mcp.CallToolRequest, input GetCellOutputInput) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	svcs, err := s.resolveServices(ctx, input.SessionID, false)
	if err != nil {
		return s.failTool(ctx, nil, err, time.Since(start).Milliseconds())
	}
	report, err := svcs.Notebook.GetCellOutput(ctx, input.CellID, input.SaveImagesDir)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return s.failTool(ctx, svcs.Target, err, latency)
	}
	return successResult(svcs.Target, report, latency, "")
}

func (s *Server) colabSaveNotebook(ctx context.Context, req *mcp.CallToolRequest, input SessionOnlyInput) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	svcs, err := s.resolveServices(ctx, input.SessionID, true)
	if err != nil {
		return s.failTool(ctx, nil, err, time.Since(start).Milliseconds())
	}
	err = svcs.Notebook.SaveNotebook(ctx)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return s.failTool(ctx, svcs.Target, err, latency)
	}
	return successResult(svcs.Target, map[string]any{
		"saved":   true,
		"message": "Notebook edits and outputs saved to Google Drive.",
	}, latency, "")
}
