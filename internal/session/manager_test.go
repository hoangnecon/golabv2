package session_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hoangnecon/golabv2/internal/domain"
	"github.com/hoangnecon/golabv2/internal/session"
	"github.com/hoangnecon/golabv2/internal/store/sqlite"
)

type fakeBrowser struct {
	mu          sync.Mutex
	running     map[int]bool
	targets     map[int][]domain.CDPTarget
	closedTabs  []string
	nextTabSeq  int
}

func newFakeBrowser() *fakeBrowser {
	return &fakeBrowser{
		running: make(map[int]bool),
		targets: make(map[int][]domain.CDPTarget),
	}
}

func (b *fakeBrowser) LaunchProfile(ctx context.Context, profile domain.Profile, initialURL string) (int, int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.running[profile.CDPPort] = true
	return 7000 + profile.CDPPort, profile.CDPPort, nil
}

func (b *fakeBrowser) StopProfile(ctx context.Context, profile domain.Profile) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.running[profile.CDPPort] = false
	return nil
}

func (b *fakeBrowser) IsProfileRunning(ctx context.Context, cdpPort int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.running[cdpPort]
}

func (b *fakeBrowser) ListTargets(ctx context.Context, cdpPort int) ([]domain.CDPTarget, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	cp := make([]domain.CDPTarget, len(b.targets[cdpPort]))
	copy(cp, b.targets[cdpPort])
	return cp, nil
}

func (b *fakeBrowser) OpenOrFocusTab(ctx context.Context, cdpPort int, targetURL string, matchSubstr string) (domain.CDPTarget, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.running[cdpPort] = true
	for _, t := range b.targets[cdpPort] {
		if matchSubstr != "" && t.URL == targetURL {
			return t, nil
		}
	}
	b.nextTabSeq++
	tgt := domain.CDPTarget{
		ID:                   "tab-" + string(rune('a'+b.nextTabSeq)),
		Type:                 "page",
		Title:                "Notebook.ipynb",
		URL:                  targetURL,
		WebSocketDebuggerURL: "ws://127.0.0.1/devtools/page/mock",
	}
	b.targets[cdpPort] = append(b.targets[cdpPort], tgt)
	return tgt, nil
}

func (b *fakeBrowser) FocusTab(ctx context.Context, cdpPort int, tabID string) error { return nil }

func (b *fakeBrowser) CloseTab(ctx context.Context, cdpPort int, tabID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closedTabs = append(b.closedTabs, tabID)
	cur := b.targets[cdpPort]
	filtered := cur[:0]
	for _, t := range cur {
		if t.ID != tabID {
			filtered = append(filtered, t)
		}
	}
	b.targets[cdpPort] = filtered
	return nil
}

func (b *fakeBrowser) ShutdownAdapter() error { return nil }

type fakeDriver struct {
	mu               sync.Mutex
	email            string
	displayName      string
	perm             domain.PermissionLevel
	connState        domain.ConnectionState
	saveCalled       atomic.Int32
	probeCalls       atomic.Int32
	lastKernelVerify atomic.Bool
	probeDelay       time.Duration
}

func (d *fakeDriver) Close() error { return nil }

func (d *fakeDriver) ProbeTabState(ctx context.Context) (*domain.TabProbe, error) {
	if d.probeDelay > 0 {
		time.Sleep(d.probeDelay)
	}
	d.probeCalls.Add(1)
	d.mu.Lock()
	defer d.mu.Unlock()
	return &domain.TabProbe{
		URL:             "https://colab.research.google.com/drive/1abc",
		Title:           "Exp.ipynb",
		ConnectionState: d.connState,
		Permission:      d.perm,
		ConnectState:    "connected",
		KernelState:     string(d.connState),
		IsExecuting:     d.connState == domain.StateBusy,
		Identity: domain.NotebookIdentity{
			EmailAddress:    d.email,
			DisplayName:     d.displayName,
			IsAuthenticated: d.perm != domain.PermissionAuthRequired,
			Permission:      d.perm,
			Editable:        d.perm.CanWrite(),
			Saveable:        d.perm.CanWrite(),
			IsSoleAuthored:  d.perm == domain.PermissionOwner,
		},
	}, nil
}

