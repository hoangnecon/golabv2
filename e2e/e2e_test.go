package e2e_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/hoangnecon/golabv2/internal/bridge"
	"github.com/hoangnecon/golabv2/internal/domain"
	"github.com/hoangnecon/golabv2/internal/process"
	"github.com/hoangnecon/golabv2/internal/store/sqlite"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"nhooyr.io/websocket"
)

func logStep(t *testing.T, format string, args ...any) {
	t.Helper()
	msg := fmt.Sprintf(format, args...)
	fmt.Printf("[E2E %s] %s\n", time.Now().Format("15:04:05"), msg)
	t.Log(msg)
}

func reserveFreePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

type mockAccountConfig struct {
	email       string
	displayName string
	tier        domain.ProfileTier
	balance     float64
	burnRate    float64
	permissions map[string]domain.PermissionLevel // notebookID -> permission
}

type mockCDPServer struct {
	port        int
	cfg         mockAccountConfig
	mu          sync.Mutex
	nextTabSeq  int
	tabs        []domain.CDPTarget
	closedTabs  []string
	saveCount   atomic.Int64
	httpServer  *http.Server
}

func startAccountMockCDP(t *testing.T, cfg mockAccountConfig) *mockCDPServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen mock CDP: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	m := &mockCDPServer{
		port: port,
		cfg:  cfg,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/json/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Browser":              "Chrome/134.0.0.0-Mock",
			"Protocol-Version":     "1.3",
			"webSocketDebuggerUrl": fmt.Sprintf("ws://127.0.0.1:%d/devtools/browser/mock", port),
		})
	})

	mux.HandleFunc("/json/list", func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		out := make([]domain.CDPTarget, len(m.tabs))
		copy(out, m.tabs)
		m.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})

	mux.HandleFunc("/json/new", func(w http.ResponseWriter, r *http.Request) {
		targetURL := strings.TrimSpace(r.URL.RawQuery)
		if targetURL == "" {
			targetURL = "https://colab.research.google.com"
		}
		m.mu.Lock()
		m.nextTabSeq++
		tabID := fmt.Sprintf("mock-tab-%d-%d", port, m.nextTabSeq)
		target := domain.CDPTarget{
			ID:                   tabID,
			Type:                 "page",
			Title:                "Colab Notebook",
			URL:                  targetURL,
			WebSocketDebuggerURL: fmt.Sprintf("ws://127.0.0.1:%d/devtools/page/%s", port, tabID),
		}
		m.tabs = append(m.tabs, target)
		m.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(target)
	})

	mux.HandleFunc("/json/activate/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Target activated"))
	})

	mux.HandleFunc("/json/close/", func(w http.ResponseWriter, r *http.Request) {
		tabID := strings.TrimPrefix(r.URL.Path, "/json/close/")
		m.mu.Lock()
		m.closedTabs = append(m.closedTabs, tabID)
		filtered := m.tabs[:0]
		for _, tb := range m.tabs {
			if tb.ID != tabID {
				filtered = append(filtered, tb)
			}
		}
		m.tabs = filtered
		m.mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Target is closing"))
	})

	mux.HandleFunc("/devtools/page/", func(w http.ResponseWriter, r *http.Request) {
		tabID := strings.TrimPrefix(r.URL.Path, "/devtools/page/")
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		ctx := r.Context()
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var req struct {
				ID     int64  `json:"id"`
				Method string `json:"method"`
				Params struct {
					Expression string `json:"expression"`
				} `json:"params"`
			}
			if err := json.Unmarshal(data, &req); err != nil {
				continue
			}

			outStr, evalErr := m.evaluate(tabID, req.Params.Expression)
			var resp map[string]any
			if evalErr != nil {
				resp = map[string]any{
					"id": req.ID,
					"result": map[string]any{
						"exceptionDetails": map[string]any{"text": evalErr.Error()},
					},
				}
			} else {
				resp = map[string]any{
					"id": req.ID,
					"result": map[string]any{
						"result": map[string]any{
							"type":  "string",
							"value": outStr,
						},
					},
				}
			}
			b, _ := json.Marshal(resp)
			_ = conn.Write(ctx, websocket.MessageText, b)
		}
	})

	srv := &http.Server{Handler: mux}
	m.httpServer = srv
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	return m
}

func (m *mockCDPServer) tabURL(tabID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, tb := range m.tabs {
		if tb.ID == tabID {
			return tb.URL
		}
	}
	return "https://colab.research.google.com/drive/1AlphaNotebookID00001"
}

func (m *mockCDPServer) permissionForURL(tabURL string) (string, domain.PermissionLevel) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for nbID, perm := range m.cfg.permissions {
		if strings.Contains(tabURL, nbID) {
			return nbID, perm
		}
	}
	return "drive/1AlphaNotebookID00001", domain.PermissionOwner
}

func (m *mockCDPServer) closedTabCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.closedTabs)
}

