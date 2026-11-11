package mcpserver_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/hoangnecon/golabv2/internal/domain"
	"github.com/hoangnecon/golabv2/internal/mcpserver"
	"github.com/hoangnecon/golabv2/internal/service"
	"github.com/hoangnecon/golabv2/internal/session"
	"github.com/hoangnecon/golabv2/internal/store/sqlite"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type mcpFakeBrowser struct{}

func (mcpFakeBrowser) LaunchProfile(ctx context.Context, profile domain.Profile, initialURL string) (int, int, error) {
	return 7000 + profile.CDPPort, profile.CDPPort, nil
}
func (mcpFakeBrowser) StopProfile(ctx context.Context, profile domain.Profile) error { return nil }
func (mcpFakeBrowser) IsProfileRunning(ctx context.Context, cdpPort int) bool        { return true }
func (mcpFakeBrowser) ListTargets(ctx context.Context, cdpPort int) ([]domain.CDPTarget, error) {
	return nil, nil
}
func (mcpFakeBrowser) OpenOrFocusTab(ctx context.Context, cdpPort int, targetURL string, matchSubstr string) (domain.CDPTarget, error) {
	return domain.CDPTarget{ID: "tab-" + matchSubstr, Type: "page", URL: targetURL}, nil
}
func (mcpFakeBrowser) FocusTab(ctx context.Context, cdpPort int, tabID string) error { return nil }
func (mcpFakeBrowser) CloseTab(ctx context.Context, cdpPort int, tabID string) error { return nil }
func (mcpFakeBrowser) ShutdownAdapter() error                                        { return nil }

type mcpFakeDriver struct {
	accountEmail string
	perm         domain.PermissionLevel
	notebookID   string
	oauthPending bool
}

