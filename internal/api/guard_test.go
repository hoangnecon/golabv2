package api_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hoangnecon/golabv2/internal/api"
	"github.com/hoangnecon/golabv2/internal/domain"
	"github.com/hoangnecon/golabv2/internal/service"
	"github.com/hoangnecon/golabv2/internal/session"
	"github.com/hoangnecon/golabv2/internal/store/sqlite"
)

type apiTestBrowser struct{}

func (apiTestBrowser) LaunchProfile(ctx context.Context, profile domain.Profile, initialURL string) (int, int, error) {
	return 8888, profile.CDPPort, nil
}
func (apiTestBrowser) StopProfile(ctx context.Context, profile domain.Profile) error { return nil }
func (apiTestBrowser) IsProfileRunning(ctx context.Context, cdpPort int) bool        { return true }
func (apiTestBrowser) ListTargets(ctx context.Context, cdpPort int) ([]domain.CDPTarget, error) {
	return []domain.CDPTarget{{ID: "tab-1", Type: "page", URL: "https://colab.research.google.com/drive/1AlphaNotebookID0001"}}, nil
}
func (apiTestBrowser) OpenOrFocusTab(ctx context.Context, cdpPort int, targetURL string, matchSubstr string) (domain.CDPTarget, error) {
	return domain.CDPTarget{ID: "tab-1", Type: "page", URL: targetURL}, nil
}
func (apiTestBrowser) FocusTab(ctx context.Context, cdpPort int, tabID string) error { return nil }
func (apiTestBrowser) CloseTab(ctx context.Context, cdpPort int, tabID string) error { return nil }
func (apiTestBrowser) ShutdownAdapter() error                                        { return nil }

type apiTestDriver struct{}

func (apiTestDriver) Close() error { return nil }
func (apiTestDriver) ProbeTabState(ctx context.Context) (*domain.TabProbe, error) {
	return &domain.TabProbe{
		URL:             "https://colab.research.google.com/drive/1AlphaNotebookID0001",
		Title:           "Alpha.ipynb",
		ConnectionState: domain.StateIdle,
		Permission:      domain.PermissionOwner,
		Identity:        domain.NotebookIdentity{EmailAddress: "owner@gmail.com", Permission: domain.PermissionOwner, Editable: true},
	}, nil
}
func (apiTestDriver) GetComputeUnits(ctx context.Context) (*domain.ComputeUnitsInfo, error) {
	return &domain.ComputeUnitsInfo{Balance: 15.25, FormattedBalance: "15.25", Tier: domain.TierPro}, nil
}
func (apiTestDriver) ListAccountVMSessions(ctx context.Context) ([]domain.ColabVMSession, error) {
	return []domain.ColabVMSession{}, nil
}
func (apiTestDriver) FetchKernelXHR(ctx context.Context, path string, opts domain.FetchOptions) (*domain.FetchResponse, error) {
	return &domain.FetchResponse{Status: 200, OK: true, Body: json.RawMessage(`{}`)}, nil
}
func (apiTestDriver) CallColabMcpTool(ctx context.Context, toolName string, args map[string]any) (json.RawMessage, error) {
	return json.RawMessage(`{"cells":[]}`), nil
}
func (apiTestDriver) ConnectRuntime(ctx context.Context) error                              { return nil }
func (apiTestDriver) DisconnectRuntime(ctx context.Context) error                           { return nil }
func (apiTestDriver) InterruptKernel(ctx context.Context) error                             { return nil }
func (apiTestDriver) RestartKernel(ctx context.Context) error                               { return nil }
func (apiTestDriver) GetAvailableAccelerators(ctx context.Context) (*domain.RuntimeConfigReport, error) {
	return &domain.RuntimeConfigReport{}, nil
}
func (apiTestDriver) SetAccelerator(ctx context.Context, value string, highRAM bool) error { return nil }
func (apiTestDriver) ExecPython(ctx context.Context, code string, preferSidecarWhenBusy bool) (*domain.PythonExecResult, error) {
	return &domain.PythonExecResult{}, nil
}
func (apiTestDriver) GetHardwareTelemetry(ctx context.Context) (*domain.HardwareTelemetry, error) {
	return &domain.HardwareTelemetry{}, nil
}
func (apiTestDriver) GetBuildInfo(ctx context.Context) (*domain.BuildInfo, error) {
	return &domain.BuildInfo{ReleaseTag: "2026-11"}, nil
}
func (apiTestDriver) CheckDriveStatus(ctx context.Context, allowKernelVerify bool) (*domain.DriveStatus, error) {
	return &domain.DriveStatus{Mounted: true, Status: "mounted"}, nil
}
func (apiTestDriver) MountDrive(ctx context.Context) (*domain.DriveMountResult, error) {
	return &domain.DriveMountResult{Mounted: true, Status: "mounted"}, nil
}
func (apiTestDriver) StartBackgroundJob(ctx context.Context, command string, cwd string) (*domain.BackgroundJob, error) {
	return &domain.BackgroundJob{}, nil
}
func (apiTestDriver) PollBackgroundJob(ctx context.Context, jobID string, tailLines int) (*domain.BackgroundJob, error) {
	return &domain.BackgroundJob{}, nil
}
func (apiTestDriver) KillBackgroundJob(ctx context.Context, jobID string) (*domain.BackgroundJob, error) {
	return &domain.BackgroundJob{}, nil
}
func (apiTestDriver) GetRunningCells(ctx context.Context) (*domain.RunningCellsReport, error) {
	return &domain.RunningCellsReport{}, nil
}
func (apiTestDriver) SaveNotebook(ctx context.Context) error { return nil }
func (apiTestDriver) RunAll(ctx context.Context) error       { return nil }
func (apiTestDriver) GetProxyURL(ctx context.Context, port int) (*domain.ProxyEndpoint, error) {
	return &domain.ProxyEndpoint{Port: port, URL: "https://8080.colab.dev"}, nil
}

