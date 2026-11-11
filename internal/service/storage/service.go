package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/hoangnecon/golabv2/internal/domain"
)

// Service provides non-blocking file and directory operations via Jupyter /api/contents
// with ground-truth Google Drive FUSE mount verification on content/drive/... paths.
type Service struct {
	drv domain.RuntimeDriver
}

func NewService(drv domain.RuntimeDriver) *Service {
	return &Service{drv: drv}
}

func cleanColabPath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, "/")
	if p == "" {
		return ""
	}
	segments := strings.Split(p, "/")
	escaped := make([]string, 0, len(segments))
	for _, s := range segments {
		if s == "" || s == "." {
			continue
		}
		escaped = append(escaped, url.PathEscape(s))
	}
	return strings.Join(escaped, "/")
}

func isDrivePath(cleaned string) bool {
	return cleaned == "content/drive" ||
		strings.HasPrefix(cleaned, "content/drive/") ||
		cleaned == "drive" ||
		strings.HasPrefix(cleaned, "drive/")
}

func (s *Service) verifyDriveGuardrail(ctx context.Context, rawPath, cleaned string) error {
	if !isDrivePath(cleaned) {
		return nil
	}
	ds, err := s.drv.CheckDriveStatus(ctx, true)
	if err != nil {
		return err
	}
	if ds == nil {
		return nil
	}
	if ds.Status == "corrupted_local_dir" {
		return domain.NewAppError(
			domain.CodeDriveLocalShadowDir,
			fmt.Sprintf("refusing %q because /content/drive is a local unmounted shadow directory on ephemeral VM SSD", rawPath),
			"Clean /content/drive and call colab_mount_drive to mount real Google Drive FUSE.",
			http.StatusConflict,
		)
	}
	if !ds.Mounted {
		return domain.NewAppError(
			domain.CodeDriveUnmounted,
			fmt.Sprintf("Google Drive is not mounted; path %q is inaccessible", rawPath),
			"Call colab_mount_drive first to mount Google Drive at /content/drive.",
			http.StatusConflict,
		)
	}
	return nil
}

// StatPath inspects metadata of a file or directory without downloading its payload.
func (s *Service) StatPath(ctx context.Context, path string) (*StatResult, error) {
	cleaned := cleanColabPath(path)
	if err := s.verifyDriveGuardrail(ctx, path, cleaned); err != nil {
		return nil, err
	}
	apiPath := "/api/contents/" + cleaned + "?content=0"
	if cleaned == "" {
		apiPath = "/api/contents?content=0"
	}
	resp, err := s.drv.FetchKernelXHR(ctx, apiPath, domain.FetchOptions{Method: "GET"})
	if err != nil {
		return nil, err
	}
	if resp.Status == 404 {
		return &StatResult{Exists: false, Path: cleaned}, nil
	}
	if !resp.OK {
		return nil, fmt.Errorf("stat %q failed with status %d: %s", path, resp.Status, resp.RawText)
	}
	var raw struct {
		Name         string     `json:"name"`
		Path         string     `json:"path"`
		Type         string     `json:"type"`
		Size         *int64     `json:"size"`
		Writable     bool       `json:"writable"`
		MIMEType     string     `json:"mimetype"`
		LastModified *time.Time `json:"last_modified"`
	}
	if err := json.Unmarshal(resp.Body, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal stat response: %w", err)
	}
	return &StatResult{
		Exists:       true,
		Name:         raw.Name,
		Path:         raw.Path,
		Type:         raw.Type,
		Size:         raw.Size,
		Writable:     raw.Writable,
		MIMEType:     raw.MIMEType,
		LastModified: raw.LastModified,
	}, nil
}