func (d *fakeDriver) GetComputeUnits(ctx context.Context) (*domain.ComputeUnitsInfo, error) {
	return &domain.ComputeUnitsInfo{
		Balance:               15.25,
		ConsumptionRateHourly: 0.08,
		FormattedBalance:      "15.25",
		Tier:                  domain.TierPro,
		EligibleGPUs:          []string{"T4", "L4", "A100"},
	}, nil
}

func (d *fakeDriver) ListAccountVMSessions(ctx context.Context) ([]domain.ColabVMSession, error) {
	return []domain.ColabVMSession{{Title: "Exp.ipynb", AcceleratorModel: "T4"}}, nil
}

func (d *fakeDriver) FetchKernelXHR(ctx context.Context, path string, opts domain.FetchOptions) (*domain.FetchResponse, error) {
	return &domain.FetchResponse{Status: 200, OK: true, Body: json.RawMessage(`{}`)}, nil
}

func (d *fakeDriver) CallColabMcpTool(ctx context.Context, toolName string, args map[string]any) (json.RawMessage, error) {
	return json.RawMessage(`{"cells":[]}`), nil
}

func (d *fakeDriver) ConnectRuntime(ctx context.Context) error                              { return nil }
func (d *fakeDriver) DisconnectRuntime(ctx context.Context) error                           { return nil }
func (d *fakeDriver) InterruptKernel(ctx context.Context) error                             { return nil }
func (d *fakeDriver) RestartKernel(ctx context.Context) error                               { return nil }
func (d *fakeDriver) GetAvailableAccelerators(ctx context.Context) (*domain.RuntimeConfigReport, error) {
	return &domain.RuntimeConfigReport{}, nil
}
func (d *fakeDriver) SetAccelerator(ctx context.Context, value string, highRAM bool) error { return nil }
func (d *fakeDriver) ExecPython(ctx context.Context, code string, preferSidecarWhenBusy bool) (*domain.PythonExecResult, error) {
	return &domain.PythonExecResult{Stdout: "ok"}, nil
}
func (d *fakeDriver) GetHardwareTelemetry(ctx context.Context) (*domain.HardwareTelemetry, error) {
	return &domain.HardwareTelemetry{RAM: domain.RAMTelemetry{TotalGB: 12.7, UsedGB: 2.1}}, nil
}
func (d *fakeDriver) GetBuildInfo(ctx context.Context) (*domain.BuildInfo, error) {
	return &domain.BuildInfo{ReleaseTag: "2026-11"}, nil
}
func (d *fakeDriver) CheckDriveStatus(ctx context.Context, allowKernelVerify bool) (*domain.DriveStatus, error) {
	d.lastKernelVerify.Store(allowKernelVerify)
	return &domain.DriveStatus{Mounted: true, Status: "mounted", MountPath: "/content/drive", TotalGB: 100, UsedGB: 5, FreeGB: 95}, nil
}
func (d *fakeDriver) MountDrive(ctx context.Context) (*domain.DriveMountResult, error) {
	return &domain.DriveMountResult{Mounted: true, Status: "mounted"}, nil
}
func (d *fakeDriver) StartBackgroundJob(ctx context.Context, command string, cwd string) (*domain.BackgroundJob, error) {
	return &domain.BackgroundJob{JobID: "j1", Running: true}, nil
}
func (d *fakeDriver) PollBackgroundJob(ctx context.Context, jobID string, tailLines int) (*domain.BackgroundJob, error) {
	return &domain.BackgroundJob{JobID: jobID}, nil
}
func (d *fakeDriver) KillBackgroundJob(ctx context.Context, jobID string) (*domain.BackgroundJob, error) {
	return &domain.BackgroundJob{JobID: jobID}, nil
}
func (d *fakeDriver) GetRunningCells(ctx context.Context) (*domain.RunningCellsReport, error) {
	return &domain.RunningCellsReport{}, nil
}
func (d *fakeDriver) SaveNotebook(ctx context.Context) error {
	d.saveCalled.Add(1)
	return nil
}
func (d *fakeDriver) RunAll(ctx context.Context) error { return nil }
func (d *fakeDriver) GetProxyURL(ctx context.Context, port int) (*domain.ProxyEndpoint, error) {
	return &domain.ProxyEndpoint{Port: port, URL: "https://8080.colab.dev"}, nil
}

type fakeDriverFactory struct {
	mu      sync.Mutex
	byPort  map[int]*fakeDriver
}

