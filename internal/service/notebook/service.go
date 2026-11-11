package notebook

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/hoangnecon/golabv2/internal/domain"
)

// Service provides surgical control over notebook cells, search, execution, and output extraction.
type Service struct {
	drv domain.RuntimeDriver
}

func NewService(drv domain.RuntimeDriver) *Service {
	return &Service{drv: drv}
}

func (s *Service) GetCells(ctx context.Context, start, end int, includeOutputs bool) ([]Cell, error) {
	if end <= 0 {
		end = 500
	}
	raw, err := s.drv.CallColabMcpTool(ctx, "get_cells", map[string]any{
		"cellIndexStart": start,
		"cellIndexEnd":   end,
		"includeOutputs": includeOutputs,
	})
	if err != nil {
		return nil, fmt.Errorf("get cells: %w", err)
	}
	var res struct {
		Cells []Cell `json:"cells"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("unmarshal cells: %w", err)
	}
	return res.Cells, nil
}

func (s *Service) AddCell(ctx context.Context, cellType string, index int, content string) (string, error) {
	cellType = strings.ToLower(strings.TrimSpace(cellType))
	if cellType == "markdown" || cellType == "text" {
		return s.AddTextCell(ctx, index, content)
	}
	return s.AddCodeCell(ctx, index, content)
}

func (s *Service) AddCodeCell(ctx context.Context, index int, code string) (string, error) {
	raw, err := s.drv.CallColabMcpTool(ctx, "add_code_cell", map[string]any{
		"cellIndex": index,
		"language":  "python",
		"code":      code,
	})
	if err != nil {
		return "", fmt.Errorf("add code cell: %w", err)
	}
	var res struct {
		NewCellID string `json:"newCellId"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", fmt.Errorf("unmarshal add cell response: %w", err)
	}
	return res.NewCellID, nil
}

