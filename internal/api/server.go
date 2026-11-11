package api

import (
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/hoangnecon/golabv2/internal/api/controller"
	"github.com/hoangnecon/golabv2/internal/domain"
	"github.com/hoangnecon/golabv2/internal/service"
)

// Dependencies groups all services and ports injected into the HTTP Control Plane Server.
type Dependencies struct {
	WorkspaceSvc *service.WorkspaceService
	ProfileSvc   *service.ProfileService
	ProjectSvc   *service.ProjectService
	SessionSvc   *service.SessionService
	ActivitySvc  *service.ActivityService
	EventBus     domain.EventBus
	MCPHandler   http.Handler
	StaticFS     fs.FS
	Version      string
	MCPHTTPURL   string
}

// Server provides the HTTP Control Plane v1 REST API, typed SSE stream, /mcp Streamable HTTP endpoint, and embedded UI SPA.
type Server struct {
	deps Dependencies
}

// NewServer constructs a Control-Plane HTTP Server with pure-domain service dependencies.
func NewServer(deps Dependencies) *Server {
	if deps.Version == "" {
		deps.Version = "v2.0.0"
	}
	return &Server{deps: deps}
}

type ioReadSeeker interface {
	Read(p []byte) (n int, err error)
	Seek(offset int64, whence int) (int64, error)
}

// Handler mounts all /api/v1/* endpoints, /mcp Streamable HTTP, and static SPA assets behind the loopback Host/Origin guard.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	wsCtrl := controller.NewWorkspaceController(s.deps.WorkspaceSvc, s.deps.Version, s.deps.MCPHTTPURL)
	profileCtrl := controller.NewProfileController(s.deps.ProfileSvc)
	projectCtrl := controller.NewProjectController(s.deps.ProjectSvc)
	sessionCtrl := controller.NewSessionController(s.deps.SessionSvc)
	activityCtrl := controller.NewActivityController(s.deps.ActivitySvc)

	// Workspace, Health & Typed SSE Stream
	mux.HandleFunc("GET /api/v1/health", wsCtrl.HandleHealth)
	mux.HandleFunc("GET /api/v1/workspace", wsCtrl.HandleWorkspace)
	mux.HandleFunc("GET /api/v1/events", s.handleSSE)

	// Account Profiles & Compute Quota
	mux.HandleFunc("GET /api/v1/profiles", profileCtrl.HandleList)
	mux.HandleFunc("POST /api/v1/profiles", profileCtrl.HandleCreate)
	mux.HandleFunc("PATCH /api/v1/profiles/{id}", profileCtrl.HandleUpdate)
	mux.HandleFunc("DELETE /api/v1/profiles/{id}", profileCtrl.HandleDelete)
	mux.HandleFunc("POST /api/v1/profiles/{id}/launch", profileCtrl.HandleLaunch)
	mux.HandleFunc("POST /api/v1/profiles/{id}/stop", profileCtrl.HandleStop)
	mux.HandleFunc("GET /api/v1/profiles/{id}/quota", profileCtrl.HandleQuota)

	// Canonical Notebook Projects & Token Rotation
	mux.HandleFunc("GET /api/v1/projects", projectCtrl.HandleList)
	mux.HandleFunc("POST /api/v1/projects", projectCtrl.HandleCreate)
	mux.HandleFunc("GET /api/v1/projects/{id}", projectCtrl.HandleGet)
	mux.HandleFunc("PATCH /api/v1/projects/{id}", projectCtrl.HandleUpdate)
	mux.HandleFunc("DELETE /api/v1/projects/{id}", projectCtrl.HandleDelete)
	mux.HandleFunc("POST /api/v1/projects/{id}/rotate-token", projectCtrl.HandleRotateToken)
	mux.HandleFunc("POST /api/v1/projects/{id}/unlock", projectCtrl.HandleUnlock)

	// Account Sessions, Per-Project Exclusive Lock & Live Read-Only Observation
	mux.HandleFunc("GET /api/v1/sessions", sessionCtrl.HandleList)
	mux.HandleFunc("POST /api/v1/sessions", sessionCtrl.HandleCreate)
	mux.HandleFunc("DELETE /api/v1/sessions/{id}", sessionCtrl.HandleDelete)
	mux.HandleFunc("POST /api/v1/sessions/{id}/lock", sessionCtrl.HandleLock)
	mux.HandleFunc("POST /api/v1/sessions/{id}/unlock", sessionCtrl.HandleUnlock)
	mux.HandleFunc("POST /api/v1/sessions/{id}/open-tab", sessionCtrl.HandleOpenTab)
	mux.HandleFunc("POST /api/v1/sessions/{id}/rotate-token", sessionCtrl.HandleRotateToken)
	mux.HandleFunc("GET /api/v1/sessions/{id}/observe", sessionCtrl.HandleObserve)

	// Faceted Tool-Call Audit Logs & Payload Details
	mux.HandleFunc("GET /api/v1/activity", activityCtrl.HandleList)
	mux.HandleFunc("GET /api/v1/activity/{id}", activityCtrl.HandleGet)

	// Streamable HTTP MCP Endpoint
	if s.deps.MCPHandler != nil {
		mux.Handle("/mcp", s.deps.MCPHandler)
		mux.Handle("/mcp/", s.deps.MCPHandler)
	}

	// Embedded UI SPA assets / JSON fallback
	if s.deps.StaticFS != nil {
		fileServer := http.FileServer(http.FS(s.deps.StaticFS))
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				controller.WriteError(w, http.StatusNotFound, domain.CodeNotFound, "API endpoint not found")
				return
			}
			path := strings.TrimPrefix(r.URL.Path, "/")
			if path == "" {
				path = "index.html"
			}
			f, err := s.deps.StaticFS.Open(path)
			if err != nil {
				indexFile, errIndex := s.deps.StaticFS.Open("index.html")
				if errIndex == nil {
					defer indexFile.Close()
					if rs, ok := indexFile.(ioReadSeeker); ok {
						http.ServeContent(w, r, "index.html", time.Now(), rs)
						return
					}
				}
			} else {
				_ = f.Close()
			}
			fileServer.ServeHTTP(w, r)
		})
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				controller.WriteError(w, http.StatusNotFound, domain.CodeNotFound, "API endpoint not found")
				return
			}
			controller.WriteJSON(w, http.StatusOK, map[string]any{
				"message": "GoLab Control Plane Daemon running.",
				"version": s.deps.Version,
			})
		})
	}

	return guardMiddleware(mux)
}
