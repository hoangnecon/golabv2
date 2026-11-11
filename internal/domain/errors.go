package domain

import (
	"errors"
	"fmt"
	"net/http"
)

// ErrNotFound indicates a requested entity does not exist.
var ErrNotFound = errors.New("not found")

// Canonical GoLab error codes shared across REST API and MCP envelopes.
const (
	CodeInvalidInput                 = "GOLAB_INVALID_INPUT"
	CodeNotFound                     = "GOLAB_NOT_FOUND"
	CodeConflict                     = "GOLAB_CONFLICT"
	CodeProjectUnlocked              = "GOLAB_PROJECT_UNLOCKED"
	CodeProjectLockedByOtherAccount  = "GOLAB_PROJECT_LOCKED_BY_OTHER_ACCOUNT"
	CodeSessionNotLocked             = "GOLAB_SESSION_NOT_LOCKED"
	CodeNoLockedSession              = "GOLAB_NO_LOCKED_SESSION"
	CodeAmbiguousMultiProjectSession = "GOLAB_AMBIGUOUS_MULTI_PROJECT_SESSION"
	CodeInvalidToken                 = "GOLAB_INVALID_TOKEN"
	CodePermissionReadOnly           = "GOLAB_PERMISSION_READ_ONLY"
	CodePermissionDenied             = "GOLAB_PERMISSION_DENIED"
	CodeAuthRequired                 = "GOLAB_AUTH_REQUIRED"
	CodeBrowserStopped               = "GOLAB_BROWSER_STOPPED"
	CodeCDPPortConflict              = "GOLAB_CDP_PORT_CONFLICT"
	CodeTabClosed                    = "GOLAB_TAB_CLOSED"
	CodeKernelDisconnected           = "GOLAB_KERNEL_DISCONNECTED"
	CodeDriveUnmounted               = "GOLAB_DRIVE_UNMOUNTED"
	CodeDriveLocalShadowDir          = "GOLAB_DRIVE_LOCAL_SHADOW_DIR"
	CodeDriveOAuthRequired           = "GOLAB_DRIVE_OAUTH_INTERACTION_REQUIRED"
	CodeInternalError                = "GOLAB_INTERNAL_ERROR"
)

// AppError represents a structured, actionable domain error.
type AppError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	Hint       string `json:"hint,omitempty"`
	HTTPStatus int    `json:"-"`
	Cause      error  `json:"-"`
}

func (e *AppError) Error() string {
	if e == nil {
		return ""
	}
	if e.Hint != "" {
		return fmt.Sprintf("[%s] %s (%s)", e.Code, e.Message, e.Hint)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func (e *AppError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// NewAppError creates a new AppError with an explicit HTTP status.
func NewAppError(code, message, hint string, httpStatus int) *AppError {
	if httpStatus <= 0 {
		httpStatus = http.StatusBadRequest
	}
	return &AppError{
		Code:       code,
		Message:    message,
		Hint:       hint,
		HTTPStatus: httpStatus,
	}
}

// WrapAppError wraps an underlying error with a domain AppError.
func WrapAppError(code, message, hint string, httpStatus int, cause error) *AppError {
	err := NewAppError(code, message, hint, httpStatus)
	err.Cause = cause
	return err
}

// AsAppError extracts an *AppError from err or converts ErrNotFound / generic errors.
func AsAppError(err error) *AppError {
	if err == nil {
		return nil
	}
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr
	}
	if errors.Is(err, ErrNotFound) {
		return NewAppError(CodeNotFound, err.Error(), "Check the requested resource ID.", http.StatusNotFound)
	}
	return WrapAppError(CodeInternalError, err.Error(), "", http.StatusInternalServerError, err)
}
