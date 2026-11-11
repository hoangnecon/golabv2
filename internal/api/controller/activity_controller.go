package controller

import (
	"net/http"
	"strconv"

	"github.com/hoangnecon/golabv2/internal/domain"
	"github.com/hoangnecon/golabv2/internal/service"
)

// ActivityController serves /api/v1/activity faceted cursor list and /api/v1/activity/{id} payload details.
type ActivityController struct {
	activitySvc *service.ActivityService
}

func NewActivityController(activitySvc *service.ActivityService) *ActivityController {
	return &ActivityController{activitySvc: activitySvc}
}

func (c *ActivityController) HandleList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var cursor int64
	if raw := q.Get("cursor"); raw != "" {
		if v, err := strconv.ParseInt(raw, 10, 64); err == nil && v > 0 {
			cursor = v
		}
	}
	limit := 50
	if raw := q.Get("limit"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 && v <= 200 {
			limit = v
		}
	}

	filter := domain.ActivityFilter{
		Cursor:    cursor,
		Limit:     limit,
		ProjectID: q.Get("project_id"),
		SessionID: q.Get("session_id"),
		ProfileID: q.Get("profile_id"),
		Status:    q.Get("status"),
		ToolName:  q.Get("tool"),
	}
	if filter.ToolName == "" {
		filter.ToolName = q.Get("tool_name")
	}

	items, nextCursor, err := c.activitySvc.List(r.Context(), filter)
	if err != nil {
		WriteAppError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"activity":    items,
		"next_cursor": nextCursor,
	})
}

func (c *ActivityController) HandleGet(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		WriteError(w, http.StatusBadRequest, domain.CodeInvalidInput, "invalid activity id")
		return
	}
	detail, err := c.activitySvc.Get(r.Context(), id)
	if err != nil {
		WriteAppError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, detail)
}
