package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hoangnecon/golabv2/internal/domain"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// maxPayloadBytes caps each stored request/response body at 64 KiB.
const maxPayloadBytes = 64 << 10

const redacted = "[redacted]"

type callAuditMeta struct {
	projectID   string
	sessionID   string
	profileID   string
	errorCode   string
	errorMsg    string
}

type callAuditKey struct{}

func setCallAuditTarget(ctx context.Context, t *domain.ResolvedTarget) {
	if meta, ok := ctx.Value(callAuditKey{}).(*callAuditMeta); ok && meta != nil && t != nil {
		if t.Project != nil {
			meta.projectID = t.Project.ID
		}
		if t.Session != nil {
			meta.sessionID = t.Session.ID
		}
		if t.Profile != nil {
			meta.profileID = t.Profile.ID
		}
	}
}

func setCallAuditIDs(ctx context.Context, projectID, sessionID, profileID string) {
	if meta, ok := ctx.Value(callAuditKey{}).(*callAuditMeta); ok && meta != nil {
		if projectID != "" {
			meta.projectID = projectID
		}
		if sessionID != "" {
			meta.sessionID = sessionID
		}
		if profileID != "" {
			meta.profileID = profileID
		}
	}
}

func setCallAuditError(ctx context.Context, err error) {
	if err == nil {
		return
	}
	if meta, ok := ctx.Value(callAuditKey{}).(*callAuditMeta); ok && meta != nil {
		appErr := domain.AsAppError(err)
		if appErr != nil {
			meta.errorCode = appErr.Code
			meta.errorMsg = appErr.Error()
		} else {
			meta.errorCode = domain.CodeInternalError
			meta.errorMsg = err.Error()
		}
	}
}

// captureToolCalls is outermost receiving middleware that records 100% of tools/call invocations
// (including pre-session token/lock/permission errors and schema rejections) into ActivityService.
func (s *Server) captureToolCalls(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		call, ok := req.(*mcp.CallToolRequest)
		if method != "tools/call" || !ok || call.Params == nil || s.deps.ActivitySvc == nil {
			return next(ctx, method, req)
		}

		start := time.Now()
		meta := &callAuditMeta{}
		s.prefillAuditFromTokenAndArgs(ctx, call.Params.Arguments, meta)

		res, callErr := next(context.WithValue(ctx, callAuditKey{}, meta), method, req)
		latencyMs := time.Since(start).Milliseconds()

		s.recordCall(ctx, call.Params, meta, latencyMs, res, callErr)
		return res, callErr
	}
}

func (s *Server) prefillAuditFromTokenAndArgs(ctx context.Context, rawArgs []byte, meta *callAuditMeta) {
	var argProbe struct {
		SessionID string `json:"session_id"`
		ProfileID string `json:"profile_id"`
	}
	if len(rawArgs) > 0 {
		_ = json.Unmarshal(rawArgs, &argProbe)
	}
	if argProbe.ProfileID != "" {
		meta.profileID = argProbe.ProfileID
	}

	if s.deps.WorkspaceSvc == nil {
		return
	}
	snap, err := s.deps.WorkspaceSvc.GetSnapshot(ctx)
	if err != nil || snap == nil {
		return
	}

	explicitSess := strings.TrimSpace(argProbe.SessionID)
	if explicitSess != "" {
		for _, sess := range snap.Sessions {
			if sess.ID == explicitSess || sess.SessionToken == explicitSess {
				meta.sessionID = sess.ID
				meta.projectID = sess.ProjectID
				meta.profileID = sess.ProfileID
				return
			}
		}
	}

	tok := strings.TrimSpace(s.connToken)
	if strings.HasPrefix(tok, domain.TokenPrefixProject) {
		for _, p := range snap.Projects {
			if p.ProjectToken == tok {
				meta.projectID = p.ID
				if p.LockedSessionID != "" {
					meta.sessionID = p.LockedSessionID
					for _, sess := range snap.Sessions {
						if sess.ID == p.LockedSessionID {
							meta.profileID = sess.ProfileID
							break
						}
					}
				}
				return
			}
		}
	} else if strings.HasPrefix(tok, domain.TokenPrefixSession) {
		for _, sess := range snap.Sessions {
			if sess.SessionToken == tok {
				meta.sessionID = sess.ID
				meta.projectID = sess.ProjectID
				meta.profileID = sess.ProfileID
				return
			}
		}
	}
}

