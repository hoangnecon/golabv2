package domain

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	TokenPrefixProject = "glp_"
	TokenPrefixSession = "gls_"
)

var bareDriveFileIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{15,80}$`)

// Project represents one canonical Colab Notebook governed by GoLab.
type Project struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	NotebookID      string    `json:"notebook_id"`
	NotebookURL     string    `json:"notebook_url"`
	OwnerProfileID  string    `json:"owner_profile_id"`
	ProjectToken    string    `json:"project_token"`
	LockedSessionID string    `json:"locked_session_id"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// GenerateToken generates a 24-byte random URL-safe token with the given prefix (e.g. "glp_" or "gls_").
func GenerateToken(prefix string) (string, error) {
	if prefix != TokenPrefixProject && prefix != TokenPrefixSession {
		return "", NewAppError(CodeInvalidInput, fmt.Sprintf("unsupported token prefix %q", prefix), "", http.StatusBadRequest)
	}
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate token entropy: %w", err)
	}
	return prefix + base64.RawURLEncoding.EncodeToString(buf), nil
}

// MaskTokenPrefix returns a non-secret display prefix such as "glp_8f2a9c1b..." for audit logs.
func MaskTokenPrefix(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	if len(token) <= 12 {
		return token[:minInt(len(token), 4)] + "..."
	}
	return token[:12] + "..."
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// NormalizeNotebookURL extracts a canonical notebook_id and canonical Colab URL.
// Supported inputs:
//   - Bare Drive file ID: "1YsugLzQOOw3CJUOiqhVJM0U7fP62x4Du"
//   - Colab Drive URL: "https://colab.research.google.com/drive/<FILE_ID>?usp=sharing#scrollTo=..."
//   - Google Drive URL: "https://drive.google.com/file/d/<FILE_ID>/view?..." or "https://drive.google.com/open?id=<FILE_ID>"
//   - Colab GitHub URL: "https://colab.research.google.com/github/<org>/<repo>/blob/<ref>/<path>"
//   - Colab Gist URL: "https://colab.research.google.com/gist/<user>/<gist_id>..."
func NormalizeNotebookURL(raw string) (notebookID string, canonicalURL string, err error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", "", NewAppError(CodeInvalidInput, "notebook URL cannot be empty", "Provide a Colab URL or Google Drive file ID.", http.StatusBadRequest)
	}

	if !strings.Contains(trimmed, "://") && !strings.Contains(trimmed, "/") {
		if bareDriveFileIDRe.MatchString(trimmed) {
			id := "drive/" + trimmed
			return id, "https://colab.research.google.com/" + id, nil
		}
		return "", "", NewAppError(CodeInvalidInput, fmt.Sprintf("invalid bare notebook file ID %q", trimmed), "Provide a valid Colab URL or Google Drive file ID.", http.StatusBadRequest)
	}

	u, parseErr := url.Parse(trimmed)
	if parseErr != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", "", NewAppError(CodeInvalidInput, fmt.Sprintf("invalid notebook URL %q", trimmed), "URL must start with https://colab.research.google.com/ or https://drive.google.com/.", http.StatusBadRequest)
	}

	host := strings.ToLower(u.Hostname())
	cleanPath := strings.Trim(u.Path, "/")
	parts := filterNonEmpty(strings.Split(cleanPath, "/"))

	switch host {
	case "colab.research.google.com", "colab.google.com":
		if len(parts) >= 2 && parts[0] == "drive" {
			fileID := parts[1]
			if fileID == "" {
				break
			}
			id := "drive/" + fileID
			return id, "https://colab.research.google.com/" + id, nil
		}
		if len(parts) >= 5 && parts[0] == "github" {
			id := strings.Join(parts, "/")
			return id, "https://colab.research.google.com/" + id, nil
		}
		if len(parts) >= 3 && parts[0] == "gist" {
			id := strings.Join(parts, "/")
			return id, "https://colab.research.google.com/" + id, nil
		}
	case "drive.google.com":
		// https://drive.google.com/file/d/<FILE_ID>/view
		if len(parts) >= 3 && parts[0] == "file" && parts[1] == "d" && parts[2] != "" {
			id := "drive/" + parts[2]
			return id, "https://colab.research.google.com/" + id, nil
		}
		if qid := strings.TrimSpace(u.Query().Get("id")); qid != "" {
			id := "drive/" + qid
			return id, "https://colab.research.google.com/" + id, nil
		}
	}

	return "", "", NewAppError(
		CodeInvalidInput,
		fmt.Sprintf("unsupported Colab notebook URL %q", trimmed),
		"Use https://colab.research.google.com/drive/<id>, /github/<org>/<repo>/blob/<ref>/<path>, /gist/<user>/<id>, or https://drive.google.com/file/d/<id>.",
		http.StatusBadRequest,
	)
}

func filterNonEmpty(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// DefaultNotebookTitle derives a clean default project name from a canonical notebook ID.
func DefaultNotebookTitle(notebookID string) string {
	parts := strings.Split(notebookID, "/")
	if len(parts) == 0 {
		return "Colab Notebook"
	}
	last := parts[len(parts)-1]
	if strings.HasSuffix(last, ".ipynb") {
		return strings.TrimSuffix(last, ".ipynb")
	}
	if len(last) > 12 {
		return "Notebook " + last[:8]
	}
	return "Notebook " + last
}

// ProjectRepository defines persistence operations for Projects.
type ProjectRepository interface {
	CreateProject(ctx context.Context, p *Project) error
	GetProject(ctx context.Context, id string) (*Project, error)
	GetProjectByNotebookID(ctx context.Context, notebookID string) (*Project, error)
	GetProjectByToken(ctx context.Context, token string) (*Project, error)
	ListProjects(ctx context.Context) ([]*Project, error)
	UpdateProject(ctx context.Context, p *Project) error
	DeleteProject(ctx context.Context, id string) error
}