func (f *fakeDriverFactory) NewDriver(ctx context.Context, cdpPort int, target domain.CDPTarget) (domain.RuntimeDriver, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if drv, ok := f.byPort[cdpPort]; ok {
		return drv, nil
	}
	return &fakeDriver{email: "default@gmail.com", perm: domain.PermissionOwner, connState: domain.StateIdle}, nil
}

func setupCoordinatorTest(t *testing.T) (*sqlite.Store, *fakeBrowser, *fakeDriverFactory, *session.EventBus, *session.Coordinator) {
	t.Helper()
	tmp := t.TempDir()
	st, err := sqlite.Open(filepath.Join(tmp, "state.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	if err := st.CreateProfile(context.Background(), &domain.Profile{
		ID:          "default",
		Name:        "Default Account",
		Tier:        domain.TierFree,
		UserDataDir: filepath.Join(tmp, "default-profile"),
		CDPPort:     9222,
	}); err != nil {
		t.Fatalf("seed default profile: %v", err)
	}

	fb := newFakeBrowser()
	df := &fakeDriverFactory{byPort: make(map[int]*fakeDriver)}
	bus := session.NewEventBus()
	coord := session.NewCoordinator(st, fb, df, bus, session.CoordinatorOptions{
		ObservationTTL: 2 * time.Second,
	})
	t.Cleanup(func() { _ = coord.Close() })
	return st, fb, df, bus, coord
}

func TestAcquireLock_ConflictAndForceSwitchFlushesSaveAndClosesOldTab(t *testing.T) {
	st, fb, df, bus, coord := setupCoordinatorTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eventsCh := bus.Subscribe(ctx)

	acc2 := &domain.Profile{ID: "acc2", Name: "Account 2", Tier: domain.TierPro, UserDataDir: filepath.Join(t.TempDir(), "p2"), CDPPort: 9223}
	if err := st.CreateProfile(ctx, acc2); err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}

	drv1 := &fakeDriver{email: "owner1@gmail.com", displayName: "Owner 1", perm: domain.PermissionOwner, connState: domain.StateIdle}
	drv2 := &fakeDriver{email: "editor2@gmail.com", displayName: "Editor 2", perm: domain.PermissionEditor, connState: domain.StateIdle}
	df.byPort[9222] = drv1
	df.byPort[9223] = drv2

	proj := &domain.Project{ID: "proj-a", Name: "Project Alpha", NotebookURL: "https://colab.research.google.com/drive/1AlphaNotebookID0001", OwnerProfileID: "default"}
	if err := st.CreateProject(ctx, proj); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	s1 := &domain.Session{ID: "sess-1", ProjectID: proj.ID, ProfileID: "default", Name: "Alpha - Acc1"}
	s2 := &domain.Session{ID: "sess-2", ProjectID: proj.ID, ProfileID: "acc2", Name: "Alpha - Acc2"}
	if err := st.CreateSession(ctx, s1); err != nil {
		t.Fatalf("CreateSession s1: %v", err)
	}
	if err := st.CreateSession(ctx, s2); err != nil {
		t.Fatalf("CreateSession s2: %v", err)
	}

	// Lock Account 1
	locked1, err := coord.AcquireLock(ctx, s1.ID, false, true)
	if err != nil {
		t.Fatalf("AcquireLock s1: %v", err)
	}
	if !locked1.IsLocked || locked1.TabID == "" || locked1.Permission != domain.PermissionOwner {
		t.Fatalf("unexpected locked1: %+v", locked1)
	}
	oldTabID := locked1.TabID

	// Attempt locking Account 2 without forceSwitch -> 409 GOLAB_PROJECT_LOCKED_BY_OTHER_ACCOUNT
	_, err = coord.AcquireLock(ctx, s2.ID, false, true)
	var appErr *domain.AppError
	if !errors.As(err, &appErr) || appErr.Code != domain.CodeProjectLockedByOtherAccount {
		t.Fatalf("expected GOLAB_PROJECT_LOCKED_BY_OTHER_ACCOUNT, got %v", err)
	}

	// Switch Lock with forceSwitch=true -> flushes SaveNotebook on Account 1 & closes Account 1's tab!
	locked2, err := coord.AcquireLock(ctx, s2.ID, true, true)
	if err != nil {
		t.Fatalf("AcquireLock s2 forceSwitch: %v", err)
	}
	if !locked2.IsLocked || locked2.Permission != domain.PermissionEditor {
		t.Fatalf("unexpected locked2: %+v", locked2)
	}
	if drv1.saveCalled.Load() != 1 {
		t.Fatalf("expected SaveNotebook called once on Account 1 before switch, got %d", drv1.saveCalled.Load())
	}
	fb.mu.Lock()
	closed := append([]string(nil), fb.closedTabs...)
	fb.mu.Unlock()
	if len(closed) != 1 || closed[0] != oldTabID {
		t.Fatalf("expected old tab %q closed on forceSwitch, got %v", oldTabID, closed)
	}

	// Verify profile email auto-synced
	p2, _ := st.GetProfile(ctx, "acc2")
	if p2.AccountEmail != "editor2@gmail.com" {
		t.Fatalf("expected profile email synced to editor2@gmail.com, got %q", p2.AccountEmail)
	}

	// Verify SSE events emitted
	select {
	case <-eventsCh:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("expected SSE workspace events emitted on lock switch")
	}
}