func (m *mockCDPServer) evaluate(tabID string, expr string) (string, error) {
	tabURL := m.tabURL(tabID)
	nbID, perm := m.permissionForURL(tabURL)
	driveFileID := strings.TrimPrefix(nbID, "drive/")

	switch {
	case strings.Contains(expr, "GOLAB_OP:probe_tab_state"):
		connState := domain.StateIdle
		connectState := "connected"
		kernelState := "idle"
		accel := "T4"
		uptime := int64(420)
		if perm == domain.PermissionViewOnly {
			connState = domain.StateRuntimeDisconnected
			connectState = "disconnected"
			kernelState = "connect"
			accel = ""
			uptime = 0
		}
		probe := domain.TabProbe{
			URL:             tabURL,
			Title:           driveFileID + ".ipynb",
			ConnectionState: connState,
			Permission:      perm,
			ConnectState:    connectState,
			KernelState:     kernelState,
			IsExecuting:     false,
			Identity: domain.NotebookIdentity{
				EmailAddress:         m.cfg.email,
				DisplayName:          m.cfg.displayName,
				IsAuthenticated:      true,
				DriveFileID:          driveFileID,
				Title:                driveFileID + ".ipynb",
				Role:                 string(perm),
				Permission:           perm,
				Editable:             perm.CanWrite(),
				Saveable:             perm.CanWrite(),
				IsSoleAuthored:       perm == domain.PermissionOwner,
				AcceleratorName:      accel,
				RuntimeUptimeSeconds: uptime,
				RuntimeShape:         "standard",
			},
		}
		b, _ := json.Marshal(probe)
		return string(b), nil

	case strings.Contains(expr, "GOLAB_OP:get_compute_units"):
		gpus := []string{"T4"}
		tpus := []string{}
		if m.cfg.tier != domain.TierFree {
			gpus = []string{"G4", "A100", "L4", "T4"}
			tpus = []string{"V6E1", "V5E1"}
		}
		info := domain.ComputeUnitsInfo{
			AssignmentsCount:      1,
			Balance:               m.cfg.balance,
			ConsumptionRateHourly: m.cfg.burnRate,
			FormattedBalance:      fmt.Sprintf("%.2f", m.cfg.balance),
			Tier:                  m.cfg.tier,
			EligibleGPUs:          gpus,
			IneligibleGPUs:        []string{"H100"},
			EligibleTPUs:          tpus,
			IneligibleTPUs:        []string{},
		}
		b, _ := json.Marshal(info)
		return string(b), nil

	case strings.Contains(expr, "GOLAB_OP:list_vm_sessions"):
		vms := []domain.ColabVMSession{
			{
				FileID:           driveFileID,
				Title:            driveFileID + ".ipynb",
				KernelID:         "k-alpha-01",
				SessionID:        "s-alpha-01",
				Accelerator:      "GPU",
				AcceleratorModel: "T4",
				Shape:            "standard",
				ManagedID:        "m-s-alpha01",
				ProxyURL:         "https://8080-m-s-alpha01.prod.r.colab.dev",
				TokenExpiresSec:  3600,
			},
		}
		b, _ := json.Marshal(vms)
		return string(b), nil

	case strings.Contains(expr, "GOLAB_OP:fetch_kernel_xhr"):
		switch {
		case strings.Contains(expr, `"/api/colab/resources"`):
			body := json.RawMessage(`{"memory":{"totalBytes":13605658624,"freeBytes":11200000000},"disks":[{"filesystem":{"label":"root","totalBytes":120946279055,"usedBytes":41231686041}}],"gpus":[{"name":"Tesla T4","memoryUsedBytes":2254438400,"memoryTotalBytes":16106127360,"gpuUtilization":0.18,"memoryUtilization":0.14,"everUsed":true}]}`)
			b, _ := json.Marshal(domain.FetchResponse{Status: 200, StatusText: "OK", OK: true, Body: body})
			return string(b), nil
		case strings.Contains(expr, `"/api/colab/build-info"`):
			body := json.RawMessage(`{"release_tag":"colab_20261111_0600_RC00"}`)
			b, _ := json.Marshal(domain.FetchResponse{Status: 200, StatusText: "OK", OK: true, Body: body})
			return string(b), nil
		default:
			body := json.RawMessage(`{"name":"content","path":"content","type":"directory","writable":true,"content":[]}`)
			b, _ := json.Marshal(domain.FetchResponse{Status: 200, StatusText: "OK", OK: true, Body: body})
			return string(b), nil
		}

	case strings.Contains(expr, "GOLAB_OP:check_drive_fast"):
		b, _ := json.Marshal(domain.DriveStatus{
			Mounted:       true,
			Status:        "mounted",
			MountPath:     "/content/drive",
			ButtonState:   "Unmount Drive",
			CredentialsOK: true,
			Message:       "Google Drive mounted.",
		})
		return string(b), nil

	case strings.Contains(expr, "GOLAB_OP:exec_python"):
		if strings.Contains(expr, "shutil.disk_usage") || strings.Contains(expr, "/proc/mounts") {
			return `{"used_sidecar":false,"stdout":"{\"has_fuse\": true, \"is_mount\": true, \"dir_exists\": true, \"non_empty\": true, \"total\": 115663142912, \"used\": 5884108800, \"free\": 109779034112}\n"}`, nil
		}
		return `{"used_sidecar":false,"stdout":"ok\n"}`, nil

	case strings.Contains(expr, "nb.saveNotebook"):
		m.saveCount.Add(1)
		return `{"success":true}`, nil

	case strings.Contains(expr, "nb.getCells()"):
		return `[{"index":0,"cell_id":"cell-alpha-1","is_running":false,"elapsed_seconds":0.4,"execution_count":"1","stdout":"ready"},{"index":1,"cell_id":"cell-alpha-2","is_running":false,"elapsed_seconds":1.2,"execution_count":"2","stdout":"epoch 1"}]`, nil

	case strings.Contains(expr, "GOLAB_OP:get_proxy_url"):
		return `{"port":8080,"url":"https://8080-m-s-alpha01.prod.r.colab.dev"}`, nil

	case strings.Contains(expr, "GOLAB_OP:run_code_cell"):
		return `{"success":true,"result":{"cell_id":"cell-alpha-1","index":0,"status":"running","is_running":true,"execution_count":1,"elapsed_seconds":0.2,"stdout":"epoch 1/100...","outputs":[],"message":"Cell is actively executing in the background."}}`, nil

	case strings.Contains(expr, "colabMcpToolsService"):
		switch {
		case strings.Contains(expr, `"getCellsTool"`):
			res := map[string]any{
				"success": true,
				"result": map[string]any{
					"cells": []map[string]any{
						{
							"id":        "cell-alpha-1",
							"cell_type": "code",
							"source":    []string{fmt.Sprintf("# Active account: %s (%s)\nimport torch", m.cfg.email, nbID)},
							"outputs":   []any{},
						},
						{
							"id":        "cell-alpha-2",
							"cell_type": "markdown",
							"source":    []string{"## Experiment Summary"},
							"outputs":   []any{},
						},
					},
				},
			}
			b, _ := json.Marshal(res)
			return string(b), nil
		case strings.Contains(expr, `"addCodeCellTool"`), strings.Contains(expr, `"addTextCellTool"`):
			return `{"success":true,"result":{"newCellId":"cell-added-99"}}`, nil
		default:
			return `{"success":true,"result":{"ok":true}}`, nil
		}
	}

	return "", errors.New("unhandled mock CDP expression: " + expr)
}

