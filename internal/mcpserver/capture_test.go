package mcpserver

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCapturePayloadRedactsCredentialFields(t *testing.T) {
	p := capturePayload([]byte(`{"path":"a<b>.py","API-Key":"k","headers":{"Authorization":"Bearer x","accept":"*/*"},
		"items":[{"refresh_token":"r","n":12345678901234567890}],"token_count":3,"db_password":"p"}`))
	if p == nil || p.Truncated {
		t.Fatalf("unexpected payload: %+v", p)
	}
	var got map[string]any
	if err := json.Unmarshal(p.Body, &got); err != nil {
		t.Fatalf("body is not JSON: %v (%s)", err, p.Body)
	}
	if got["API-Key"] != redacted || got["db_password"] != redacted {
		t.Errorf("top-level secrets not redacted: %s", p.Body)
	}
	headers := got["headers"].(map[string]any)
	if headers["Authorization"] != redacted || headers["accept"] != "*/*" {
		t.Errorf("nested headers wrong: %s", p.Body)
	}
	item := got["items"].([]any)[0].(map[string]any)
	if item["refresh_token"] != redacted {
		t.Errorf("secret inside array not redacted: %s", p.Body)
	}
	if got["token_count"] != float64(3) || got["path"] != "a<b>.py" {
		t.Errorf("non-secret fields changed: %s", p.Body)
	}
	if !strings.Contains(string(p.Body), "12345678901234567890") || !strings.Contains(string(p.Body), "a<b>.py") {
		t.Errorf("numbers or HTML characters were rewritten: %s", p.Body)
	}
}

func TestCapturePayloadTruncatesLargeBodies(t *testing.T) {
	big := `{"content":"` + strings.Repeat("é", maxPayloadBytes) + `"}`
	p := capturePayload([]byte(big))
	if p == nil || !p.Truncated || p.Bytes != len(big) {
		t.Fatalf("expected truncation of %d bytes, got %+v", len(big), p)
	}
	var prefix string
	if err := json.Unmarshal(p.Body, &prefix); err != nil {
		t.Fatalf("truncated body must be a JSON string: %v", err)
	}
	if len(prefix) > maxPayloadBytes || !strings.HasPrefix(big, prefix) {
		t.Fatalf("prefix wrong: len=%d", len(prefix))
	}
}

func TestCapturePayloadNonJSONAndEmpty(t *testing.T) {
	if capturePayload(nil) != nil || capturePayload([]byte("  ")) != nil {
		t.Fatal("empty input must not produce a payload")
	}
	p := capturePayload([]byte("not json"))
	if p == nil || string(p.Body) != `"not json"` {
		t.Fatalf("non-JSON input must be stored as a JSON string, got %+v", p)
	}
}