func (d *mcpFakeDriver) Close() error { return nil }
func (d *mcpFakeDriver) ProbeTabState(ctx context.Context) (*domain.TabProbe, error) {
	return &domain.TabProbe{
		URL:             "https://colab.research.google.com/" + d.notebookID,
		Title:           d.notebookID + ".ipynb",
		ConnectionState: domain.StateIdle,
		Permission:      d.perm,
		Identity: domain.NotebookIdentity{
			EmailAddress: d.accountEmail,
			Permission:   d.perm,
			Editable:     d.perm.CanWrite(),
		},
	}, nil
}
func (d *mcpFakeDriver) GetComputeUnits(ctx context.Context) (*domain.ComputeUnitsInfo, error) {
	return &domain.ComputeUnitsInfo{Balance: 15.25, FormattedBalance: "15.25", Tier: domain.TierPro}, nil
}
func (d *mcpFakeDriver) ListAccountVMSessions(ctx context.Context) ([]domain.ColabVMSession, error) {
	return []domain.ColabVMSession{{FileID: d.notebookID, Accelerator: "T4"}}, nil
}
func (d *mcpFakeDriver) FetchKernelXHR(ctx context.Context, path string, opts domain.FetchOptions) (*domain.FetchResponse, error) {
	if strings.HasPrefix(path, "/api/sessions") {
		return &domain.FetchResponse{Status: 200, OK: true, Body: json.RawMessage(`[{"id":"s1","kernel":{"id":"k1","name":"python3","execution_state":"idle"}}]`)}, nil
	}
	if strings.HasPrefix(path, "/api/kernels") {
		return &domain.FetchResponse{Status: 200, OK: true, Body: json.RawMessage(`[{"id":"k1","name":"python3","execution_state":"idle"}]`)}, nil
	}
	return &domain.FetchResponse{Status: 200, OK: true, Body: json.RawMessage(`{"name":"root","path":"","type":"directory","writable":true,"content":[]}`)}, nil
}
func (d *mcpFakeDriver) CallColabMcpTool(ctx context.Context, toolName string, args map[string]any) (json.RawMessage, error) {
	switch toolName {
	case "get_cells":
		b, _ := json.Marshal(map[string]any{
			"cells": []map[string]any{
				{
					"id":        "cell-1",
					"cell_type": "code",
					"source":    []string{"# owner=" + d.accountEmail + " nb=" + d.notebookID},
					"outputs":   []map[string]any{},
				},
			},
		})
		return b, nil
	case "add_code_cell", "add_text_cell":
		return json.RawMessage(`{"newCellId":"cell-added"}`), nil
	default:
		return json.RawMessage(`{"ok":true}`), nil
	}
}
func (d *mcpFakeDriver) ConnectRuntime(ctx context.Context) error    { return nil }
func (d *mcpFakeDriver) DisconnectRuntime(ctx context.Context) error { return nil }
func (d *mcpFakeDriver) InterruptKernel(ctx context.Context) error   { return nil }
func (d *mcpFakeDriver) RestartKernel(ctx context.Context) error     { return nil }
func (d *mcpFakeDriver) GetAvailableAccelerators(ctx context.Context) (*domain.RuntimeConfigReport, error) {
	return &domain.RuntimeConfigReport{Accelerators: []domain.AcceleratorOption{{Name: "T4 GPU", Value: "T4", Checked: true}}}, nil
}
func (d *mcpFakeDriver) SetAccelerator(ctx context.Context, value string, highRAM bool) error {
	return nil
}
func (d *mcpFakeDriver) ExecPython(ctx context.Context, code string, preferSidecarWhenBusy bool) (*domain.PythonExecResult, error) {
	return &domain.PythonExecResult{Stdout: "hello\n", UsedSidecar: preferSidecarWhenBusy}, nil
}
func (d *mcpFakeDriver) GetHardwareTelemetry(ctx context.Context) (*domain.HardwareTelemetry, error) {
	return &domain.HardwareTelemetry{}, nil
}
func (d *mcpFakeDriver) GetBuildInfo(ctx context.Context) (*domain.BuildInfo, error) {
	return &domain.BuildInfo{ReleaseTag: "2026-11-11"}, nil
}
func (d *mcpFakeDriver) CheckDriveStatus(ctx context.Context, allowKernelVerify bool) (*domain.DriveStatus, error) {
	return &domain.DriveStatus{Mounted: true, Status: "mounted", IsFUSEVerified: true}, nil
}
func (d *mcpFakeDriver) MountDrive(ctx context.Context) (*domain.DriveMountResult, error) {
	if d.oauthPending {
		return &domain.DriveMountResult{
			Mounted:        false,
			Flow:           "shared_ephemeral",
			Status:         "drive_auth_pending",
			AuthPopupTabID: "oauth-popup-test-1",
			AuthPopupURL:   "https://accounts.google.com/o/oauth2/auth?client_id=colab",
			DriveStatus: domain.DriveStatus{
				Mounted:        false,
				Status:         "drive_auth_pending",
				MountPath:      "/content/drive",
				AuthPopupTabID: "oauth-popup-test-1",
				AuthPopupURL:   "https://accounts.google.com/o/oauth2/auth?client_id=colab",
			},
		}, domain.NewAppError(domain.CodeDriveOAuthRequired, "OAuth required", "Complete Google sign-in in popup.", 428)
	}
	return &domain.DriveMountResult{Mounted: true, Status: "mounted", DriveStatus: domain.DriveStatus{Mounted: true, Status: "mounted"}}, nil
}
func (d *mcpFakeDriver) StartBackgroundJob(ctx context.Context, command string, cwd string) (*domain.BackgroundJob, error) {
	return &domain.BackgroundJob{JobID: "job-1", Command: command, Running: true}, nil
}
func (d *mcpFakeDriver) PollBackgroundJob(ctx context.Context, jobID string, tailLines int) (*domain.BackgroundJob, error) {
	return &domain.BackgroundJob{JobID: jobID, Running: false, ExitCode: ptrInt(0)}, nil
}
func (d *mcpFakeDriver) KillBackgroundJob(ctx context.Context, jobID string) (*domain.BackgroundJob, error) {
	return &domain.BackgroundJob{JobID: jobID, Running: false}, nil
}
func (d *mcpFakeDriver) GetRunningCells(ctx context.Context) (*domain.RunningCellsReport, error) {
	return &domain.RunningCellsReport{}, nil
}
func (d *mcpFakeDriver) SaveNotebook(ctx context.Context) error { return nil }
func (d *mcpFakeDriver) RunAll(ctx context.Context) error       { return nil }
func (d *mcpFakeDriver) GetProxyURL(ctx context.Context, port int) (*domain.ProxyEndpoint, error) {
	return &domain.ProxyEndpoint{Port: port, URL: "https://colab-proxy.example"}, nil
}

func ptrInt(v int) *int { return &v }

type mcpFakeDriverFactory struct {
	mu           sync.Mutex
	perms        map[int]map[string]domain.PermissionLevel // cdpPort -> notebookID -> perm
	email        map[int]string
	oauthPending bool
}