type uiDriver struct {
	t   *testing.T
	cdp *bridge.CDPClient
}

func launchHeadlessUIChrome(t *testing.T, ctx context.Context, startURL string) *uiDriver {
	t.Helper()
	chromeBin, err := process.FindChromePath()
	if err != nil {
		t.Fatalf("FindChromePath failed (Google Chrome required for Gate 6 E2E): %v", err)
	}

	uiCDPPort := reserveFreePort(t)
	userDataDir, err := os.MkdirTemp("", "golab-e2e-chrome-*")
	if err != nil {
		t.Fatalf("MkdirTemp chrome profile: %v", err)
	}
	args := []string{
		"--headless=new",
		"--disable-gpu",
		"--no-sandbox",
		"--disable-dev-shm-usage",
		"--disable-background-networking",
		"--disable-extensions",
		"--disable-sync",
		"--disable-translate",
		"--no-first-run",
		"--no-default-browser-check",
		"--use-mock-keychain",
		"--password-store=basic",
		"--window-size=1440,900",
		fmt.Sprintf("--remote-debugging-port=%d", uiCDPPort),
		"--remote-allow-origins=*",
		fmt.Sprintf("--user-data-dir=%s", userDataDir),
		startURL,
	}

	cmd := exec.Command(chromeBin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(userDataDir)
		t.Fatalf("launch headless Chrome: %v", err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
		for range 10 {
			if os.RemoveAll(userDataDir) == nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	})

	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		if process.IsCDPAlive(ctx, uiCDPPort) {
			break
		}
		time.Sleep(120 * time.Millisecond)
	}
	if !process.IsCDPAlive(ctx, uiCDPPort) {
		t.Fatalf("headless Chrome CDP did not become ready on 127.0.0.1:%d", uiCDPPort)
	}

	client := bridge.NewCDPClient(fmt.Sprintf("http://127.0.0.1:%d", uiCDPPort))
	t.Cleanup(func() { _ = client.Close() })

	var pageTarget *bridge.TargetInfo
	targetDeadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(targetDeadline) {
		targets, err := client.ListTargets(ctx)
		if err == nil {
			for i := range targets {
				if targets[i].Type == "page" && strings.HasPrefix(targets[i].URL, "http://127.0.0.1:") {
					pageTarget = &targets[i]
					break
				}
			}
		}
		if pageTarget != nil && pageTarget.WebSocketDebuggerURL != "" {
			break
		}
		time.Sleep(120 * time.Millisecond)
	}
	if pageTarget == nil {
		t.Fatalf("could not find UI page target in headless Chrome")
	}

	if err := client.ConnectToTarget(ctx, pageTarget.WebSocketDebuggerURL); err != nil {
		t.Fatalf("ConnectToTarget UI page: %v", err)
	}

	return &uiDriver{t: t, cdp: client}
}

func (u *uiDriver) evalString(ctx context.Context, js string) string {
	u.t.Helper()
	callCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	raw, err := u.cdp.Evaluate(callCtx, js)
	if err != nil {
		u.t.Fatalf("UI JS evaluate error: %v\nscript:\n%s", err, js)
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return string(raw)
	}
	return s
}

