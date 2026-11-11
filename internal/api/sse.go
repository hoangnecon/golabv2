package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/hoangnecon/golabv2/internal/api/controller"
	"github.com/hoangnecon/golabv2/internal/domain"
)

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		controller.WriteError(w, http.StatusInternalServerError, domain.CodeInternalError, "streaming unsupported")
		return
	}

	// Disable any HTTP server write deadline on this long-lived SSE stream.
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ctx := r.Context()

	// Subscribe BEFORE capturing the initial workspace snapshot so zero live events are missed.
	var eventsCh <-chan domain.WorkspaceEvent
	if s.deps.EventBus != nil {
		eventsCh = s.deps.EventBus.Subscribe(ctx)
	}

	snap, err := s.deps.WorkspaceSvc.GetSnapshot(ctx)
	if err != nil {
		controller.WriteAppError(w, err)
		return
	}

	initFrame, err := json.Marshal(domain.WorkspaceEvent{
		Type:      domain.EventWorkspaceSnapshot,
		Timestamp: time.Now().UTC(),
		Data:      snap,
	})
	if err != nil {
		controller.WriteError(w, http.StatusInternalServerError, domain.CodeInternalError, "failed to marshal workspace snapshot")
		return
	}

	_, _ = fmt.Fprintf(w, "data: %s\n\n", initFrame)
	flusher.Flush()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case evt, ok := <-eventsCh:
			if !ok {
				return
			}
			payload, err := json.Marshal(evt)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