func (s *Server) recordCall(ctx context.Context, params *mcp.CallToolParamsRaw, meta *callAuditMeta, latencyMs int64, res mcp.Result, callErr error) {
	respBytes := resultJSON(res, callErr)

	status := "success"
	errCode := meta.errorCode
	errMsg := meta.errorMsg

	if callErr != nil {
		status = "error"
		if errCode == "" {
			errCode = domain.CodeInternalError
		}
		if errMsg == "" {
			errMsg = callErr.Error()
		}
	} else if toolRes, ok := res.(*mcp.CallToolResult); ok && toolRes != nil && toolRes.IsError {
		status = "error"
		if errCode == "" || errMsg == "" {
			var parsed ResponseEnvelope
			if len(respBytes) > 0 && json.Unmarshal(respBytes, &parsed) == nil && parsed.Error != nil {
				if errCode == "" {
					errCode = parsed.Error.Code
				}
				if errMsg == "" {
					errMsg = parsed.Error.Message
				}
			}
		}
	} else if errCode != "" {
		status = "error"
	}

	_, _ = s.deps.ActivitySvc.Append(ctx, &domain.ActivityRecord{
		SessionID:    meta.sessionID,
		ProjectID:    meta.projectID,
		ProfileID:    meta.profileID,
		TokenPrefix:  domain.MaskTokenPrefix(s.connToken),
		ToolName:     params.Name,
		Status:       status,
		ErrorCode:    errCode,
		ErrorMessage: errMsg,
		LatencyMs:    latencyMs,
		Request:      capturePayload(params.Arguments),
		Response:     capturePayload(respBytes),
	})
}

func resultJSON(res mcp.Result, callErr error) []byte {
	if callErr != nil {
		b, _ := json.Marshal(map[string]string{"error": callErr.Error()})
		return b
	}
	r, ok := res.(*mcp.CallToolResult)
	if !ok || r == nil {
		return nil
	}
	if r.StructuredContent != nil {
		b, _ := json.Marshal(r.StructuredContent)
		return b
	}
	var texts []string
	for _, c := range r.Content {
		if t, ok := c.(*mcp.TextContent); ok {
			texts = append(texts, t.Text)
		}
	}
	if len(texts) == 0 {
		return nil
	}
	joined := strings.Join(texts, "\n")
	if json.Valid([]byte(joined)) {
		return []byte(joined)
	}
	b, _ := json.Marshal(joined)
	return b
}

func capturePayload(raw []byte) *domain.Payload {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil
	}
	body := redactJSON(raw)
	if len(body) <= maxPayloadBytes {
		return &domain.Payload{Body: body, Bytes: len(body)}
	}
	cut := maxPayloadBytes
	for cut > 0 && !utf8.RuneStart(body[cut]) {
		cut--
	}
	prefix, _ := marshalNoEscape(string(body[:cut]))
	return &domain.Payload{Body: prefix, Bytes: len(body), Truncated: true}
}

func redactJSON(raw []byte) []byte {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil || dec.More() {
		b, _ := marshalNoEscape(string(raw))
		return b
	}
	b, err := marshalNoEscape(redactValue(v))
	if err != nil {
		b, _ = marshalNoEscape(string(raw))
	}
	return b
}

func redactValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if isSensitiveKey(k) {
				t[k] = redacted
			} else {
				t[k] = redactValue(val)
			}
		}
	case []any:
		for i, val := range t {
			t[i] = redactValue(val)
		}
	}
	return v
}

func isSensitiveKey(key string) bool {
	k := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
	switch k {
	case "authorization", "cookie", "set_cookie", "api_key", "apikey", "private_key", "passwd", "pwd":
		return true
	}
	return strings.Contains(k, "password") || strings.Contains(k, "secret") ||
		k == "token" || strings.HasSuffix(k, "_token")
}

func marshalNoEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