func (u *uiDriver) waitFor(ctx context.Context, desc string, timeout time.Duration, predicateJS string) {
	u.t.Helper()
	deadline := time.Now().Add(timeout)
	var lastVal string
	for time.Now().Before(deadline) {
		callCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		raw, err := u.cdp.Evaluate(callCtx, predicateJS)
		cancel()
		if err == nil {
			lastVal = strings.TrimSpace(string(raw))
			if lastVal == "true" {
				return
			}
		} else {
			lastVal = err.Error()
		}
		time.Sleep(100 * time.Millisecond)
	}
	snapCtx, cancelSnap := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancelSnap()
	var bodySnap, hashSnap string
	if rawB, errB := u.cdp.Evaluate(snapCtx, `String(document.body?.innerText || '').slice(0, 1200)`); errB == nil {
		_ = json.Unmarshal(rawB, &bodySnap)
	}
	if rawH, errH := u.cdp.Evaluate(snapCtx, `String(window.location.hash || '')`); errH == nil {
		_ = json.Unmarshal(rawH, &hashSnap)
	}
	u.t.Fatalf("timed out waiting for %s (last=%s, hash=%s)\nvisible DOM text:\n%s", desc, lastVal, hashSnap, bodySnap)
}

func (u *uiDriver) navigateHash(ctx context.Context, hash string) {
	u.t.Helper()
	_ = u.evalString(ctx, fmt.Sprintf(`(() => { window.location.hash = %q; return window.location.hash; })()`, hash))
}

func (u *uiDriver) clickButtonByText(ctx context.Context, scopeSelector string, exactText string) {
	u.t.Helper()
	u.waitFor(ctx, fmt.Sprintf("button %q in %s", exactText, scopeSelector), 8*time.Second, fmt.Sprintf(`(() => {
		const root = %q ? document.querySelector(%q) : document;
		if (!root) return false;
		const btns = Array.from(root.querySelectorAll('button, a[role="button"]'));
		const target = btns.find(b => !b.disabled && b.offsetParent !== null && b.textContent.trim() === %q);
		if (!target) return false;
		target.click();
		return true;
	})()`, scopeSelector, scopeSelector, exactText))
}

func (u *uiDriver) setReactInputByLabel(ctx context.Context, scopeSelector string, labelSubstr string, value string) {
	u.t.Helper()
	u.waitFor(ctx, fmt.Sprintf("input labeled %q", labelSubstr), 8*time.Second, fmt.Sprintf(`(() => {
		const root = %q ? document.querySelector(%q) : document;
		if (!root) return false;
		const labels = Array.from(root.querySelectorAll('label'));
		const lbl = labels.find(l => l.textContent.includes(%q));
		if (!lbl) return false;
		const input = lbl.htmlFor ? document.getElementById(lbl.htmlFor) : lbl.querySelector('input');
		if (!input) return false;
		const setter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value').set;
		setter.call(input, %q);
		input.dispatchEvent(new Event('input', { bubbles: true }));
		input.dispatchEvent(new Event('change', { bubbles: true }));
		return true;
	})()`, scopeSelector, scopeSelector, labelSubstr, value))
}

func (u *uiDriver) setReactSelectByLabel(ctx context.Context, scopeSelector string, labelSubstr string, optionValue string) {
	u.t.Helper()
	u.waitFor(ctx, fmt.Sprintf("select labeled %q -> %q", labelSubstr, optionValue), 8*time.Second, fmt.Sprintf(`(() => {
		const root = %q ? document.querySelector(%q) : document;
		if (!root) return false;
		const labels = Array.from(root.querySelectorAll('label'));
		const lbl = labels.find(l => l.textContent.includes(%q));
		if (!lbl) return false;
		const sel = lbl.htmlFor ? document.getElementById(lbl.htmlFor) : lbl.querySelector('select');
		if (!sel) return false;
		const setter = Object.getOwnPropertyDescriptor(window.HTMLSelectElement.prototype, 'value').set;
		setter.call(sel, %q);
		sel.dispatchEvent(new Event('input', { bubbles: true }));
		sel.dispatchEvent(new Event('change', { bubbles: true }));
		return sel.value === %q;
	})()`, scopeSelector, scopeSelector, labelSubstr, optionValue, optionValue))
}