func TestResolveMCPTarget_PriorityFailoverMultiProjectAndPermissions(t *testing.T) {
	st, _, df, _, coord := setupCoordinatorTest(t)
	ctx := context.Background()

	acc2 := &domain.Profile{ID: "acc2", Name: "Account 2", Tier: domain.TierPro, UserDataDir: filepath.Join(t.TempDir(), "p2"), CDPPort: 9223}
	_ = st.CreateProfile(ctx, acc2)

	drv1 := &fakeDriver{email: "acc1@gmail.com", perm: domain.PermissionOwner, connState: domain.StateIdle}
	drv2 := &fakeDriver{email: "acc2@gmail.com", perm: domain.PermissionViewOnly, connState: domain.StateIdle}
	df.byPort[9222] = drv1
	df.byPort[9223] = drv2

	projA := &domain.Project{ID: "proj-a", Name: "Alpha", NotebookURL: "https://colab.research.google.com/drive/1AlphaNotebookID0001", OwnerProfileID: "default"}
	projB := &domain.Project{ID: "proj-b", Name: "Beta", NotebookURL: "https://colab.research.google.com/drive/1BetaNotebookID00002", OwnerProfileID: "acc2"}
	_ = st.CreateProject(ctx, projA)
	_ = st.CreateProject(ctx, projB)

	sA1 := &domain.Session{ID: "sa1", ProjectID: projA.ID, ProfileID: "default", Name: "A-1"}
	sA2 := &domain.Session{ID: "sa2", ProjectID: projA.ID, ProfileID: "acc2", Name: "A-2"}
	sB2 := &domain.Session{ID: "sb2", ProjectID: projB.ID, ProfileID: "acc2", Name: "B-2"}
	_ = st.CreateSession(ctx, sA1)
	_ = st.CreateSession(ctx, sA2)
	_ = st.CreateSession(ctx, sB2)

	// 1. Unlocked project token -> GOLAB_PROJECT_UNLOCKED
	_, err := coord.ResolveMCPTarget(ctx, projA.ProjectToken, "", false)
	var appErr *domain.AppError
	if !errors.As(err, &appErr) || appErr.Code != domain.CodeProjectUnlocked {
		t.Fatalf("expected GOLAB_PROJECT_UNLOCKED, got %v", err)
	}

	// 2. Zero locked projects + no token -> GOLAB_NO_LOCKED_SESSION
	_, err = coord.ResolveMCPTarget(ctx, "", "", false)
	if !errors.As(err, &appErr) || appErr.Code != domain.CodeNoLockedSession {
		t.Fatalf("expected GOLAB_NO_LOCKED_SESSION, got %v", err)
	}

	// 3. Lock Project A (Account 1): no-token fallback succeeds when 1 project is locked
	if _, err := coord.AcquireLock(ctx, sA1.ID, false, true); err != nil {
		t.Fatalf("AcquireLock sA1: %v", err)
	}
	resSingle, err := coord.ResolveMCPTarget(ctx, "", "", true)
	if err != nil || resSingle.Session.ID != sA1.ID {
		t.Fatalf("expected single locked project resolution to sa1, got %+v, err=%v", resSingle, err)
	}

	// Unlocked session token sA2 -> GOLAB_SESSION_NOT_LOCKED
	_, err = coord.ResolveMCPTarget(ctx, sA2.SessionToken, "", false)
	if !errors.As(err, &appErr) || appErr.Code != domain.CodeSessionNotLocked {
		t.Fatalf("expected GOLAB_SESSION_NOT_LOCKED, got %v", err)
	}

	// 4. Lock Project B concurrently -> parallel multi-project locks work!
	if _, err := coord.AcquireLock(ctx, sB2.ID, false, true); err != nil {
		t.Fatalf("AcquireLock sB2: %v", err)
	}

	// Now 2 projects are locked: no-token call MUST fail deterministically with GOLAB_AMBIGUOUS_MULTI_PROJECT_SESSION!
	_, err = coord.ResolveMCPTarget(ctx, "", "", false)
	if !errors.As(err, &appErr) || appErr.Code != domain.CodeAmbiguousMultiProjectSession {
		t.Fatalf("expected GOLAB_AMBIGUOUS_MULTI_PROJECT_SESSION, got %v", err)
	}

	// Project tokens glp_A and glp_B resolve deterministically to their respective sessions
	tA, err := coord.ResolveMCPTarget(ctx, projA.ProjectToken, "", true)
	if err != nil || tA.Session.ID != sA1.ID {
		t.Fatalf("expected glp_A -> sa1, got %+v, err=%v", tA, err)
	}

	// sB2 has view_only permission: read succeeds (requireWrite=false), write fails with GOLAB_PERMISSION_READ_ONLY!
	tBRead, err := coord.ResolveMCPTarget(ctx, projB.ProjectToken, "", false)
	if err != nil || tBRead.Session.ID != sB2.ID {
		t.Fatalf("expected glp_B read -> sb2, got %+v, err=%v", tBRead, err)
	}
	_, err = coord.ResolveMCPTarget(ctx, projB.ProjectToken, "", true)
	if !errors.As(err, &appErr) || appErr.Code != domain.CodePermissionReadOnly {
		t.Fatalf("expected GOLAB_PERMISSION_READ_ONLY on view_only write, got %v", err)
	}

	// 5. Switch Project A lock from Account 1 -> Account 2: same glp_A ProjectToken seamlessly fails over to sA2!
	drv2.mu.Lock()
	drv2.perm = domain.PermissionEditor
	drv2.mu.Unlock()
	if _, err := coord.AcquireLock(ctx, sA2.ID, true, true); err != nil {
		t.Fatalf("AcquireLock sA2 forceSwitch: %v", err)
	}
	tAFailover, err := coord.ResolveMCPTarget(ctx, projA.ProjectToken, "", true)
	if err != nil || tAFailover.Session.ID != sA2.ID {
		t.Fatalf("expected seamless glp_A failover -> sa2, got %+v, err=%v", tAFailover, err)
	}
}

