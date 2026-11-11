package golab_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/hoangnecon/golabv2/internal/bridge"
	"github.com/hoangnecon/golabv2/internal/domain"
	"github.com/hoangnecon/golabv2/internal/driver/golab"
	"nhooyr.io/websocket"
)

func TestGolabDriverInterfaceImplementation(t *testing.T) {
	var _ domain.RuntimeDriver = (*golab.Driver)(nil)
	var _ domain.DriverFactory = (*golab.Factory)(nil)
}

type mockBrowser struct {
	mu      sync.Mutex
	targets []domain.CDPTarget
}

func (m *mockBrowser) LaunchProfile(ctx context.Context, profile domain.Profile, initialURL string) (int, int, error) {
	return 1001, profile.CDPPort, nil
}
func (m *mockBrowser) StopProfile(ctx context.Context, profile domain.Profile) error { return nil }
func (m *mockBrowser) IsProfileRunning(ctx context.Context, cdpPort int) bool        { return true }
func (m *mockBrowser) ListTargets(ctx context.Context, cdpPort int) ([]domain.CDPTarget, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]domain.CDPTarget, len(m.targets))
	copy(out, m.targets)
	return out, nil
}
func (m *mockBrowser) OpenOrFocusTab(ctx context.Context, cdpPort int, targetURL string, matchSubstr string) (domain.CDPTarget, error) {
	return domain.CDPTarget{ID: "tab-1", URL: targetURL}, nil
}
func (m *mockBrowser) FocusTab(ctx context.Context, cdpPort int, tabID string) error { return nil }
func (m *mockBrowser) CloseTab(ctx context.Context, cdpPort int, tabID string) error { return nil }
func (m *mockBrowser) ShutdownAdapter() error                                        { return nil }

func startMockCDPServer(t *testing.T, evalHandler func(expr string) (string, error)) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			outStr, evalErr := evalHandler(req.Params.Expression)
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
	}))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/devtools/page/tab1"
}

func newTestDriver(t *testing.T, browser domain.BrowserManager, evalHandler func(expr string) (string, error)) *golab.Driver {
	t.Helper()
	wsURL := startMockCDPServer(t, evalHandler)
	cdp := bridge.NewTargetCDPClient(9222, bridge.TargetInfo{
		ID:                   "tab1",
		Type:                 "page",
		WebSocketDebuggerURL: wsURL,
	})
	if err := cdp.Connect(context.Background()); err != nil {
		t.Fatalf("cdp.Connect: %v", err)
	}
	drv := golab.NewDriverWithBrowser(cdp, 9222, browser)
	t.Cleanup(func() { _ = drv.Close() })
	return drv
}