func (s *Service) ReadFile(ctx context.Context, filePath string, format string) (*FileReadResult, error) {
	cleaned := cleanColabPath(filePath)
	if err := s.verifyDriveGuardrail(ctx, filePath, cleaned); err != nil {
		return nil, err
	}
	if format != "base64" {
		format = "text"
	}

	apiPath := fmt.Sprintf("/api/contents/%s?content=1&format=%s", cleaned, format)
	resp, err := s.drv.FetchKernelXHR(ctx, apiPath, domain.FetchOptions{Method: "GET"})
	if err != nil {
		return nil, fmt.Errorf("read file %q: %w", filePath, err)
	}
	if !resp.OK {
		if resp.Status == 404 {
			return nil, domain.NewAppError(domain.CodeNotFound, fmt.Sprintf("file not found: %s", filePath), "", http.StatusNotFound)
		}
		return nil, fmt.Errorf("read file failed with status %d: %s", resp.Status, resp.RawText)
	}

	var rawEntry struct {
		Name    string `json:"name"`
		Path    string `json:"path"`
		Type    string `json:"type"`
		Size    int64  `json:"size"`
		Format  string `json:"format"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(resp.Body, &rawEntry); err != nil {
		return nil, fmt.Errorf("unmarshal file read response: %w", err)
	}
	if rawEntry.Type == "directory" {
		return nil, domain.NewAppError(domain.CodeInvalidInput, fmt.Sprintf("path %q is a directory, not a file", filePath), "Use colab_list_directory instead.", http.StatusBadRequest)
	}

	return &FileReadResult{
		Path:    rawEntry.Path,
		Name:    rawEntry.Name,
		Type:    rawEntry.Type,
		Size:    rawEntry.Size,
		Format:  rawEntry.Format,
		Content: rawEntry.Content,
	}, nil
}

func (s *Service) WriteFile(ctx context.Context, filePath string, content string, format string) (*FileWriteResult, error) {
	cleaned := cleanColabPath(filePath)
	if cleaned == "" {
		return nil, domain.NewAppError(domain.CodeInvalidInput, "file path cannot be empty", "", http.StatusBadRequest)
	}
	if err := s.verifyDriveGuardrail(ctx, filePath, cleaned); err != nil {
		return nil, err
	}
	if format != "base64" {
		format = "text"
	}

	bodyBytes, err := json.Marshal(map[string]any{
		"name":    cleaned,
		"path":    cleaned,
		"type":    "file",
		"format":  format,
		"content": content,
	})
	if err != nil {
		return nil, err
	}

	resp, err := s.drv.FetchKernelXHR(ctx, "/api/contents/"+cleaned, domain.FetchOptions{
		Method:  "PUT",
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    string(bodyBytes),
	})
	if err != nil {
		return nil, fmt.Errorf("write file %q: %w", filePath, err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("write file failed with status %d: %s", resp.Status, resp.RawText)
	}

	var rawEntry struct {
		Name string `json:"name"`
		Path string `json:"path"`
		Type string `json:"type"`
		Size int64  `json:"size"`
	}
	if err := json.Unmarshal(resp.Body, &rawEntry); err != nil {
		return nil, fmt.Errorf("unmarshal write response: %w", err)
	}

	return &FileWriteResult{
		Path:    rawEntry.Path,
		Name:    rawEntry.Name,
		Size:    rawEntry.Size,
		Type:    rawEntry.Type,
		Created: true,
	}, nil
}

func (s *Service) ListDirectory(ctx context.Context, dirPath string) (*DirectoryListing, error) {
	cleaned := cleanColabPath(dirPath)
	if err := s.verifyDriveGuardrail(ctx, dirPath, cleaned); err != nil {
		return nil, err
	}
	apiPath := "/api/contents/" + cleaned + "?content=1"
	if cleaned == "" {
		apiPath = "/api/contents?content=1"
	}

	resp, err := s.drv.FetchKernelXHR(ctx, apiPath, domain.FetchOptions{Method: "GET"})
	if err != nil {
		return nil, fmt.Errorf("list directory %q: %w", dirPath, err)
	}
	if !resp.OK {
		if resp.Status == 404 {
			return nil, domain.NewAppError(domain.CodeNotFound, fmt.Sprintf("directory not found: %s", dirPath), "", http.StatusNotFound)
		}
		return nil, fmt.Errorf("list directory failed with status %d: %s", resp.Status, resp.RawText)
	}

	var rawDir struct {
		Name     string      `json:"name"`
		Path     string      `json:"path"`
		Type     string      `json:"type"`
		Writable bool        `json:"writable"`
		Content  []FileEntry `json:"content"`
	}
	if err := json.Unmarshal(resp.Body, &rawDir); err != nil {
		return nil, fmt.Errorf("unmarshal list directory response: %w", err)
	}

	return &DirectoryListing{
		Path:         rawDir.Path,
		Name:         rawDir.Name,
		Type:         rawDir.Type,
		Writable:     rawDir.Writable,
		Entries:      rawDir.Content,
		TotalEntries: len(rawDir.Content),
	}, nil
}

func (s *Service) CreateDirectory(ctx context.Context, dirPath string) error {
	cleaned := cleanColabPath(dirPath)
	if cleaned == "" {
		return domain.NewAppError(domain.CodeInvalidInput, "directory path cannot be empty", "", http.StatusBadRequest)
	}
	if err := s.verifyDriveGuardrail(ctx, dirPath, cleaned); err != nil {
		return err
	}

	bodyBytes, _ := json.Marshal(map[string]any{"type": "directory"})
	resp, err := s.drv.FetchKernelXHR(ctx, "/api/contents/"+cleaned, domain.FetchOptions{
		Method:  "PUT",
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    string(bodyBytes),
	})
	if err != nil {
		return fmt.Errorf("create directory %q: %w", dirPath, err)
	}
	if !resp.OK {
		return fmt.Errorf("create directory failed with status %d: %s", resp.Status, resp.RawText)
	}
	return nil
}

func (s *Service) DeleteFile(ctx context.Context, path string) error {
	cleaned := cleanColabPath(path)
	if cleaned == "" {
		return domain.NewAppError(domain.CodeInvalidInput, "path cannot be empty", "", http.StatusBadRequest)
	}
	if err := s.verifyDriveGuardrail(ctx, path, cleaned); err != nil {
		return err
	}

	resp, err := s.drv.FetchKernelXHR(ctx, "/api/contents/"+cleaned, domain.FetchOptions{Method: "DELETE"})
	if err != nil {
		return fmt.Errorf("delete %q: %w", path, err)
	}
	if resp.Status != 204 && !resp.OK {
		return fmt.Errorf("delete failed with status %d: %s", resp.Status, resp.RawText)
	}
	return nil
}

func (s *Service) MoveFile(ctx context.Context, oldPath, newPath string) error {
	cleanedOld := cleanColabPath(oldPath)
	cleanedNew := cleanColabPath(newPath)
	if cleanedOld == "" || cleanedNew == "" {
		return domain.NewAppError(domain.CodeInvalidInput, "old_path and new_path cannot be empty", "", http.StatusBadRequest)
	}
	if err := s.verifyDriveGuardrail(ctx, oldPath, cleanedOld); err != nil {
		return err
	}
	if err := s.verifyDriveGuardrail(ctx, newPath, cleanedNew); err != nil {
		return err
	}

	bodyBytes, _ := json.Marshal(map[string]any{"path": cleanedNew})
	resp, err := s.drv.FetchKernelXHR(ctx, "/api/contents/"+cleanedOld, domain.FetchOptions{
		Method:  "PATCH",
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    string(bodyBytes),
	})
	if err != nil {
		return fmt.Errorf("move %q -> %q: %w", oldPath, newPath, err)
	}
	if !resp.OK {
		return fmt.Errorf("move failed with status %d: %s", resp.Status, resp.RawText)
	}
	return nil
}
