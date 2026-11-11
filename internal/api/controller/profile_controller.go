package controller

import (
	"encoding/json"
	"net/http"

	"github.com/hoangnecon/golabv2/internal/domain"
	"github.com/hoangnecon/golabv2/internal/service"
)

// ProfileController handles /api/v1/profiles* endpoints.
type ProfileController struct {
	profileSvc *service.ProfileService
}

func NewProfileController(profileSvc *service.ProfileService) *ProfileController {
	return &ProfileController{profileSvc: profileSvc}
}

func (c *ProfileController) HandleList(w http.ResponseWriter, r *http.Request) {
	profiles, err := c.profileSvc.ListProfiles(r.Context())
	if err != nil {
		WriteAppError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"profiles": profiles})
}

func (c *ProfileController) HandleCreate(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name         string             `json:"name"`
		AccountEmail string             `json:"account_email"`
		Tier         domain.ProfileTier `json:"tier"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		WriteError(w, http.StatusBadRequest, domain.CodeInvalidInput, "invalid JSON body")
		return
	}
	p, err := c.profileSvc.CreateProfile(r.Context(), input.Name, input.AccountEmail, input.Tier)
	if err != nil {
		WriteAppError(w, err)
		return
	}
	WriteJSON(w, http.StatusCreated, p)
}

func (c *ProfileController) HandleUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var patch service.ProfilePatch
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		WriteError(w, http.StatusBadRequest, domain.CodeInvalidInput, "invalid JSON body")
		return
	}
	p, err := c.profileSvc.UpdateProfile(r.Context(), id, patch)
	if err != nil {
		WriteAppError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, p)
}

func (c *ProfileController) HandleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := c.profileSvc.DeleteProfile(r.Context(), id); err != nil {
		WriteAppError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"deleted": true, "id": id})
}

func (c *ProfileController) HandleLaunch(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var input struct {
		InitialURL string `json:"initial_url"`
	}
	_ = json.NewDecoder(r.Body).Decode(&input)
	p, err := c.profileSvc.LaunchProfile(r.Context(), id, input.InitialURL)
	if err != nil {
		WriteAppError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, p)
}

func (c *ProfileController) HandleStop(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, err := c.profileSvc.StopProfile(r.Context(), id)
	if err != nil {
		WriteAppError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, p)
}

func (c *ProfileController) HandleQuota(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	q, err := c.profileSvc.GetQuota(r.Context(), id)
	if err != nil {
		WriteAppError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, q)
}