func TestProbeTabState_OwnerEditorViewOnlyDeniedAndAuthRedirect(t *testing.T) {
	mode := "owner_idle"
	drv := newTestDriver(t, &mockBrowser{}, func(expr string) (string, error) {
		if !strings.Contains(expr, "GOLAB_OP:probe_tab_state") {
			t.Fatalf("unexpected script: %s", expr)
		}
		switch mode {
		case "auth_redirect":
			return `{"url":"https://accounts.google.com/v3/signin","title":"Sign in","connection_state":"auth_required","permission":"auth_required","connect_state":"disconnected","kernel_state":"","is_executing":false,"identity":{"is_authenticated":false,"permission":"auth_required"}}`, nil
		case "denied":
			return `{"url":"https://colab.research.google.com/drive/1abc","title":"You need access","connection_state":"permission_denied","permission":"denied","connect_state":"disconnected","kernel_state":"","is_executing":false,"identity":{"email_address":"acc2@gmail.com","is_authenticated":true,"permission":"denied"}}`, nil
		case "view_only":
			return `{"url":"https://colab.research.google.com/drive/1abc","title":"Demo.ipynb","connection_state":"runtime_disconnected","permission":"view_only","connect_state":"disconnected","kernel_state":"connect","is_executing":false,"identity":{"email_address":"viewer@gmail.com","display_name":"Viewer","is_authenticated":true,"role":"reader","permission":"view_only","editable":false,"saveable":false,"is_sole_authored":false}}`, nil
		default:
			return `{"url":"https://colab.research.google.com/drive/1YsugLzQOOw3CJUOiqhVJM0U7fP62x4Du","title":"Exp.ipynb","connection_state":"idle","permission":"owner","connect_state":"connected","kernel_state":"idle","is_executing":false,"identity":{"email_address":"hoangvipper1105@gmail.com","display_name":"Hoang","is_authenticated":true,"drive_file_id":"1YsugLzQOOw3CJUOiqhVJM0U7fP62x4Du","title":"Exp.ipynb","role":"owner","permission":"owner","editable":true,"saveable":true,"is_sole_authored":true,"accelerator_name":"T4","runtime_uptime_seconds":312}}`, nil
		}
	})

	ctx := context.Background()
	p1, err := drv.ProbeTabState(ctx)
	if err != nil {
		t.Fatalf("ProbeTabState owner: %v", err)
	}
	if p1.ConnectionState != domain.StateIdle || p1.Permission != domain.PermissionOwner || !p1.Identity.IsSoleAuthored || p1.Identity.EmailAddress != "hoangvipper1105@gmail.com" {
		t.Fatalf("unexpected owner probe: %+v", p1)
	}

	mode = "auth_redirect"
	p2, err := drv.ProbeTabState(ctx)
	if err != nil || p2.ConnectionState != domain.StateAuthRequired || p2.Permission != domain.PermissionAuthRequired {
		t.Fatalf("unexpected auth probe: %+v, err=%v", p2, err)
	}

	mode = "denied"
	p3, err := drv.ProbeTabState(ctx)
	if err != nil || p3.ConnectionState != domain.StatePermissionDenied || p3.Permission != domain.PermissionDenied {
		t.Fatalf("unexpected denied probe: %+v, err=%v", p3, err)
	}

	mode = "view_only"
	p4, err := drv.ProbeTabState(ctx)
	if err != nil || p4.Permission != domain.PermissionViewOnly || p4.Permission.CanWrite() {
		t.Fatalf("unexpected view_only probe: %+v, err=%v", p4, err)
	}
}

func TestGetComputeUnitsAndListAccountVMSessions_LivePayload(t *testing.T) {
	drv := newTestDriver(t, &mockBrowser{}, func(expr string) (string, error) {
		switch {
		case strings.Contains(expr, "GOLAB_OP:get_compute_units"):
			return `{"assignments_count":1,"balance":15.246404817662752,"consumption_rate_hourly":0.08,"formatted_balance":"15.25","tier":"pro","eligible_gpus":["G4","A100","L4","T4"],"ineligible_gpus":["H100"],"eligible_tpus":["V6E1","V5E1"],"ineligible_tpus":[]}`, nil
		case strings.Contains(expr, "GOLAB_OP:list_vm_sessions"):
			return `[{"file_id":"1YsugLzQOOw3CJUOiqhVJM0U7fP62x4Du","title":"Exp.ipynb","kernel_id":"k-123","session_id":"s-456","accelerator":"GPU","accelerator_model":"T4","shape":"standard","managed_id":"m-s-xyz","proxy_url":"https://8080-m-s-xyz.prod.r.colab.dev","token_expires_sec":3600}]`, nil
		default:
			return "", errors.New("unexpected expression")
		}
	})

	ctx := context.Background()
	ccu, err := drv.GetComputeUnits(ctx)
	if err != nil {
		t.Fatalf("GetComputeUnits: %v", err)
	}
	if ccu.FormattedBalance != "15.25" || ccu.ConsumptionRateHourly != 0.08 || ccu.Tier != domain.TierPro || len(ccu.EligibleGPUs) != 4 {
		t.Fatalf("unexpected ccu: %+v", ccu)
	}

	vms, err := drv.ListAccountVMSessions(ctx)
	if err != nil {
		t.Fatalf("ListAccountVMSessions: %v", err)
	}
	if len(vms) != 1 || vms[0].AcceleratorModel != "T4" || vms[0].ManagedID != "m-s-xyz" {
		t.Fatalf("unexpected vms: %+v", vms)
	}
}

