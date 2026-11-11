package notebook

import "github.com/hoangnecon/golabv2/internal/domain"

type Cell = domain.Cell
type OutlineItem = domain.OutlineItem
type NotebookOutline = domain.NotebookOutline
type RunningCellInfo = domain.RunningCellInfo
type RunningCellsReport = domain.RunningCellsReport

// EditLinesResult reports the outcome of a line-level edit.
type EditLinesResult struct {
	CellID       string `json:"cell_id"`
	Modified     bool   `json:"modified"`
	LinesChanged int    `json:"lines_changed"`
	TotalLines   int    `json:"total_lines"`
}

// CellSearchMatch represents a line match inside a notebook cell.
type CellSearchMatch struct {
	CellIndex  int    `json:"cell_index"`
	CellID     string `json:"cell_id"`
	CellType   string `json:"cell_type"`
	LineNumber int    `json:"line_number"`
	LineText   string `json:"line_text"`
}

// CellOutputReport contains structured stdout, stderr, rich outputs, errors, and exported image paths.
type CellOutputReport struct {
	CellID      string           `json:"cell_id"`
	CellType    string           `json:"cell_type"`
	Stdout      string           `json:"stdout"`
	Stderr      string           `json:"stderr,omitempty"`
	TextResult  string           `json:"text_result,omitempty"`
	ErrorText   string           `json:"error_text,omitempty"`
	ImageCount  int              `json:"image_count"`
	SavedImages []string         `json:"saved_images,omitempty"`
	RawOutputs  []map[string]any `json:"raw_outputs"`
}