func (s *Service) AddTextCell(ctx context.Context, index int, content string) (string, error) {
	raw, err := s.drv.CallColabMcpTool(ctx, "add_text_cell", map[string]any{
		"cellIndex": index,
		"content":   content,
	})
	if err != nil {
		return "", fmt.Errorf("add text cell: %w", err)
	}
	var res struct {
		NewCellID string `json:"newCellId"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", fmt.Errorf("unmarshal add text cell response: %w", err)
	}
	return res.NewCellID, nil
}

func (s *Service) UpdateCell(ctx context.Context, cellID, content string) error {
	if strings.TrimSpace(cellID) == "" {
		return errors.New("cell_id cannot be empty")
	}
	_, err := s.drv.CallColabMcpTool(ctx, "update_cell", map[string]any{
		"cellId":  cellID,
		"content": content,
	})
	if err != nil {
		return fmt.Errorf("update cell %s: %w", cellID, err)
	}
	return nil
}

func (s *Service) DeleteCell(ctx context.Context, cellID string) error {
	if strings.TrimSpace(cellID) == "" {
		return errors.New("cell_id cannot be empty")
	}
	_, err := s.drv.CallColabMcpTool(ctx, "delete_cell", map[string]any{
		"cellId": cellID,
	})
	if err != nil {
		return fmt.Errorf("delete cell %s: %w", cellID, err)
	}
	return nil
}

func (s *Service) MoveCell(ctx context.Context, cellID string, newIndex int) error {
	if strings.TrimSpace(cellID) == "" {
		return errors.New("cell_id cannot be empty")
	}
	_, err := s.drv.CallColabMcpTool(ctx, "move_cell", map[string]any{
		"cellId":    cellID,
		"cellIndex": newIndex,
	})
	if err != nil {
		return fmt.Errorf("move cell %s: %w", cellID, err)
	}
	return nil
}

func (s *Service) RunCodeCell(ctx context.Context, cellID string, waitSeconds ...int) (json.RawMessage, error) {
	if strings.TrimSpace(cellID) == "" {
		return nil, errors.New("cell_id cannot be empty")
	}
	waitMs := 8000
	if len(waitSeconds) > 0 {
		if waitSeconds[0] == 0 {
			waitMs = 0
		} else if waitSeconds[0] > 0 {
			if waitSeconds[0] > 30 {
				waitSeconds[0] = 30
			}
			waitMs = waitSeconds[0] * 1000
		}
	}
	raw, err := s.drv.CallColabMcpTool(ctx, "run_code_cell", map[string]any{
		"cellId": cellID,
		"waitMs": waitMs,
	})
	if err != nil {
		return nil, fmt.Errorf("run cell %s: %w", cellID, err)
	}
	return raw, nil
}

func (s *Service) EditCellLines(ctx context.Context, cellID string, startLine, endLine int, newContent string) (*EditLinesResult, error) {
	cells, err := s.GetCells(ctx, 0, 500, false)
	if err != nil {
		return nil, err
	}
	var target *Cell
	for i := range cells {
		if cells[i].ID == cellID {
			target = &cells[i]
			break
		}
	}
	if target == nil {
		return nil, domain.NewAppError(domain.CodeNotFound, fmt.Sprintf("cell not found: %s", cellID), "", http.StatusNotFound)
	}

	fullSource := strings.Join(target.Source, "")
	lines := strings.Split(fullSource, "\n")
	totalLines := len(lines)
	if startLine < 1 || endLine > totalLines || startLine > endLine {
		return nil, domain.NewAppError(domain.CodeInvalidInput, fmt.Sprintf("invalid line range %d-%d (cell has %d lines)", startLine, endLine, totalLines), "", http.StatusBadRequest)
	}

	replacementLines := strings.Split(newContent, "\n")
	newLines := make([]string, 0, totalLines-(endLine-startLine+1)+len(replacementLines))
	newLines = append(newLines, lines[:startLine-1]...)
	newLines = append(newLines, replacementLines...)
	newLines = append(newLines, lines[endLine:]...)

	newSource := strings.Join(newLines, "\n")
	if err := s.UpdateCell(ctx, cellID, newSource); err != nil {
		return nil, err
	}

	return &EditLinesResult{
		CellID:       cellID,
		Modified:     true,
		LinesChanged: endLine - startLine + 1,
		TotalLines:   len(newLines),
	}, nil
}

func (s *Service) GetCellWithLines(ctx context.Context, cellID string) (map[string]any, error) {
	cells, err := s.GetCells(ctx, 0, 500, true)
	if err != nil {
		return nil, err
	}
	for _, c := range cells {
		if c.ID == cellID {
			source := strings.Join(c.Source, "")
			lines := strings.Split(source, "\n")
			var sb strings.Builder
			for i, line := range lines {
				sb.WriteString(fmt.Sprintf("%3d | %s\n", i+1, line))
			}
			return map[string]any{
				"cell_id":         c.ID,
				"cell_type":       c.CellType,
				"total_lines":     len(lines),
				"numbered_source": sb.String(),
				"raw_source":      source,
				"outputs":         c.Outputs,
			}, nil
		}
	}
	return nil, domain.NewAppError(domain.CodeNotFound, fmt.Sprintf("cell not found: %s", cellID), "", http.StatusNotFound)
}

func (s *Service) SearchCells(ctx context.Context, query string) ([]CellSearchMatch, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return []CellSearchMatch{}, nil
	}
	cells, err := s.GetCells(ctx, 0, 500, false)
	if err != nil {
		return nil, err
	}
	qLower := strings.ToLower(q)
	matches := []CellSearchMatch{}
	for idx, c := range cells {
		source := strings.Join(c.Source, "")
		lines := strings.Split(source, "\n")
		for lineIdx, line := range lines {
			if strings.Contains(strings.ToLower(line), qLower) {
				matches = append(matches, CellSearchMatch{
					CellIndex:  idx,
					CellID:     c.ID,
					CellType:   c.CellType,
					LineNumber: lineIdx + 1,
					LineText:   line,
				})
			}
		}
	}
	return matches, nil
}

func joinJupyterText(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case []any:
		var sb strings.Builder
		for _, item := range val {
			if s, ok := item.(string); ok {
				sb.WriteString(s)
			}
		}
		return sb.String()
	case []string:
		return strings.Join(val, "")
	default:
		return ""
	}
}

func (s *Service) GetCellOutput(ctx context.Context, cellID string, saveImagesDir string) (*CellOutputReport, error) {
	cells, err := s.GetCells(ctx, 0, 500, true)
	if err != nil {
		return nil, err
	}
	var target *Cell
	for i := range cells {
		if cells[i].ID == cellID {
			target = &cells[i]
			break
		}
	}
	if target == nil {
		return nil, domain.NewAppError(domain.CodeNotFound, fmt.Sprintf("cell not found: %s", cellID), "", http.StatusNotFound)
	}

	report := &CellOutputReport{
		CellID:     target.ID,
		CellType:   target.CellType,
		RawOutputs: target.Outputs,
	}
	if report.RawOutputs == nil {
		report.RawOutputs = []map[string]any{}
	}

	saveDir := strings.TrimSpace(saveImagesDir)
	if saveDir != "" {
		if err := os.MkdirAll(saveDir, 0o755); err != nil {
			return nil, fmt.Errorf("create save_images_dir %s: %w", saveDir, err)
		}
	}

	var stdoutSB, stderrSB, resSB, errSB strings.Builder
	imgIdx := 0

	for _, out := range target.Outputs {
		outType, _ := out["output_type"].(string)
		switch outType {
		case "stream":
			name, _ := out["name"].(string)
			txt := joinJupyterText(out["text"])
			if name == "stderr" {
				stderrSB.WriteString(txt)
			} else {
				stdoutSB.WriteString(txt)
			}
		case "execute_result", "display_data":
			if data, ok := out["data"].(map[string]any); ok {
				if plain, okP := data["text/plain"]; okP {
					resSB.WriteString(joinJupyterText(plain))
				}
				if pngRaw, okPNG := data["image/png"]; okPNG {
					imgIdx++
					b64 := strings.TrimSpace(joinJupyterText(pngRaw))
					if saveDir != "" && b64 != "" {
						if decoded, errD := base64.StdEncoding.DecodeString(b64); errD == nil {
							outPath := filepath.Join(saveDir, fmt.Sprintf("%s_output_%d.png", cellID, imgIdx))
							if os.WriteFile(outPath, decoded, 0o644) == nil {
								report.SavedImages = append(report.SavedImages, outPath)
							}
						}
					}
				}
			}
		case "error":
			ename, _ := out["ename"].(string)
			evalue, _ := out["evalue"].(string)
			errSB.WriteString(fmt.Sprintf("%s: %s\n", ename, evalue))
		}
	}

	report.Stdout = stdoutSB.String()
	report.Stderr = stderrSB.String()
	report.TextResult = resSB.String()
	report.ErrorText = strings.TrimSpace(errSB.String())
	report.ImageCount = imgIdx
	return report, nil
}

func (s *Service) GetNotebookOutline(ctx context.Context) (*NotebookOutline, error) {
	cells, err := s.GetCells(ctx, 0, 500, false)
	if err != nil {
		return nil, err
	}
	outline := make([]OutlineItem, 0, len(cells))
	codeCount := 0
	mdCount := 0
	for i, c := range cells {
		source := strings.Join(c.Source, "")
		lines := strings.Split(source, "\n")
		summary := ""
		if len(lines) > 0 {
			summary = strings.TrimSpace(lines[0])
		}
		if c.CellType == "code" {
			codeCount++
		} else {
			mdCount++
		}
		outline = append(outline, OutlineItem{
			Index:     i,
			CellID:    c.ID,
			CellType:  c.CellType,
			Summary:   summary,
			LineCount: len(lines),
		})
	}
	return &NotebookOutline{
		TotalCells:    len(cells),
		CodeCells:     codeCount,
		MarkdownCells: mdCount,
		Outline:       outline,
	}, nil
}

func (s *Service) GetRunningCells(ctx context.Context, onlyActive bool) (*RunningCellsReport, error) {
	rep, err := s.drv.GetRunningCells(ctx)
	if err != nil {
		return nil, err
	}
	if !onlyActive {
		return rep, nil
	}
	filtered := make([]RunningCellInfo, 0, len(rep.RunningCells))
	for _, c := range rep.RunningCells {
		if c.IsRunning {
			filtered = append(filtered, c)
		}
	}
	return &RunningCellsReport{
		HasRunningCells: rep.HasRunningCells,
		TotalRunning:    rep.TotalRunning,
		RunningCells:    filtered,
	}, nil
}

func (s *Service) RunAll(ctx context.Context) error {
	return s.drv.RunAll(ctx)
}

func (s *Service) SaveNotebook(ctx context.Context) error {
	return s.drv.SaveNotebook(ctx)
}