func TestCheckDriveStatus_FUSEMountedVsShadowLocalDir(t *testing.T) {
	mode := "shadow_dir"
	drv := newTestDriver(t, &mockBrowser{}, func(expr string) (string, error) {
		switch {
		case strings.Contains(expr, "GOLAB_OP:check_drive_fast"):
			if mode == "mounted" {
				return `{"mounted":true,"status":"mounted","mount_path":"/content/drive","is_fuse_verified":false,"button_state":"Unmount Drive","credentials_ok":true,"message":"Google Drive toolbar button indicates mounted and /api/colab/drive has active DFS."}`, nil
			}
			return `{"mounted":false,"status":"unmounted","mount_path":"/content/drive","is_fuse_verified":false,"button_state":"Mount Drive","credentials_ok":false,"message":"Google Drive is not mounted."}`, nil
		case strings.Contains(expr, "GOLAB_OP:exec_python"):
			if mode == "shadow_dir" {
				return `{"used_sidecar":false,"stdout":"{\"has_fuse\": false, \"is_mount\": false, \"dir_exists\": true, \"non_empty\": true}\n"}`, nil
			}
			return `{"used_sidecar":false,"stdout":"{\"has_fuse\": true, \"is_mount\": true, \"dir_exists\": true, \"non_empty\": true, \"total\": 115663142912, \"used\": 5884108800, \"free\": 109779034112}\n"}`, nil
		default:
			return "", errors.New("unexpected expr: " + expr)
		}
	})

	ctx := context.Background()
	stShadow, err := drv.CheckDriveStatus(ctx, true)
	if err != nil {
		t.Fatalf("CheckDriveStatus shadow: %v", err)
	}
	if stShadow.Mounted || stShadow.Status != "corrupted_local_dir" {
		t.Fatalf("expected corrupted_local_dir for shadow folder, got %+v", stShadow)
	}

	mode = "mounted"
	stFast, err := drv.CheckDriveStatus(ctx, false)
	if err != nil {
		t.Fatalf("CheckDriveStatus fast: %v", err)
	}
	if !stFast.Mounted || stFast.IsFUSEVerified {
		t.Fatalf("expected fast mounted without kernel FUSE check, got %+v", stFast)
	}

	stFull, err := drv.CheckDriveStatus(ctx, true)
	if err != nil {
		t.Fatalf("CheckDriveStatus full: %v", err)
	}
	if !stFull.Mounted || !stFull.IsFUSEVerified || stFull.TotalGB < 107.0 || stFull.FreeGB < 100.0 {
		t.Fatalf("expected FUSE verified with quota, got %+v", stFull)
	}
}

func TestCheckDriveStatus_DetectsPendingOAuthPopup(t *testing.T) {
	mb := &mockBrowser{
		targets: []domain.CDPTarget{
			{ID: "oauth-tab-77", Type: "page", URL: "https://accounts.google.com/o/oauth2/auth?client_id=colab"},
		},
	}
	drv := newTestDriver(t, mb, func(expr string) (string, error) {
		switch {
		case strings.Contains(expr, "GOLAB_OP:check_drive_fast"):
			return `{"mounted":false,"status":"unmounted","mount_path":"/content/drive","is_fuse_verified":false,"button_state":"Mount Drive","credentials_ok":false,"message":"Google Drive is not mounted."}`, nil
		case strings.Contains(expr, "GOLAB_OP:exec_python"):
			return `{"used_sidecar":false,"stdout":"{\"has_fuse\": false, \"is_mount\": false, \"dir_exists\": false, \"non_empty\": false}\n"}`, nil
		default:
			return "", errors.New("unexpected expr: " + expr)
		}
	})

	st, err := drv.CheckDriveStatus(context.Background(), true)
	if err != nil {
		t.Fatalf("CheckDriveStatus pending popup: %v", err)
	}
	if st.Mounted || st.Status != "drive_auth_pending" || st.AuthPopupTabID != "oauth-tab-77" || !strings.Contains(st.AuthPopupURL, "accounts.google.com") {
		t.Fatalf("expected drive_auth_pending with popup metadata, got %+v", st)
	}
}

