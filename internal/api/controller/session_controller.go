package controller

import (
	"encoding/json"
	"net/http"

	"github.com/hoangnecon/golabv2/internal/domain"
	"github.com/hoangnecon/golabv2/internal/service"
)

// SessionController handles /api/v1/sessions* governance and read-only observation endpoints.
// Note: Colab-duplicating runtime mutation buttons (/connect, /disconnect, /runall, /save, /accelerator)
// are intentionally excluded per Spec v2 §1.1.
type SessionController struct {
	sessionSvc *service.SessionService
}

func NewSessionController(sessionSvc *service.SessionService) *SessionController {
	return &SessionController{sessionSvc: sessionSvc}
}

func (c *SessionController) HandleList(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("project_id")
	sessions, err := c.sessionSvc.ListSessions(r.Context(), projectID)
	if err != nil {
		WriteAppError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
}

func (c *SessionController) HandleCreate(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ProjectID string `json:"project_id"`
		ProfileID string `json:"profile_id"`
		Name      string `json:"name"`
		Lock      bool   `json:"lock"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		WriteError(w, http.StatusBadRequest, domain.CodeInvalidInput, "invalid JSON body")
		return
	}
	sess, err := c.sessionSvc.CreateSession(r.Context(), input.ProjectID, input.ProfileID, input.Name, input.Lock)
	if err != nil {
		WriteAppError(w, err)
		return
	}
	WriteJSON(w, http.StatusCreated, sess)
}

func (c *SessionController) HandleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	closeTab := r.URL.Query().Get("close_tab") == "true" || r.URL.Query().Get("close_tab") == "1"
	if err := c.sessionSvc.DeleteSession(r.Context(), id, closeTab); err != nil {
		WriteAppError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"deleted": true, "id": id})
}

func (c *SessionController) HandleLock(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	input := struct {
		ForceSwitch bool  `json:"force_switch"`
		OpenTab     *bool `json:"open_tab"`
	}{}
	_ = json.NewDecoder(r.Body).Decode(&input)
	openTab := true
	if input.OpenTab != nil {
		openTab = *input.OpenTab
	}
	sess, err := c.sessionSvc.LockSession(r.Context(), id, input.ForceSwitch, openTab)
	if err != nil {
		WriteAppError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, sess)
}

func (c *SessionController) HandleUnlock(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess, err := c.sessionSvc.UnlockSession(r.Context(), id)
	if err != nil {
		WriteAppError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, sess)
}

func (c *SessionController) HandleOpenTab(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess, err := c.sessionSvc.OpenTab(r.Context(), id)
	if err != nil {
		WriteAppError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, sess)
}

func (c *SessionController) HandleRotateToken(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess, err := c.sessionSvc.RotateToken(r.Context(), id)
	if err != nil {
		WriteAppError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, sess)
}

func (c *SessionController) HandleObserve(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	snap, err := c.sessionSvc.ObserveSession(r.Context(), id)
	if err != nil {
		WriteAppError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, snap)
}
