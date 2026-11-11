package process_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hoangnecon/golabv2/internal/domain"
	"github.com/hoangnecon/golabv2/internal/process"
)

func portFromURL(t *testing.T, raw string) int {
	t.Helper()
	parts := strings.Split(raw, ":")
	p, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		t.Fatalf("parse port from %q: %v", raw, err)
	}
	return p
}

func TestIsProfileRunning_DistinguishesCDPVsNonCDPPort(t *testing.T) {
	ctx := context.Background()
	mgr := process.NewChromeManager(process.ChromeOptions{})

	// 1. Server returning 404 on /json/version (like user's non-CDP port 9222) -> MUST return false!
	nonCDPSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer nonCDPSrv.Close()
	nonCDPPort := portFromURL(t, nonCDPSrv.URL)

	if mgr.IsProfileRunning(ctx, nonCDPPort) {
		t.Fatal("expected IsProfileRunning=false when port returns 404 without webSocketDebuggerUrl")
	}

	// 2. Server returning valid CDP /json/version JSON with webSocketDebuggerUrl -> MUST return true!
	cdpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json/version" {
			_ = json.NewEncoder(w).Encode(map[string]string{
				"Browser":              "Chrome/157.0.0.0",
				"webSocketDebuggerUrl": "ws://127.0.0.1:9223/devtools/browser/abc",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer cdpSrv.Close()
	cdpPort := portFromURL(t, cdpSrv.URL)

	if !mgr.IsProfileRunning(ctx, cdpPort) {
		t.Fatal("expected IsProfileRunning=true when /json/version returns valid webSocketDebuggerUrl")
	}
}

func TestListOpenFocusCloseTargets(t *testing.T) {
	ctx := context.Background()
	mgr := process.NewChromeManager(process.ChromeOptions{})

	activated := ""
	closed := ""
	targets := []domain.CDPTarget{
		{
			ID:                   "tab_1",
			Title:                "Demo Notebook",
			Type:                 "page",
			URL:                  "https://colab.research.google.com/drive/1YsugLzQOOw3CJUOiqhVJM0U7fP62x4Du",
			WebSocketDebuggerURL: "ws://127.0.0.1:9999/devtools/page/tab_1",
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/json/list":
			_ = json.NewEncoder(w).Encode(targets)
		case r.URL.Path == "/json/new":
			created := domain.CDPTarget{
				ID:                   "tab_2",
				Title:                "New Notebook",
				Type:                 "page",
				URL:                  r.URL.RawQuery,
				WebSocketDebuggerURL: "ws://127.0.0.1:9999/devtools/page/tab_2",
			}
			targets = append(targets, created)
			_ = json.NewEncoder(w).Encode(created)
		case strings.HasPrefix(r.URL.Path, "/json/activate/"):
			activated = strings.TrimPrefix(r.URL.Path, "/json/activate/")
			w.WriteHeader(http.StatusOK)
		case strings.HasPrefix(r.URL.Path, "/json/close/"):
			closed = strings.TrimPrefix(r.URL.Path, "/json/close/")
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	port := portFromURL(t, srv.URL)

	// Existing matching tab should focus without opening a duplicate.
	got, err := mgr.OpenOrFocusTab(ctx, port, "https://colab.research.google.com/drive/1YsugLzQOOw3CJUOiqhVJM0U7fP62x4Du", "drive/1YsugLzQOOw3CJUOiqhVJM0U7fP62x4Du")
	if err != nil || got.ID != "tab_1" || activated != "tab_1" {
		t.Fatalf("OpenOrFocusTab existing mismatch: got=%+v activated=%q err=%v", got, activated, err)
	}

	// Non-matching tab should open new tab via /json/new.
	gotNew, err := mgr.OpenOrFocusTab(ctx, port, "https://colab.research.google.com/drive/other999", "drive/other999")
	if err != nil || gotNew.ID != "tab_2" {
		t.Fatalf("OpenOrFocusTab new mismatch: got=%+v err=%v", gotNew, err)
	}

	if err := mgr.CloseTab(ctx, port, "tab_2"); err != nil || closed != "tab_2" {
		t.Fatalf("CloseTab mismatch: closed=%q err=%v", closed, err)
	}
}

func TestStopProfile_KillsExternalPersistedPIDAndShutdownAdapterPreservesChrome(t *testing.T) {
	ctx := context.Background()
	mgr := process.NewChromeManager(process.ChromeOptions{})

	// Reserve a free ephemeral port, close listener, then start an external python3 TCP listener (untracked by ChromeManager!).
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	pyScript := fmt.Sprintf(`
import http.server, socketserver, json
class H(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(json.dumps({"webSocketDebuggerUrl": "ws://127.0.0.1:%d/devtools/browser/1"}).encode())
    def log_message(self, *args): pass
socketserver.TCPServer.allow_reuse_address = True
with socketserver.TCPServer(("127.0.0.1", %d), H) as httpd:
    httpd.serve_forever()
`, port, port)

	cmd := exec.Command("python3", "-c", pyScript)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start dummy cdp server: %v", err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})

	// Wait until dummy CDP server is ready.
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if mgr.IsProfileRunning(ctx, port) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !mgr.IsProfileRunning(ctx, port) {
		t.Fatal("dummy external CDP server did not come up")
	}

	// ShutdownAdapter() MUST NOT kill running browser processes!
	if err := mgr.ShutdownAdapter(); err != nil {
		t.Fatalf("ShutdownAdapter: %v", err)
	}
	if !mgr.IsProfileRunning(ctx, port) {
		t.Fatal("ShutdownAdapter killed external Chrome process — violates Spec §3.1!")
	}

	// Explicit StopProfile MUST terminate external persisted ChromePID and free the port!
	prof := domain.Profile{
		ID:        "ext_prof",
		Name:      "External Chrome",
		CDPPort:   port,
		ChromePID: cmd.Process.Pid,
	}
	if err := mgr.StopProfile(ctx, prof); err != nil {
		t.Fatalf("StopProfile external PID: %v", err)
	}
	_, _ = cmd.Process.Wait()

	if mgr.IsProfileRunning(ctx, port) {
		t.Fatal("expected external Chrome process to be stopped after StopProfile")
	}
}