func TestCheckDriveStatus_IgnoresRotateCookiesIframeAndDetectsManualSharedMount(t *testing.T) {
	// Reproduces exact live bug: Chrome has hidden RotateCookiesPage iframe + Gemini RotateCookiesPage iframe.
	mb := &mockBrowser{
		targets: []domain.CDPTarget{
			{
				ID:   "F2A6704DE316DEC9AB7D8A9CCA49C36A",
				Type: "page",
				URL:  "https://colab.research.google.com/drive/1GGXu3tPKe2Vup5RFd3-I9v3fWdRhc2v6",
			},
			{
				ID:   "F26B5363138C4ECE8D2A73FD4DCD6FCD",
				Type: "iframe",
				URL:  "https://accounts.google.com/RotateCookiesPage?og_pid=425&rot=3&origin=https%3A%2F%2Fcolab.research.google.com&exp_id=0",
			},
			{
				ID:   "7C70A785E072F485F8723A8F8EA2C20D",
				Type: "page",
				URL:  "https://accounts.google.com/RotateCookiesPage?origin=https%3A%2F%2Fgemini.google.com&hfcr_pid=2",
			},
		},
	}

	isMounted := false
	drv := newTestDriver(t, mb, func(expr string) (string, error) {
		switch {
		case strings.Contains(expr, "GOLAB_OP:check_drive_fast"):
			if isMounted {
				return `{"mounted":true,"status":"mounted","mount_path":"/content/drive","is_fuse_verified":false,"button_state":"Unmount Drive","credentials_ok":true,"message":"Google Drive (/content/drive/MyDrive) is mounted in this Colab runtime."}`, nil
			}
			return `{"mounted":false,"status":"unmounted","mount_path":"/content/drive","is_fuse_verified":false,"button_state":"Mount Drive","credentials_ok":false,"message":"Google Drive is not mounted."}`, nil
		case strings.Contains(expr, "GOLAB_OP:exec_python"):
			if isMounted {
				return `{"used_sidecar":false,"stdout":"{\"has_fuse\": true, \"is_mount\": true, \"dir_exists\": true, \"non_empty\": true, \"total\": 120942624768, \"used\": 6204850176, \"free\": 114737774592}\n"}`, nil
			}
			return `{"used_sidecar":false,"stdout":"{\"has_fuse\": false, \"is_mount\": false, \"dir_exists\": false, \"non_empty\": false}\n"}`, nil
		default:
			return "", errors.New("unexpected expr: " + expr)
		}
	})

	ctx := context.Background()
	// 1. When unmounted, RotateCookiesPage MUST NOT trigger drive_auth_pending!
	stUnmounted, err := drv.CheckDriveStatus(ctx, true)
	if err != nil {
		t.Fatalf("CheckDriveStatus unmounted: %v", err)
	}
	if stUnmounted.Mounted || stUnmounted.Status != "unmounted" || stUnmounted.AuthPopupTabID != "" {
		t.Fatalf("RotateCookiesPage falsely triggered %q: %+v", stUnmounted.Status, stUnmounted)
	}

	// 2. When manually mounted on web, both fast check and full FUSE check MUST return mounted=true!
	isMounted = true
	stFast, err := drv.CheckDriveStatus(ctx, false)
	if err != nil || !stFast.Mounted || stFast.Status != "mounted" {
		t.Fatalf("expected fast manual mount detection, got %+v, err=%v", stFast, err)
	}
	stFull, err := drv.CheckDriveStatus(ctx, true)
	if err != nil || !stFull.Mounted || !stFull.IsFUSEVerified || stFull.Status != "mounted" || stFull.TotalGB < 112.0 {
		t.Fatalf("expected FUSE-verified manual mount with real quota, got %+v, err=%v", stFull, err)
	}
}