func (u *uiDriver) assertPureMonochromeDOM(ctx context.Context, routeLabel string) int {
	u.t.Helper()
	raw := u.evalString(ctx, `(() => {
		const props = [
			'color',
			'backgroundColor',
			'borderTopColor',
			'borderRightColor',
			'borderBottomColor',
			'borderLeftColor',
			'outlineColor',
			'fill',
			'stroke',
		];
		const violations = [];
		let checked = 0;
		const nodes = document.querySelectorAll('*');
		for (const el of nodes) {
			const rect = el.getBoundingClientRect();
			if (rect.width === 0 && rect.height === 0) continue;
			const cs = window.getComputedStyle(el);
			if (cs.display === 'none' || cs.visibility === 'hidden') continue;
			checked++;
			for (const p of props) {
				const val = String(cs[p] || '').trim();
				if (!val || val === 'none' || val === 'transparent' || val === 'currentcolor' || val.startsWith('url(')) {
					continue;
				}
				const m = val.match(/^rgba?\(\s*([0-9.]+)\s*,\s*([0-9.]+)\s*,\s*([0-9.]+)(?:\s*,\s*([0-9.]+))?\s*\)$/i);
				if (m) {
					const r = Number(m[1]);
					const g = Number(m[2]);
					const b = Number(m[3]);
					const a = m[4] !== undefined ? Number(m[4]) : 1;
					if (a === 0) continue;
					if (r !== g || g !== b) {
						violations.push({
							tag: el.tagName.toLowerCase(),
							className: String(el.className || '').slice(0, 80),
							prop: p,
							value: val,
						});
						if (violations.length >= 10) break;
					}
					continue;
				}
				// Catch modern color(srgb r g b) if emitted by Blink
				const srgb = val.match(/^color\(srgb\s+([0-9.]+)\s+([0-9.]+)\s+([0-9.]+)/i);
				if (srgb) {
					const r = Number(srgb[1]);
					const g = Number(srgb[2]);
					const b = Number(srgb[3]);
					if (Math.abs(r - g) > 1e-4 || Math.abs(g - b) > 1e-4) {
						violations.push({
							tag: el.tagName.toLowerCase(),
							className: String(el.className || '').slice(0, 80),
							prop: p,
							value: val,
						});
						if (violations.length >= 10) break;
					}
				}
			}
			if (violations.length >= 10) break;
		}
		return JSON.stringify({ checked, violations });
	})()`)

	var res struct {
		Checked    int `json:"checked"`
		Violations []struct {
			Tag       string `json:"tag"`
			ClassName string `json:"className"`
			Prop      string `json:"prop"`
			Value     string `json:"value"`
		} `json:"violations"`
	}
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		u.t.Fatalf("parse monochrome DOM check on %s: %v (raw=%s)", routeLabel, err, raw)
	}
	if res.Checked == 0 {
		u.t.Fatalf("monochrome DOM check on %s inspected 0 visible elements", routeLabel)
	}
	if len(res.Violations) > 0 {
		u.t.Fatalf("Gate 6 R===G===B monochrome violation on %s (%d checked): %+v", routeLabel, res.Checked, res.Violations)
	}
	return res.Checked
}

func decodeEnvelope(t *testing.T, res *mcp.CallToolResult) (string, string) {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatalf("empty MCP CallToolResult content")
	}
	txt, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", res.Content[0])
	}
	var env struct {
		Status string `json:"status"`
		Error  *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(txt.Text), &env); err != nil {
		t.Fatalf("unmarshal MCP envelope (%s): %v", txt.Text, err)
	}
	code := ""
	if env.Error != nil {
		code = env.Error.Code
	}
	return env.Status, code
}

