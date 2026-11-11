package controller

import (
	"encoding/json"
	"net/http"

	"github.com/hoangnecon/golabv2/internal/domain"
	"github.com/hoangnecon/golabv2/internal/service"
)

// ProjectController handles /api/v1/projects* endpoints.
type ProjectController struct {
	projectSvc *service.ProjectService
}

func NewProjectController(projectSvc *service.ProjectService) *ProjectController {
	return &ProjectController{projectSvc: projectSvc}
}

func (c *ProjectController) HandleList(w http.ResponseWriter, r *http.Request) {
	list, err := c.projectSvc.ListProjects(r.Context())
	if err != nil {
		WriteAppError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"projects": list})
}

func (c *ProjectController) HandleGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, err := c.projectSvc.GetProject(r.Context(), id)
	if err != nil {
		WriteAppError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, p)
}

func (c *ProjectController) HandleCreate(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name           string `json:"name"`
		NotebookURL    string `json:"notebook_url"`
		OwnerProfileID string `json:"owner_profile_id"`
		Lock           bool   `json:"lock"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		WriteError(w, http.StatusBadRequest, domain.CodeInvalidInput, "invalid JSON body")
		return
	}
	p, ownerSess, err := c.projectSvc.CreateProject(r.Context(), input.Name, input.NotebookURL, input.OwnerProfileID, input.Lock)
	if err != nil {
		WriteAppError(w, err)
		return
	}
	WriteJSON(w, http.StatusCreated, map[string]any{
		"project": p,
		"session": ownerSess,
	})
}

func (c *ProjectController) HandleUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var patch service.ProjectPatch
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		WriteError(w, http.StatusBadRequest, domain.CodeInvalidInput, "invalid JSON body")
		return
	}
	p, err := c.projectSvc.UpdateProject(r.Context(), id, patch)
	if err != nil {
		WriteAppError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, p)
}

func (c *ProjectController) HandleRotateToken(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, err := c.projectSvc.RotateToken(r.Context(), id)
	if err != nil {
		WriteAppError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, p)
}

func (c *ProjectController) HandleUnlock(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, err := c.projectSvc.UnlockProject(r.Context(), id)
	if err != nil {
		WriteAppError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, p)
}

func (c *ProjectController) HandleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	force := r.URL.Query().Get("force") == "true" || r.URL.Query().Get("force") == "1"
	if err := c.projectSvc.DeleteProject(r.Context(), id, force); err != nil {
		WriteAppError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"deleted": true, "id": id})
}
