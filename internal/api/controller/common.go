package controller

import (
	"encoding/json"
	"net/http"

	"github.com/hoangnecon/golabv2/internal/domain"
)

// WriteJSON writes a JSON payload with standard content-type and status code.
func WriteJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// WriteAppError maps any error into a structured domain AppError JSON response.
func WriteAppError(w http.ResponseWriter, err error) {
	appErr := domain.AsAppError(err)
	if appErr == nil {
		return
	}
	status := appErr.HTTPStatus
	if status <= 0 {
		status = http.StatusInternalServerError
	}
	WriteJSON(w, status, map[string]any{
		"error": appErr,
	})
}

// WriteError formats an explicit HTTP status + message as structured AppError JSON.
func WriteError(w http.ResponseWriter, status int, code string, message string) {
	if code == "" {
		code = domain.CodeInternalError
	}
	WriteJSON(w, status, map[string]any{
		"error": domain.NewAppError(code, message, "", status),
	})
}
