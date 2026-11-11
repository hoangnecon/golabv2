package controller

import (
	"net/http"
	"os"
	"time"

	"github.com/hoangnecon/golabv2/internal/service"
)

// WorkspaceController serves /api/v1/health and /api/v1/workspace.
type WorkspaceController struct {
	wsSvc      *service.WorkspaceService
	startedAt  time.Time
	version    string
	mcpHTTPURL string
}

func NewWorkspaceController(wsSvc *service.WorkspaceService, version string, mcpHTTPURL string) *WorkspaceController {
	return &WorkspaceController{
		wsSvc:      wsSvc,
		startedAt:  time.Now(),
		version:    version,
		mcpHTTPURL: mcpHTTPURL,
	}
}

func (c *WorkspaceController) HandleHealth(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]any{
		"status":       "ok",
		"pid":          os.Getpid(),
		"version":      c.version,
		"uptime_sec":   int64(time.Since(c.startedAt).Seconds()),
		"db_mode":      "wal",
		"mcp_http_url": c.mcpHTTPURL,
	})
}

func (c *WorkspaceController) HandleWorkspace(w http.ResponseWriter, r *http.Request) {
	snap, err := c.wsSvc.GetSnapshot(r.Context())
	if err != nil {
		WriteAppError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, snap)
}