type apiTestDriverFactory struct{}

func (apiTestDriverFactory) NewDriver(ctx context.Context, cdpPort int, target domain.CDPTarget) (domain.RuntimeDriver, error) {
	return apiTestDriver{}, nil
}

func newTestAPIServer(t *testing.T) (http.Handler, *sqlite.Store, *service.ActivityService, *session.EventBus) {
	t.Helper()
	tmp := t.TempDir()
	st, err := sqlite.Open(filepath.Join(tmp, "api_v1.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	bus := session.NewEventBus()
	browser := apiTestBrowser{}
	coord := session.NewCoordinator(st, browser, apiTestDriverFactory{}, bus, session.CoordinatorOptions{})
	t.Cleanup(func() { _ = coord.Close() })

	profSvc := service.NewProfileService(st, browser, coord, bus, filepath.Join(tmp, "profiles"))
	_, _ = profSvc.EnsureDefaultProfile(context.Background())
	projSvc := service.NewProjectService(st, coord, bus)
	sessSvc := service.NewSessionService(st, coord, bus)
	actSvc := service.NewActivityService(st, bus)
	wsSvc := service.NewWorkspaceService(st, browser, "v2.0.0-test", "http://127.0.0.1:9120/mcp")

	mcpStub := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"mcp":"ok"}`))
	})

	srv := api.NewServer(api.Dependencies{
		WorkspaceSvc: wsSvc,
		ProfileSvc:   profSvc,
		ProjectSvc:   projSvc,
		SessionSvc:   sessSvc,
		ActivitySvc:  actSvc,
		EventBus:     bus,
		MCPHandler:   mcpStub,
		Version:      "v2.0.0-test",
		MCPHTTPURL:   "http://127.0.0.1:9120/mcp",
	})
	return srv.Handler(), st, actSvc, bus
}

func TestGuardHostOriginAndMCPPassThrough(t *testing.T) {
	h, _, _, _ := newTestAPIServer(t)

	// 1. Valid loopback request without Origin (curl / MCP client)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	req.Host = "127.0.0.1:9120"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on /api/v1/health, got %d: %s", rec.Code, rec.Body.String())
	}

	// 2. Foreign Host (DNS rebinding) -> 403 Forbidden
	evilHost := httptest.NewRequest(http.MethodGet, "/api/v1/workspace", nil)
	evilHost.Host = "evil.example.com"
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, evilHost)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on DNS rebinding host, got %d", rec.Code)
	}

	// 3. Foreign Origin -> 403 Forbidden
	evilOrigin := httptest.NewRequest(http.MethodPost, "/api/v1/profiles", strings.NewReader(`{"name":"x"}`))
	evilOrigin.Host = "127.0.0.1:9120"
	evilOrigin.Header.Set("Origin", "https://evil.example.com")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, evilOrigin)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on foreign origin, got %d", rec.Code)
	}

	// 4. /mcp without Origin on loopback -> 200 OK
	mcpReq := httptest.NewRequest(http.MethodPost, "/mcp?token=glp_test", strings.NewReader(`{}`))
	mcpReq.Host = "127.0.0.1:9120"
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, mcpReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on /mcp without Origin, got %d", rec.Code)
	}
}

func TestV1EndpointsLifecycleLockConflictAndActivity(t *testing.T) {
	h, _, actSvc, _ := newTestAPIServer(t)

	doJSON := func(method, path, body string) *httptest.ResponseRecorder {
		var r *http.Request
		if body != "" {
			r = httptest.NewRequest(method, path, strings.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
		} else {
			r = httptest.NewRequest(method, path, nil)
		}
		r.Host = "127.0.0.1:9120"
		r.Header.Set("Origin", "http://127.0.0.1:9120")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	// 1. Create second profile
	wProf := doJSON(http.MethodPost, "/api/v1/profiles", `{"name":"Account 2","tier":"pro"}`)
	if wProf.Code != http.StatusCreated {
		t.Fatalf("create profile: %d %s", wProf.Code, wProf.Body.String())
	}
	var prof2 domain.Profile
	_ = json.Unmarshal(wProf.Body.Bytes(), &prof2)

	// 2. Create project -> returns project + auto-created owner session
	wProj := doJSON(http.MethodPost, "/api/v1/projects", `{"name":"Alpha","notebook_url":"https://colab.research.google.com/drive/1AlphaNotebookID0001","owner_profile_id":"default"}`)
	if wProj.Code != http.StatusCreated {
		t.Fatalf("create project: %d %s", wProj.Code, wProj.Body.String())
	}
	var createdProj struct {
		Project *domain.Project `json:"project"`
		Session *domain.Session `json:"session"`
	}
	_ = json.Unmarshal(wProj.Body.Bytes(), &createdProj)
	if createdProj.Project == nil || createdProj.Session == nil {
		t.Fatalf("expected project and initial session, got %s", wProj.Body.String())
	}

	// 3. Add Account 2 session
	wSess2 := doJSON(http.MethodPost, "/api/v1/sessions", fmt.Sprintf(`{"project_id":%q,"profile_id":%q}`, createdProj.Project.ID, prof2.ID))
	if wSess2.Code != http.StatusCreated {
		t.Fatalf("create session 2: %d %s", wSess2.Code, wSess2.Body.String())
	}
	var sess2 domain.Session
	_ = json.Unmarshal(wSess2.Body.Bytes(), &sess2)

	// 4. Lock session 1
	wLock1 := doJSON(http.MethodPost, "/api/v1/sessions/"+createdProj.Session.ID+"/lock", `{"force_switch":false,"open_tab":true}`)
	if wLock1.Code != http.StatusOK {
		t.Fatalf("lock s1: %d %s", wLock1.Code, wLock1.Body.String())
	}

	// 5. Lock session 2 with force_switch=false -> 409 Conflict with GOLAB_PROJECT_LOCKED_BY_OTHER_ACCOUNT
	wConflict := doJSON(http.MethodPost, "/api/v1/sessions/"+sess2.ID+"/lock", `{"force_switch":false}`)
	if wConflict.Code != http.StatusConflict || !strings.Contains(wConflict.Body.String(), domain.CodeProjectLockedByOtherAccount) {
		t.Fatalf("expected 409 GOLAB_PROJECT_LOCKED_BY_OTHER_ACCOUNT, got %d %s", wConflict.Code, wConflict.Body.String())
	}

	// 6. Switch Lock with force_switch=true -> 200 OK
	wSwitch := doJSON(http.MethodPost, "/api/v1/sessions/"+sess2.ID+"/lock", `{"force_switch":true,"open_tab":true}`)
	if wSwitch.Code != http.StatusOK {
		t.Fatalf("switch lock s2: %d %s", wSwitch.Code, wSwitch.Body.String())
	}

	// 7. Observe locked session
	wObs := doJSON(http.MethodGet, "/api/v1/sessions/"+sess2.ID+"/observe", "")
	if wObs.Code != http.StatusOK || !strings.Contains(wObs.Body.String(), `"connection_state":"idle"`) {
		t.Fatalf("observe session: %d %s", wObs.Code, wObs.Body.String())
	}

	// 8. Activity list + detail
	logged, err := actSvc.Append(context.Background(), &domain.ActivityRecord{
		ProjectID: createdProj.Project.ID,
		SessionID: sess2.ID,
		ProfileID: prof2.ID,
		ToolName:  "colab_get_cells",
		Status:    "success",
		LatencyMs: 12,
		Request:   &domain.Payload{Body: json.RawMessage(`{"start":0}`), Bytes: 11},
		Response:  &domain.Payload{Body: json.RawMessage(`{"status":"success"}`), Bytes: 20},
	})
	if err != nil {
		t.Fatalf("append activity: %v", err)
	}
	wActList := doJSON(http.MethodGet, "/api/v1/activity?project_id="+createdProj.Project.ID, "")
	if wActList.Code != http.StatusOK || !strings.Contains(wActList.Body.String(), "colab_get_cells") {
		t.Fatalf("list activity: %d %s", wActList.Code, wActList.Body.String())
	}
	wActGet := doJSON(http.MethodGet, fmt.Sprintf("/api/v1/activity/%d", logged.ID), "")
	if wActGet.Code != http.StatusOK || !strings.Contains(wActGet.Body.String(), `"start":0`) {
		t.Fatalf("get activity detail: %d %s", wActGet.Code, wActGet.Body.String())
	}
}

func TestSSERealTimeWorkspaceAndActivityEvents(t *testing.T) {
	h, _, actSvc, _ := newTestAPIServer(t)
	ts := httptest.NewServer(h)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/v1/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("connect SSE: %v", err)
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	readNextDataFrame := func() string {
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatalf("read SSE line: %v", err)
			}
			if strings.HasPrefix(line, "data: ") {
				return strings.TrimSpace(strings.TrimPrefix(line, "data: "))
			}
		}
	}

	// First frame MUST be workspace.snapshot
	firstFrame := readNextDataFrame()
	if !strings.Contains(firstFrame, `"type":"workspace.snapshot"`) {
		t.Fatalf("expected initial workspace.snapshot SSE frame, got %s", firstFrame)
	}

	// Append an audit activity and assert activity.appended arrives over SSE immediately
	go func() {
		time.Sleep(30 * time.Millisecond)
		_, _ = actSvc.Append(context.Background(), &domain.ActivityRecord{
			ToolName:  "colab_get_compute_units",
			Status:    "success",
			LatencyMs: 7,
		})
	}()

	secondFrame := readNextDataFrame()
	if !strings.Contains(secondFrame, `"type":"activity.appended"`) || !strings.Contains(secondFrame, "colab_get_compute_units") {
		t.Fatalf("expected live activity.appended SSE frame, got %s", secondFrame)
	}
}