func TestObserveSession_SingleflightAndBusySafe(t *testing.T) {
	st, _, df, _, coord := setupCoordinatorTest(t)
	ctx := context.Background()

	drv := &fakeDriver{
		email:      "busy@gmail.com",
		perm:       domain.PermissionOwner,
		connState:  domain.StateBusy,
		probeDelay: 40 * time.Millisecond,
	}
	df.byPort[9222] = drv

	proj := &domain.Project{ID: "proj-busy", Name: "Training", NotebookURL: "https://colab.research.google.com/drive/1BusyNotebookID00001", OwnerProfileID: "default"}
	_ = st.CreateProject(ctx, proj)
	sess := &domain.Session{ID: "sess-busy", ProjectID: proj.ID, ProfileID: "default", Name: "Training - Acc1"}
	_ = st.CreateSession(ctx, sess)

	if _, err := coord.AcquireLock(ctx, sess.ID, false, true); err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	drv.probeCalls.Store(0)

	const callers = 10
	var wg sync.WaitGroup
	wg.Add(callers)
	for i := 0; i < callers; i++ {
		go func() {
			defer wg.Done()
			snap, err := coord.ObserveSession(ctx, sess.ID)
			if err != nil {
				t.Errorf("ObserveSession: %v", err)
				return
			}
			if snap.ConnectionState != domain.StateBusy {
				t.Errorf("expected busy state, got %s", snap.ConnectionState)
			}
		}()
	}
	wg.Wait()

	if got := drv.probeCalls.Load(); got != 1 {
		t.Fatalf("expected singleflight to coalesce 10 concurrent ObserveSession calls into 1 CDP probe, got %d", got)
	}
	if drv.lastKernelVerify.Load() {
		t.Fatalf("expected CheckDriveStatus(allowKernelVerify=false) when kernel is busy!")
	}
}