func TestMountDrive_SoleAuthoredAndSharedAccountOAuthPending(t *testing.T) {
	isSoleAuthored := true
	soleMounted := false
	mb := &mockBrowser{}
	drv := newTestDriver(t, mb, func(expr string) (string, error) {
		switch {
		case strings.Contains(expr, "GOLAB_OP:mount_drive_probe"):
			if isSoleAuthored {
				return `{"is_sole_authored":true}`, nil
			}
			return `{"is_sole_authored":false}`, nil
		case strings.Contains(expr, "GOLAB_OP:mount_drive_sole"):
			soleMounted = true
			return `{"clicked_mount":true,"confirmed_dialog":true}`, nil
		case strings.Contains(expr, "GOLAB_OP:mount_drive_shared_trigger"):
			mb.mu.Lock()
			mb.targets = []domain.CDPTarget{
				{ID: "oauth-popup-99", Type: "page", Title: "Sign in - Google Accounts", URL: "https://accounts.google.com/o/oauth2/auth?client_id=colab"},
			}
			mb.mu.Unlock()
			return `{"triggered":true,"confirmed_dialog":true}`, nil
		case strings.Contains(expr, "GOLAB_OP:check_drive_fast"):
			if isSoleAuthored && soleMounted {
				return `{"mounted":true,"status":"mounted","mount_path":"/content/drive","button_state":"Unmount Drive","credentials_ok":true}`, nil
			}
			return `{"mounted":false,"status":"unmounted","mount_path":"/content/drive","button_state":"Mount Drive","credentials_ok":false}`, nil
		case strings.Contains(expr, "GOLAB_OP:exec_python"):
			if isSoleAuthored && soleMounted {
				return `{"used_sidecar":false,"stdout":"{\"has_fuse\": true, \"is_mount\": true, \"dir_exists\": true, \"non_empty\": true, \"total\": 107374182400, \"used\": 5368709120, \"free\": 102005473280}\n"}`, nil
			}
			return `{"used_sidecar":true,"stdout":"{\"has_fuse\": false, \"is_mount\": false, \"dir_exists\": false, \"non_empty\": false}\n"}`, nil
		default:
			return "", errors.New("unexpected expr: " + expr)
		}
	})

	ctx := context.Background()
	resSole, err := drv.MountDrive(ctx)
	if err != nil {
		t.Fatalf("MountDrive sole: %v", err)
	}
	if !resSole.Mounted || resSole.Flow != "sole_authored_persistent" || !resSole.DriveStatus.IsFUSEVerified {
		t.Fatalf("unexpected sole mount result: %+v", resSole)
	}

	isSoleAuthored = false
	resShared, err := drv.MountDrive(ctx)
	var appErr *domain.AppError
	if !errors.As(err, &appErr) || appErr.Code != domain.CodeDriveOAuthRequired {
		t.Fatalf("expected GOLAB_DRIVE_OAUTH_INTERACTION_REQUIRED for shared account with pending OAuth popup, got res=%+v err=%v", resShared, err)
	}
	if resShared == nil || resShared.Status != "drive_auth_pending" || resShared.AuthPopupTabID != "oauth-popup-99" {
		t.Fatalf("expected auth popup metadata in DriveMountResult, got %+v", resShared)
	}
}