func (f *mcpFakeDriverFactory) NewDriver(ctx context.Context, cdpPort int, target domain.CDPTarget) (domain.RuntimeDriver, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	nbID := strings.TrimPrefix(target.ID, "tab-")
	perm := domain.PermissionEditor
	if byNB, ok := f.perms[cdpPort]; ok {
		if p, ok2 := byNB[nbID]; ok2 {
			perm = p
		}
	}
	em := f.email[cdpPort]
	if em == "" {
		em = "owner@gmail.com"
	}
	return &mcpFakeDriver{
		accountEmail: em,
		perm:         perm,
		notebookID:   nbID,
		oauthPending: f.oauthPending,
	}, nil
}

type mcpTestEnv struct {
	store    *sqlite.Store
	coord    *session.Coordinator
	profSvc  *service.ProfileService
	projSvc  *service.ProjectService
	sessSvc  *service.SessionService
	actSvc   *service.ActivityService
	wsSvc    *service.WorkspaceService
	ts       *httptest.Server
	factory  *mcpFakeDriverFactory
	prof1    *domain.Profile
	prof2    *domain.Profile
}

func setupMCPTestEnv(t *testing.T) *mcpTestEnv {
	t.Helper()
	tmp := t.TempDir()
	st, err := sqlite.Open(filepath.Join(tmp, "mcp_v2.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	bus := session.NewEventBus()
	browser := mcpFakeBrowser{}
	factory := &mcpFakeDriverFactory{
		perms: map[int]map[string]domain.PermissionLevel{
			9222: {
				"drive/1AlphaNotebookID0001": domain.PermissionOwner,
				"drive/1BetaNotebookID00002": domain.PermissionOwner,
			},
			9223: {
				"drive/1AlphaNotebookID0001": domain.PermissionEditor,
				"drive/1BetaNotebookID00002": domain.PermissionViewOnly,
			},
		},
		email: map[int]string{
			9222: "acc1@gmail.com",
			9223: "acc2@gmail.com",
		},
	}

	coord := session.NewCoordinator(st, browser, factory, bus, session.CoordinatorOptions{})
	t.Cleanup(func() { _ = coord.Close() })

	profSvc := service.NewProfileService(st, browser, coord, bus, filepath.Join(tmp, "profiles"))
	prof1, err := profSvc.EnsureDefaultProfile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	prof2, err := profSvc.CreateProfile(context.Background(), "Account 2", "acc2@gmail.com", domain.TierPro)
	if err != nil {
		t.Fatal(err)
	}
	projSvc := service.NewProjectService(st, coord, bus)
	sessSvc := service.NewSessionService(st, coord, bus)
	actSvc := service.NewActivityService(st, bus)
	wsSvc := service.NewWorkspaceService(st, browser, "v2.0.0-test", "http://127.0.0.1:9120/mcp")

	handler := mcpserver.NewHTTPHandler(mcpserver.Dependencies{
		Coordinator:  coord,
		WorkspaceSvc: wsSvc,
		ProfileSvc:   profSvc,
		ProjectSvc:   projSvc,
		SessionSvc:   sessSvc,
		ActivitySvc:  actSvc,
	})
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	return &mcpTestEnv{
		store:   st,
		coord:   coord,
		profSvc: profSvc,
		projSvc: projSvc,
		sessSvc: sessSvc,
		actSvc:  actSvc,
		wsSvc:   wsSvc,
		ts:      ts,
		factory: factory,
		prof1:   prof1,
		prof2:   prof2,
	}
}

func connectHTTPMCPClient(t *testing.T, ctx context.Context, endpoint string) *mcp.ClientSession {
	t.Helper()
	transport := &mcp.StreamableClientTransport{
		Endpoint:             endpoint,
		DisableStandaloneSSE: true,
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1.0.0"}, nil).Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connect MCP HTTP client (%s): %v", endpoint, err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func TestExact43CanonicalToolsRegistered(t *testing.T) {
	env := setupMCPTestEnv(t)
	ctx := context.Background()
	cs := connectHTTPMCPClient(t, ctx, env.ts.URL)

	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	expected := []string{
		// Group 1 (4)
		"colab_get_session_context",
		"colab_list_all_sessions",
		"colab_open_session_tab",
		"colab_get_account_quota",
		// Group 2 (8)
		"colab_get_kernel_status",
		"colab_connect_runtime",
		"colab_disconnect_runtime",
		"colab_interrupt_kernel",
		"colab_restart_kernel",
		"colab_get_available_accelerators",
		"colab_set_accelerator",
		"colab_exec_python",
		// Group 3 (5)
		"colab_get_hardware_telemetry",
		"colab_get_compute_units",
		"colab_check_drive_status",
		"colab_mount_drive",
		"colab_get_build_info",
		// Group 4 (7)
		"colab_list_directory",
		"colab_stat_path",
		"colab_read_file",
		"colab_write_file",
		"colab_create_directory",
		"colab_move_file",
		"colab_delete_file",
		// Group 5 (9)
		"colab_get_notebook_outline",
		"colab_get_cells",
		"colab_get_cell_with_lines",
		"colab_search_cells",
		"colab_add_cell",
		"colab_update_cell",
		"colab_edit_cell_lines",
		"colab_move_cell",
		"colab_delete_cell",
		// Group 6 (5)
		"colab_run_code_cell",
		"colab_run_all_cells",
		"colab_get_running_cells",
		"colab_get_cell_output",
		"colab_save_notebook",
		// Group 7 (5)
		"colab_start_background_job",
		"colab_poll_background_job",
		"colab_kill_background_job",
		"colab_proxy_forward",
		"colab_get_proxy_url",
	}

	if len(expected) != 43 {
		t.Fatalf("sanity check: expected slice has %d tools, want 43", len(expected))
	}

	var got []string
	for _, tool := range res.Tools {
		if strings.TrimSpace(tool.Description) == "" {
			t.Errorf("tool %s has empty description", tool.Name)
		}
		if tool.InputSchema == nil {
			t.Errorf("tool %s has nil InputSchema", tool.Name)
		}
		got = append(got, tool.Name)
	}

	sort.Strings(expected)
	sort.Strings(got)

	if len(got) != 43 {
		t.Fatalf("expected exactly 43 registered tools, got %d: %v", len(got), got)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("tool mismatch at %d: got %q, want %q\nall got: %v", i, got[i], expected[i], got)
		}
	}
}

func TestMultiClientConcurrentProjectTokensAndSeamlessFailover(t *testing.T) {
	env := setupMCPTestEnv(t)
	ctx := context.Background()

	projA, sessA1, err := env.projSvc.CreateProject(ctx, "Project Alpha", "https://colab.research.google.com/drive/1AlphaNotebookID0001", env.prof1.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	sessA2, err := env.sessSvc.CreateSession(ctx, projA.ID, env.prof2.ID, "", false)
	if err != nil {
		t.Fatal(err)
	}

	projB, _, err := env.projSvc.CreateProject(ctx, "Project Beta", "https://colab.research.google.com/drive/1BetaNotebookID00002", env.prof1.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	sessB2, err := env.sessSvc.CreateSession(ctx, projB.ID, env.prof2.ID, "", true)
	if err != nil {
		t.Fatal(err)
	}
	_ = sessA1
	_ = sessB2

	clientA := connectHTTPMCPClient(t, ctx, env.ts.URL+"?token="+projA.ProjectToken)
	clientB := connectHTTPMCPClient(t, ctx, env.ts.URL+"?token="+projB.ProjectToken)

	resA1, err := clientA.CallTool(ctx, &mcp.CallToolParams{Name: "colab_get_cells", Arguments: map[string]any{}})
	if err != nil || resA1.IsError {
		t.Fatalf("clientA initial call failed: err=%v res=%+v", err, resA1)
	}
	textA1 := resA1.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(textA1, "acc1@gmail.com") || !strings.Contains(textA1, "1AlphaNotebookID0001") {
		t.Fatalf("expected Project Alpha + acc1, got %s", textA1)
	}

	resB, err := clientB.CallTool(ctx, &mcp.CallToolParams{Name: "colab_get_cells", Arguments: map[string]any{}})
	if err != nil || resB.IsError {
		t.Fatalf("clientB call failed: err=%v res=%+v", err, resB)
	}
	textB := resB.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(textB, "acc2@gmail.com") || !strings.Contains(textB, "1BetaNotebookID00002") {
		t.Fatalf("expected Project Beta + acc2, got %s", textB)
	}

	// Switch Project Alpha lock from Account 1 -> Account 2 WITHOUT reconnecting clientA!
	if _, err := env.sessSvc.LockSession(ctx, sessA2.ID, true, true); err != nil {
		t.Fatalf("switch lock A1->A2: %v", err)
	}

	resA2, err := clientA.CallTool(ctx, &mcp.CallToolParams{Name: "colab_get_cells", Arguments: map[string]any{}})
	if err != nil || resA2.IsError {
		t.Fatalf("clientA post-failover call failed: err=%v res=%+v", err, resA2)
	}
	textA2 := resA2.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(textA2, "acc2@gmail.com") {
		t.Fatalf("expected clientA to seamlessly failover to acc2@gmail.com, got %s", textA2)
	}
}

func TestNoTokenAmbiguousAndUnlockedErrorsAreAudited(t *testing.T) {
	env := setupMCPTestEnv(t)
	ctx := context.Background()

	projA, _, err := env.projSvc.CreateProject(ctx, "Alpha", "https://colab.research.google.com/drive/1AlphaNotebookID0001", env.prof1.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	projB, _, err := env.projSvc.CreateProject(ctx, "Beta", "https://colab.research.google.com/drive/1BetaNotebookID00002", env.prof2.ID, true)
	if err != nil {
		t.Fatal(err)
	}

	// 1. Tokenless client with 2 locked projects -> GOLAB_AMBIGUOUS_MULTI_PROJECT_SESSION AND audited!
	noTokClient := connectHTTPMCPClient(t, ctx, env.ts.URL)
	ambRes, err := noTokClient.CallTool(ctx, &mcp.CallToolParams{
		Name:      "colab_get_cells",
		Arguments: map[string]any{"start_index": 0},
	})
	if err != nil {
		t.Fatalf("CallTool transport error: %v", err)
	}
	if !ambRes.IsError || !strings.Contains(ambRes.Content[0].(*mcp.TextContent).Text, domain.CodeAmbiguousMultiProjectSession) {
		t.Fatalf("expected GOLAB_AMBIGUOUS_MULTI_PROJECT_SESSION, got %+v", ambRes.Content[0])
	}

	// 2. Unlock Beta, then call with Beta's glp_ token -> GOLAB_PROJECT_UNLOCKED AND audited with project_id!
	if _, err := env.projSvc.UnlockProject(ctx, projB.ID); err != nil {
		t.Fatal(err)
	}
	betaClient := connectHTTPMCPClient(t, ctx, env.ts.URL+"?token="+projB.ProjectToken)
	unlRes, err := betaClient.CallTool(ctx, &mcp.CallToolParams{
		Name:      "colab_get_kernel_status",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !unlRes.IsError || !strings.Contains(unlRes.Content[0].(*mcp.TextContent).Text, domain.CodeProjectUnlocked) {
		t.Fatalf("expected GOLAB_PROJECT_UNLOCKED, got %+v", unlRes.Content[0])
	}
	_ = projA

	logs, _, err := env.actSvc.List(ctx, domain.ActivityFilter{Limit: 10})
	if err != nil || len(logs) < 2 {
		t.Fatalf("expected >= 2 audited error rows, got %d (err=%v): %+v", len(logs), err, logs)
	}

	if logs[0].ErrorCode != domain.CodeProjectUnlocked || logs[0].ProjectID != projB.ID || logs[0].TokenPrefix == "" {
		t.Fatalf("expected latest audit row to record GOLAB_PROJECT_UNLOCKED + projectB.ID + token_prefix, got %+v", logs[0])
	}
	if logs[1].ErrorCode != domain.CodeAmbiguousMultiProjectSession || !logs[1].HasPayload {
		t.Fatalf("expected ambiguous session error row with payload, got %+v", logs[1])
	}
}

func TestViewOnlySessionBlocksMutationsButAllowsReads(t *testing.T) {
	env := setupMCPTestEnv(t)
	ctx := context.Background()

	// On Project Beta, Account 2 (prof2, port 9223) has PermissionViewOnly
	projB, errProj, err := env.projSvc.CreateProject(ctx, "Beta", "https://colab.research.google.com/drive/1BetaNotebookID00002", env.prof2.ID, true)
	_ = errProj
	if err != nil {
		t.Fatal(err)
	}

	client := connectHTTPMCPClient(t, ctx, env.ts.URL+"?token="+projB.ProjectToken)

	// Read tool succeeds
	readRes, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "colab_get_cells", Arguments: map[string]any{}})
	if err != nil || readRes.IsError {
		t.Fatalf("expected colab_get_cells to succeed on view_only session, got err=%v res=%+v", err, readRes)
	}

	// Mutating tools fail with GOLAB_PERMISSION_READ_ONLY
	for _, mutTool := range []struct {
		name string
		args map[string]any
	}{
		{"colab_add_cell", map[string]any{"index": 0, "cell_type": "code", "content": "print(1)"}},
		{"colab_save_notebook", map[string]any{}},
	} {
		mutRes, err := client.CallTool(ctx, &mcp.CallToolParams{Name: mutTool.name, Arguments: mutTool.args})
		if err != nil {
			t.Fatalf("transport error on %s: %v", mutTool.name, err)
		}
		txt := mutRes.Content[0].(*mcp.TextContent).Text
		if !mutRes.IsError || !strings.Contains(txt, domain.CodePermissionReadOnly) {
			t.Fatalf("expected %s to fail with GOLAB_PERMISSION_READ_ONLY, got %s", mutTool.name, txt)
		}
	}
}

func TestStdioBridgeRoundTrip(t *testing.T) {
	env := setupMCPTestEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	projA, _, err := env.projSvc.CreateProject(ctx, "Alpha", "https://colab.research.google.com/drive/1AlphaNotebookID0001", env.prof1.ID, true)
	if err != nil {
		t.Fatal(err)
	}

	// Client <-> Bridge over io.Pipe pairs
	clientToBridgeR, clientToBridgeW := io.Pipe()
	bridgeToClientR, bridgeToClientW := io.Pipe()

	bridgeDone := make(chan error, 1)
	go func() {
		bridgeDone <- mcpserver.RunIOBridge(ctx, clientToBridgeR, bridgeToClientW, env.ts.URL, projA.ProjectToken)
	}()

	clientTransport := &mcp.IOTransport{
		Reader: bridgeToClientR,
		Writer: clientToBridgeW,
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "stdio-ide", Version: "1.0.0"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect stdio client through bridge: %v", err)
	}

	toolsRes, err := cs.ListTools(ctx, nil)
	if err != nil || len(toolsRes.Tools) != 43 {
		t.Fatalf("ListTools over stdio bridge: len=%d err=%v", len(toolsRes.Tools), err)
	}

	callRes, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "colab_get_session_context",
		Arguments: map[string]any{},
	})
	if err != nil || callRes.IsError {
		t.Fatalf("CallTool over stdio bridge failed: err=%v res=%+v", err, callRes)
	}
	if !strings.Contains(callRes.Content[0].(*mcp.TextContent).Text, projA.ID) {
		t.Fatalf("expected session context for projA, got %s", callRes.Content[0].(*mcp.TextContent).Text)
	}

	_ = cs.Close()
	_ = clientToBridgeW.Close()
	cancel()
	<-bridgeDone
}

func TestMountDrivePreservesOAuthPopupDataOnError(t *testing.T) {
	env := setupMCPTestEnv(t)
	ctx := context.Background()

	env.factory.mu.Lock()
	env.factory.oauthPending = true
	env.factory.mu.Unlock()

	projA, _, err := env.projSvc.CreateProject(ctx, "Alpha", "https://colab.research.google.com/drive/1AlphaNotebookID0001", env.prof2.ID, true)
	if err != nil {
		t.Fatal(err)
	}

	client := connectHTTPMCPClient(t, ctx, env.ts.URL+"?token="+projA.ProjectToken)
	res, err := client.CallTool(ctx, &mcp.CallToolParams{
		Name:      "colab_mount_drive",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool transport error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError=true when OAuth popup interaction is required")
	}

	var envResp struct {
		Status string `json:"status"`
		Data   struct {
			Status         string `json:"status"`
			AuthPopupTabID string `json:"auth_popup_tab_id"`
			AuthPopupURL   string `json:"auth_popup_url"`
		} `json:"data"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	txt := res.Content[0].(*mcp.TextContent).Text
	if err := json.Unmarshal([]byte(txt), &envResp); err != nil {
		t.Fatalf("unmarshal envelope: %v (raw=%s)", err, txt)
	}
	if envResp.Error.Code != domain.CodeDriveOAuthRequired || envResp.Data.Status != "drive_auth_pending" || envResp.Data.AuthPopupTabID != "oauth-popup-test-1" {
		t.Fatalf("expected OAuth popup metadata preserved in error envelope data, got %+v", envResp)
	}
}