func TestEndToEndControlPlaneAndMonochromeUI(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "golab-e2e.db")
	binPath := filepath.Join(tmpDir, "golab")

	logStep(t, "Phase 1/6: Starting 2-Account Mock Chrome CDP Servers and compiling real golab binary -> %s", binPath)

	mockAcct1 := startAccountMockCDP(t, mockAccountConfig{
		email:       "hoangvipper1105@gmail.com",
		displayName: "Hoang Owner",
		tier:        domain.TierPro,
		balance:     15.25,
		burnRate:    0.08,
		permissions: map[string]domain.PermissionLevel{
			"drive/1AlphaNotebookID00001": domain.PermissionOwner,
			"drive/1BetaNotebookID000002": domain.PermissionOwner,
		},
	})
	mockAcct2 := startAccountMockCDP(t, mockAccountConfig{
		email:       "editor2@gmail.com",
		displayName: "Colab Editor Two",
		tier:        domain.TierFree,
		balance:     0.00,
		burnRate:    0.00,
		permissions: map[string]domain.PermissionLevel{
			"drive/1AlphaNotebookID00001": domain.PermissionEditor,
			"drive/1BetaNotebookID000002": domain.PermissionViewOnly,
		},
	})

	// Seed isolated SQLite DB with deterministic CDP ports for Account 1 (default) and Account 2 (acct-editor-2)
	seedStore, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("open seed sqlite: %v", err)
	}
	if err := seedStore.CreateProfile(ctx, &domain.Profile{
		ID:           "default",
		Name:         "Primary Pro Account",
		AccountEmail: "hoangvipper1105@gmail.com",
		DisplayName:  "Hoang Owner",
		Tier:         domain.TierPro,
		UserDataDir:  filepath.Join(tmpDir, "profiles", "default"),
		CDPPort:      mockAcct1.port,
	}); err != nil {
		t.Fatalf("seed default profile: %v", err)
	}
	if err := seedStore.CreateProfile(ctx, &domain.Profile{
		ID:           "acct-editor-2",
		Name:         "Shared Editor Account",
		AccountEmail: "editor2@gmail.com",
		DisplayName:  "Colab Editor Two",
		Tier:         domain.TierFree,
		UserDataDir:  filepath.Join(tmpDir, "profiles", "acct-editor-2"),
		CDPPort:      mockAcct2.port,
	}); err != nil {
		t.Fatalf("seed second profile: %v", err)
	}
	_ = seedStore.Close()

	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	buildCmd := exec.CommandContext(ctx, "go", "build", "-o", binPath, "./cmd/server")
	buildCmd.Dir = repoRoot
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("compile golab binary failed: %v\n%s", err, string(out))
	}

	daemonPort := reserveFreePort(t)
	daemonAddr := fmt.Sprintf("127.0.0.1:%d", daemonPort)
	daemonURL := "http://" + daemonAddr

	daemonCmd := exec.CommandContext(ctx, binPath, "daemon", "--addr", daemonAddr, "--db", dbPath)
	daemonCmd.Dir = tmpDir
	if err := daemonCmd.Start(); err != nil {
		t.Fatalf("start golab daemon: %v", err)
	}
	t.Cleanup(func() {
		if daemonCmd.Process != nil {
			_ = daemonCmd.Process.Kill()
			_, _ = daemonCmd.Process.Wait()
		}
	})

	healthReady := false
	for i := 0; i < 60; i++ {
		resp, err := http.Get(daemonURL + "/api/v1/health")
		if err == nil && resp.StatusCode == http.StatusOK {
			_ = resp.Body.Close()
			healthReady = true
			break
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !healthReady {
		t.Fatalf("golab daemon on %s did not pass /api/v1/health", daemonAddr)
	}

	logStep(t, "Phase 2/6: Launching real Headless Chrome against %s/#/projects and creating Project Alpha & Project Beta", daemonURL)
	ui := launchHeadlessUIChrome(t, ctx, daemonURL+"/#/projects")
	ui.waitFor(ctx, "initial Projects page & SSE connected", 10*time.Second, `Boolean(document.body && document.body.innerText.includes('Projects'))`)

	// Create Project Alpha via native <dialog> UI and lock Account 1 immediately
	ui.clickButtonByText(ctx, "", "New project")
	ui.waitFor(ctx, "New project dialog open", 5*time.Second, `Boolean(document.querySelector('dialog[open]'))`)
	ui.setReactInputByLabel(ctx, "dialog[open]", "Project name", "Project Alpha")
	ui.setReactInputByLabel(ctx, "dialog[open]", "Colab notebook URL", "https://colab.research.google.com/drive/1AlphaNotebookID00001")
	ui.setReactSelectByLabel(ctx, "dialog[open]", "Owner account profile", "default")
	_ = ui.evalString(ctx, `(() => {
		const cb = document.querySelector('dialog[open] input[type="checkbox"]');
		if (cb && !cb.checked) cb.click();
		return 'ok';
	})()`)
	ui.clickButtonByText(ctx, "dialog[open]", "Create project")

	uuidRouteRe := regexp.MustCompile(`^#/projects/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	ui.waitFor(ctx, "redirect to canonical UUID route for Project Alpha", 10*time.Second, `window.location.hash.startsWith('#/projects/') && window.location.hash.length > '#/projects/'.length + 20`)
	alphaHash := ui.evalString(ctx, `window.location.hash`)
	if !uuidRouteRe.MatchString(alphaHash) {
		t.Fatalf("expected canonical UUID route #/projects/<uuid>, got %q", alphaHash)
	}
	alphaID := strings.TrimPrefix(alphaHash, "#/projects/")

	// Attach Account 2 (acct-editor-2) to Project Alpha via AddSessionDialog
	ui.clickButtonByText(ctx, "", "Attach account")
	ui.waitFor(ctx, "Attach account dialog open", 5*time.Second, `Boolean(document.querySelector('dialog[open]'))`)
	ui.setReactSelectByLabel(ctx, "dialog[open]", "Account profile", "acct-editor-2")
	ui.clickButtonByText(ctx, "dialog[open]", "Attach account")
	ui.waitFor(ctx, "Account 2 listed in Project Alpha SessionMatrixTable", 8*time.Second, `document.body.innerText.includes('Shared Editor Account')`)

	// Verify read-only TelemetryStrip populated on Project Alpha (15.25 CU, Tesla T4, Mounted FUSE Drive)
	ui.waitFor(ctx, "TelemetryStrip shows 15.25 CU and Tesla T4", 10*time.Second, `document.body.innerText.includes('15.25 CU') && document.body.innerText.includes('Tesla T4')`)

	// Navigate back to #/projects and create Project Beta locked to Account 2 (which only has view_only on Beta!)
	ui.navigateHash(ctx, "#/projects")
	ui.waitFor(ctx, "Projects index shows Project Alpha card", 5*time.Second, `document.body.innerText.includes('Project Alpha')`)
	ui.clickButtonByText(ctx, "", "New project")
	ui.waitFor(ctx, "New project dialog for Beta", 5*time.Second, `Boolean(document.querySelector('dialog[open]'))`)
	ui.setReactInputByLabel(ctx, "dialog[open]", "Project name", "Project Beta")
	ui.setReactInputByLabel(ctx, "dialog[open]", "Colab notebook URL", "https://colab.research.google.com/drive/1BetaNotebookID000002")
	ui.setReactSelectByLabel(ctx, "dialog[open]", "Owner account profile", "acct-editor-2")
	_ = ui.evalString(ctx, `(() => {
		const cb = document.querySelector('dialog[open] input[type="checkbox"]');
		if (cb && !cb.checked) cb.click();
		return 'ok';
	})()`)
	ui.clickButtonByText(ctx, "dialog[open]", "Create project")

	ui.waitFor(ctx, "Project Beta UUID route & VIEW ONLY monochrome banner", 10*time.Second,
		`window.location.hash !== `+fmt.Sprintf("%q", alphaHash)+` && window.location.hash.startsWith('#/projects/') && document.body.innerText.includes('VIEW ONLY') && document.body.innerText.includes('GOLAB_PERMISSION_READ_ONLY')`)
	betaHash := ui.evalString(ctx, `window.location.hash`)
	if !uuidRouteRe.MatchString(betaHash) {
		t.Fatalf("expected canonical UUID route for Project Beta, got %q", betaHash)
	}
	betaID := strings.TrimPrefix(betaHash, "#/projects/")

	logStep(t, "Phase 3/6: Testing Per-Project Exclusive Lock Switch (Account 1 -> Account 2 on Project Alpha) + SaveNotebook flush")
	ui.navigateHash(ctx, "#/projects/"+alphaID)
	ui.waitFor(ctx, "Project Alpha page loaded", 8*time.Second, `document.body.innerText.includes('Project Alpha') && document.body.innerText.includes('Switch lock')`)

	ui.clickButtonByText(ctx, "", "Switch lock")
	ui.waitFor(ctx, "Confirm Switch exclusive project lock dialog", 5*time.Second, `Boolean(document.querySelector('dialog[open]')) && document.querySelector('dialog[open]').innerText.includes('Switch exclusive project lock')`)
	ui.clickButtonByText(ctx, "dialog[open]", "Switch lock")

	ui.waitFor(ctx, "Project Alpha lock switched to editor2@gmail.com", 10*time.Second, `document.body.innerText.toUpperCase().includes('LOCKED: EDITOR2@GMAIL.COM')`)
	if mockAcct1.saveCount.Load() < 1 {
		t.Fatalf("expected Switch Lock to flush SaveNotebook on Account 1 before detaching, got saveCount=%d", mockAcct1.saveCount.Load())
	}
	if mockAcct1.closedTabCount() < 1 {
		t.Fatalf("expected Switch Lock to close Account 1's Colab tab to prevent merge conflicts, got closedTabs=%d", mockAcct1.closedTabCount())
	}

	// Fetch workspace snapshot to read generated glp_ Project Tokens for Alpha and Beta
	wsResp, err := http.Get(daemonURL + "/api/v1/workspace")
	if err != nil {
		t.Fatalf("GET /api/v1/workspace: %v", err)
	}
	var wsSnap domain.WorkspaceSnapshot
	if err := json.NewDecoder(wsResp.Body).Decode(&wsSnap); err != nil {
		_ = wsResp.Body.Close()
		t.Fatalf("decode workspace snapshot: %v", err)
	}
	_ = wsResp.Body.Close()

	var alphaToken, betaToken string
	for _, p := range wsSnap.Projects {
		if p.ID == alphaID {
			alphaToken = p.ProjectToken
		}
		if p.ID == betaID {
			betaToken = p.ProjectToken
		}
	}
	if !strings.HasPrefix(alphaToken, "glp_") || !strings.HasPrefix(betaToken, "glp_") {
		t.Fatalf("expected glp_ tokens for Alpha (%q) and Beta (%q)", alphaToken, betaToken)
	}

	logStep(t, "Phase 4/6: Connecting 2 concurrent HTTP MCP clients + 1 stdio subprocess (`golab mcp --token`) against singleton daemon")
	httpClientAlpha, err := mcp.NewClient(&mcp.Implementation{Name: "e2e-alpha-http", Version: "1.0.0"}, nil).Connect(
		ctx,
		&mcp.StreamableClientTransport{Endpoint: daemonURL + "/mcp?token=" + alphaToken, DisableStandaloneSSE: true},
		nil,
	)
	if err != nil {
		t.Fatalf("connect HTTP MCP Alpha: %v", err)
	}
	defer httpClientAlpha.Close()

	httpClientBeta, err := mcp.NewClient(&mcp.Implementation{Name: "e2e-beta-http", Version: "1.0.0"}, nil).Connect(
		ctx,
		&mcp.StreamableClientTransport{Endpoint: daemonURL + "/mcp?token=" + betaToken, DisableStandaloneSSE: true},
		nil,
	)
	if err != nil {
		t.Fatalf("connect HTTP MCP Beta: %v", err)
	}
	defer httpClientBeta.Close()

	stdioCmd := exec.CommandContext(ctx, binPath, "mcp", "--addr", daemonAddr, "--db", dbPath, "--token", alphaToken)
	stdioClientAlpha, err := mcp.NewClient(&mcp.Implementation{Name: "e2e-alpha-stdio", Version: "1.0.0"}, nil).Connect(
		ctx,
		&mcp.CommandTransport{Command: stdioCmd},
		nil,
	)
	if err != nil {
		t.Fatalf("connect stdio MCP subprocess (`golab mcp`): %v", err)
	}
	defer stdioClientAlpha.Close()

	toolsRes, err := stdioClientAlpha.ListTools(ctx, nil)
	if err != nil || len(toolsRes.Tools) != 43 {
		t.Fatalf("expected 43 canonical MCP tools over stdio bridge, got len=%d err=%v", len(toolsRes.Tools), err)
	}

	// Execute concurrent tool calls on Alpha (HTTP + stdio) and Beta (read allowed, write blocked with GOLAB_PERMISSION_READ_ONLY!)
	alphaOutlineRes, err := httpClientAlpha.CallTool(ctx, &mcp.CallToolParams{Name: "colab_get_notebook_outline", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("Alpha colab_get_notebook_outline failed: %v", err)
	}
	if st, code := decodeEnvelope(t, alphaOutlineRes); st != "success" {
		t.Fatalf("expected Alpha outline success, got status=%s code=%s", st, code)
	}

	stdioCCURes, err := stdioClientAlpha.CallTool(ctx, &mcp.CallToolParams{Name: "colab_get_compute_units", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("stdio colab_get_compute_units failed: %v", err)
	}
	if st, code := decodeEnvelope(t, stdioCCURes); st != "success" {
		t.Fatalf("expected stdio compute units success, got status=%s code=%s", st, code)
	}

	betaReadRes, err := httpClientBeta.CallTool(ctx, &mcp.CallToolParams{Name: "colab_get_notebook_outline", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("Beta read colab_get_notebook_outline failed: %v", err)
	}
	if st, code := decodeEnvelope(t, betaReadRes); st != "success" {
		t.Fatalf("expected Beta view_only read to succeed, got status=%s code=%s", st, code)
	}

	betaWriteRes, err := httpClientBeta.CallTool(ctx, &mcp.CallToolParams{
		Name: "colab_add_cell",
		Arguments: map[string]any{
			"index":     0,
			"cell_type": "code",
			"content":   "print('blocked on view_only')",
		},
	})
	if err != nil {
		t.Fatalf("Beta colab_add_cell transport error: %v", err)
	}
	if st, code := decodeEnvelope(t, betaWriteRes); st != "error" || code != domain.CodePermissionReadOnly {
		t.Fatalf("expected Beta view_only write to fail with %s, got status=%s code=%s", domain.CodePermissionReadOnly, st, code)
	}

	logStep(t, "Phase 5/6: Verifying zero-reload SSE audit log streaming & Right Split JSON Payload Inspector")
	// Without reloading Project Alpha, live SSE rows for colab_get_notebook_outline & colab_get_compute_units must already be visible!
	ui.waitFor(ctx, "live SSE rows on Project Alpha without reload", 8*time.Second,
		`document.body.innerText.includes('colab_get_notebook_outline') && document.body.innerText.includes('colab_get_compute_units')`)

	// Navigate to #/logs, verify live rows + GOLAB_PERMISSION_READ_ONLY, and open Right Split JSON Payload Inspector
	ui.navigateHash(ctx, "#/logs")
	ui.waitFor(ctx, "Audit Logs page shows colab_add_cell GOLAB_PERMISSION_READ_ONLY", 8*time.Second,
		`document.body.innerText.includes('colab_add_cell') && document.body.innerText.includes('GOLAB_PERMISSION_READ_ONLY')`)

	_ = ui.evalString(ctx, `(() => {
		const rows = Array.from(document.querySelectorAll('tbody tr'));
		const row = rows.find(r => r.innerText.includes('colab_add_cell'));
		if (row) row.click();
		return Boolean(row);
	})()`)
	ui.waitFor(ctx, "Right Split Payload Inspector renders Request JSON & Response JSON", 8*time.Second,
		`document.body.innerText.includes('Request JSON') && document.body.innerText.includes('Response JSON') && document.body.innerText.includes('blocked on view_only')`)

	logStep(t, "Phase 6/6: Running DOM-wide getComputedStyle R===G===B monochrome walk across all routes in Light & Dark themes")
	routesToAudit := []struct {
		hash       string
		expectText string
	}{
		{hash: "#/projects", expectText: "Project Alpha"},
		{hash: "#/projects/" + alphaID, expectText: "Account sessions & exclusive lock"},
		{hash: "#/projects/" + betaID, expectText: "VIEW ONLY"},
		{hash: "#/accounts", expectText: "Primary Pro Account"},
		{hash: "#/accounts/default", expectText: "15.25 CU"},
		{hash: "#/logs", expectText: "colab_add_cell"},
	}

	totalElementsChecked := 0
	for _, themePass := range []string{"initial", "toggled"} {
		if themePass == "toggled" {
			_ = ui.evalString(ctx, `(() => {
				const btn = document.querySelector('button[aria-label*="monochrome theme"]');
				if (!btn) throw new Error('theme toggle button not found');
				btn.click();
				return document.documentElement.dataset.theme || '';
			})()`)
		}
		for _, rt := range routesToAudit {
			ui.navigateHash(ctx, rt.hash)
			ui.waitFor(ctx, fmt.Sprintf("route %s (%s)", rt.hash, themePass), 8*time.Second,
				fmt.Sprintf(`document.body.innerText.includes(%q)`, rt.expectText))
			totalElementsChecked += ui.assertPureMonochromeDOM(ctx, fmt.Sprintf("%s [%s]", rt.hash, themePass))
		}
	}

	// Verify zero runtime-duplicating buttons exist on ProjectPage (Connect, Run all, Save)
	ui.navigateHash(ctx, "#/projects/"+alphaID)
	ui.waitFor(ctx, "Project Alpha final check", 5*time.Second, `document.body.innerText.includes('Live locked session observability (read-only)')`)
	forbiddenUIButtons := ui.evalString(ctx, `(() => {
		const labels = Array.from(document.querySelectorAll('button')).map(b => b.textContent.trim().toLowerCase());
		const banned = ['run all', 'connect runtime', 'save notebook', 'change accelerator'];
		return JSON.stringify(labels.filter(l => banned.includes(l)));
	})()`)
	if forbiddenUIButtons != "[]" {
		t.Fatalf("found forbidden Colab-duplicating runtime buttons in GoLab UI: %s", forbiddenUIButtons)
	}

	logStep(t, "Completed E2E verification: 2 projects, 2 accounts, 3 concurrent MCP clients (HTTP+stdio), %d visible DOM elements verified R===G===B", totalElementsChecked)
}