func TestExecPythonAndBackgroundJobs_SidecarWhenPrimaryBusy(t *testing.T) {
	drv := newTestDriver(t, &mockBrowser{}, func(expr string) (string, error) {
		if !strings.Contains(expr, "GOLAB_OP:exec_python") {
			return "", errors.New("unexpected expr: " + expr)
		}
		if strings.Contains(expr, `"prefer_sidecar":true`) && strings.Contains(expr, "golab_job_action") {
			switch {
			case strings.Contains(expr, `golab_job_action\\\"`) && strings.Contains(expr, `\\\"poll\\\"`):
				return `{"used_sidecar":true,"kernel_id":"sidecar-k2","stdout":"{\"job_id\":\"job_1\",\"pid\":4242,\"running\":false,\"exit_code\":0,\"log_tail\":\"done!\\n\",\"log_bytes\":18,\"log_path\":\"/tmp/.golab_jobs/job_1.log\",\"elapsed_sec\":1.4}\n"}`, nil
			case strings.Contains(expr, `golab_job_action\\\"`) && strings.Contains(expr, `\\\"kill\\\"`):
				return `{"used_sidecar":true,"kernel_id":"sidecar-k2","stdout":"{\"job_id\":\"job_1\",\"pid\":4242,\"running\":false,\"exit_code\":-15,\"log_tail\":\"killed\\n\",\"log_bytes\":25,\"log_path\":\"/tmp/.golab_jobs/job_1.log\",\"elapsed_sec\":1.6}\n"}`, nil
			default:
				return `{"used_sidecar":true,"kernel_id":"sidecar-k2","stdout":"{\"job_id\":\"job_1\",\"pid\":4242,\"running\":true,\"log_tail\":\"starting...\\n\",\"log_bytes\":12,\"log_path\":\"/tmp/.golab_jobs/job_1.log\",\"elapsed_sec\":0.1}\n"}`, nil
			}
		}
		return `{"used_sidecar":true,"kernel_id":"sidecar-k2","stdout":"hello from sidecar\n","result":"42"}`, nil
	})

	ctx := context.Background()
	pyRes, err := drv.ExecPython(ctx, "print('hello from sidecar'); 40+2", true)
	if err != nil {
		t.Fatalf("ExecPython: %v", err)
	}
	if !pyRes.UsedSidecar || pyRes.KernelID != "sidecar-k2" || !strings.Contains(pyRes.Stdout, "hello from sidecar") {
		t.Fatalf("unexpected ExecPython result: %+v", pyRes)
	}

	jobStart, err := drv.StartBackgroundJob(ctx, "python3 train_prep.py", "/content")
	if err != nil || !jobStart.Running || !jobStart.UsedSidecar || jobStart.PID != 4242 {
		t.Fatalf("unexpected StartBackgroundJob: %+v, err=%v", jobStart, err)
	}

	jobPoll, err := drv.PollBackgroundJob(ctx, "job_1", 50)
	if err != nil || jobPoll.Running || jobPoll.ExitCode == nil || *jobPoll.ExitCode != 0 {
		t.Fatalf("unexpected PollBackgroundJob: %+v, err=%v", jobPoll, err)
	}

	jobKill, err := drv.KillBackgroundJob(ctx, "job_1")
	if err != nil || jobKill.Running {
		t.Fatalf("unexpected KillBackgroundJob: %+v, err=%v", jobKill, err)
	}
}

func TestRunCodeCellBounded_NeverBlocksOnLongTraining(t *testing.T) {
	var gotWaitMs bool
	drv := newTestDriver(t, &mockBrowser{}, func(expr string) (string, error) {
		if !strings.Contains(expr, "GOLAB_OP:run_code_cell") {
			return "", errors.New("expected GOLAB_OP:run_code_cell, got: " + expr)
		}
		if strings.Contains(expr, `"wait_ms":0`) {
			gotWaitMs = true
		}
		return `{"success":true,"result":{"cell_id":"cell-train","index":3,"status":"running","is_running":true,"execution_count":1,"elapsed_seconds":0.1,"stdout":"[PLAN] Training 5 hours...\n","stderr":"","error_text":"","outputs":[],"message":"Cell is actively executing in the background."}}`, nil
	})

	raw, err := drv.CallColabMcpTool(context.Background(), "run_code_cell", map[string]any{
		"cellId": "cell-train",
		"waitMs": 0,
	})
	if err != nil {
		t.Fatalf("CallColabMcpTool run_code_cell: %v", err)
	}
	if !gotWaitMs {
		t.Fatalf("expected wait_ms=0 to be forwarded to ScriptRunCellBounded")
	}
	var parsed struct {
		CellID    string `json:"cell_id"`
		Status    string `json:"status"`
		IsRunning bool   `json:"is_running"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal run_code_cell result: %v", err)
	}
	if parsed.CellID != "cell-train" || parsed.Status != "running" || !parsed.IsRunning {
		t.Fatalf("expected non-blocking running status, got %+v", parsed)
	}
}
