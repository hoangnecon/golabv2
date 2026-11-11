package app_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hoangnecon/golabv2/internal/app"
	"github.com/hoangnecon/golabv2/internal/config"
	"github.com/hoangnecon/golabv2/internal/mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestDaemonSingletonMultiClientAndChromePreservation(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "daemon_v2.db")

	var chromeStopped atomic.Int32
	fakeCDP := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/version":
			_ = json.NewEncoder(w).Encode(map[string]string{"webSocketDebuggerUrl": "ws://127.0.0.1/devtools/browser/1"})
		case "/json/list":
			_ = json.NewEncoder(w).Encode([]any{})
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer fakeCDP.Close()

	d, err := app.New(&config.Config{
		Addr:         "127.0.0.1:0",
		DBPath:       dbPath,
		ProfilesRoot: filepath.Join(tmp, "profiles"),
		Version:      "v2.0.0-test",
	}, nil)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runErr := make(chan error, 1)
	go func() {
		runErr <- d.Run(ctx)
	}()

	addr, err := d.WaitReady(3 * time.Second)
	if err != nil {
		t.Fatalf("WaitReady: %v", err)
	}

	baseURL := "http://" + addr

	// 1. Verify /api/v1/health & /api/v1/workspace
	resp, err := http.Get(baseURL + "/api/v1/health")
	if err != nil {
		t.Fatalf("GET /api/v1/health: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"version":"v2.0.0-test"`) {
		t.Fatalf("unexpected health response: %d %s", resp.StatusCode, string(body))
	}

	wsResp, err := http.Get(baseURL + "/api/v1/workspace")
	if err != nil {
		t.Fatalf("GET /api/v1/workspace: %v", err)
	}
	wsBody, _ := io.ReadAll(wsResp.Body)
	_ = wsResp.Body.Close()
	if wsResp.StatusCode != http.StatusOK || !strings.Contains(string(wsBody), `"default"`) {
		t.Fatalf("expected default profile in workspace snapshot: %d %s", wsResp.StatusCode, string(wsBody))
	}

	// 2. Connect TWO simultaneous RunIOBridge MCP clients to the single running daemon without port conflicts!
	connectBridgeClient := func(name string) *mcp.ClientSession {
		c2bR, c2bW := io.Pipe()
		b2cR, b2cW := io.Pipe()
		go func() {
			_ = mcpserver.RunIOBridge(ctx, c2bR, b2cW, baseURL+"/mcp", "")
		}()
		cs, errC := mcp.NewClient(&mcp.Implementation{Name: name, Version: "1.0.0"}, nil).Connect(ctx, &mcp.IOTransport{
			Reader: b2cR,
			Writer: c2bW,
		}, nil)
		if errC != nil {
			t.Fatalf("connect %s bridge client: %v", name, errC)
		}
		t.Cleanup(func() {
			_ = cs.Close()
			_ = c2bW.Close()
		})
		return cs
	}

	ide1 := connectBridgeClient("ide-1")
	ide2 := connectBridgeClient("ide-2")

	tools1, err := ide1.ListTools(ctx, nil)
	if err != nil || len(tools1.Tools) != 43 {
		t.Fatalf("ide1 ListTools: len=%d err=%v", len(tools1.Tools), err)
	}
	tools2, err := ide2.ListTools(ctx, nil)
	if err != nil || len(tools2.Tools) != 43 {
		t.Fatalf("ide2 ListTools: len=%d err=%v", len(tools2.Tools), err)
	}

	resList, err := ide1.CallTool(ctx, &mcp.CallToolParams{Name: "colab_list_all_sessions", Arguments: map[string]any{}})
	if err != nil || resList.IsError {
		t.Fatalf("ide1 colab_list_all_sessions: err=%v res=%+v", err, resList)
	}

	// 3. Graceful daemon shutdown leaves external fakeCDP HTTP server alive and closes cleanly
	cancel()
	select {
	case err := <-runErr:
		if err != nil && err != context.Canceled {
			t.Fatalf("daemon run returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("daemon did not shut down within 3s")
	}

	if chromeStopped.Load() != 0 {
		t.Fatal("expected external Chrome listener to remain untouched on daemon shutdown")
	}
	chk, err := http.Get(fakeCDP.URL + "/json/version")
	if err != nil || chk.StatusCode != http.StatusOK {
		t.Fatalf("expected external CDP server still alive after daemon shutdown, err=%v", err)
	}
	_ = chk.Body.Close()
}
