package mcpserver

import (
	"net/http"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewHTTPHandler mounts the GoLab v2 Streamable HTTP MCP endpoint (/mcp).
// Each distinct connection token (from ?token= or Authorization: Bearer <tok>) gets a cached *Server
// so multiple IDE windows with different Project/Session tokens operate concurrently without conflict.
func NewHTTPHandler(deps Dependencies) http.Handler {
	var mu sync.Mutex
	serversByToken := make(map[string]*mcp.Server)

	getServer := func(r *http.Request) *mcp.Server {
		tok := extractRequestToken(r)
		mu.Lock()
		defer mu.Unlock()
		if srv, ok := serversByToken[tok]; ok {
			return srv
		}
		srv := NewServer(deps, tok).MCP()
		serversByToken[tok] = srv
		return srv
	}

	return mcp.NewStreamableHTTPHandler(getServer, &mcp.StreamableHTTPOptions{
		JSONResponse:               true,
		DisableLocalhostProtection: true, // Enforced uniformly by outer api.guardMiddleware
	})
}

func extractRequestToken(r *http.Request) string {
	if r == nil {
		return ""
	}
	if q := strings.TrimSpace(r.URL.Query().Get("token")); q != "" {
		return q
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	if hdr := strings.TrimSpace(r.Header.Get("X-GoLab-Token")); hdr != "" {
		return hdr
	}
	return ""
}
