package session

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/hoangnecon/golabv2/internal/domain"
)

// CoordinatorOptions configures cache TTLs and reconciler intervals.
type CoordinatorOptions struct {
	ObservationTTL     time.Duration
	ReconcileInterval  time.Duration
}

type attachedDriver struct {
	sessionID string
	cdpPort   int
	tabID     string
	wsURL     string
	driver    domain.RuntimeDriver
}

type cachedObservation struct {
	snap      *domain.ObservationSnapshot
	expiresAt time.Time
}

type inflightObservation struct {
	done chan struct{}
	snap *domain.ObservationSnapshot
	err  error
}

// Coordinator implements domain.SessionCoordinator importing ONLY internal/domain.
type Coordinator struct {
	store   domain.Store
	browser domain.BrowserManager
	drivers domain.DriverFactory
	bus     domain.EventBus
	opts    CoordinatorOptions

	projMu sync.Mutex
	byProj map[string]*sync.Mutex

	drvMu   sync.Mutex
	active  map[string]*attachedDriver // sessionID -> attachedDriver

	obsMu      sync.Mutex
	obsCache   map[string]cachedObservation
	obsFlight  map[string]*inflightObservation
	driveCache map[string]*domain.DriveStatus // sessionID -> last FUSE quota snapshot

	stopOnce sync.Once
	stopCh   chan struct{}
}

// NewCoordinator constructs a SessionCoordinator over pure domain ports.
func NewCoordinator(
	store domain.Store,
	browser domain.BrowserManager,
	drivers domain.DriverFactory,
	bus domain.EventBus,
	opts CoordinatorOptions,
) *Coordinator {
	if opts.ObservationTTL <= 0 {
		opts.ObservationTTL = 2 * time.Second
	}
	if opts.ReconcileInterval <= 0 {
		opts.ReconcileInterval = 5 * time.Second
	}
	return &Coordinator{
		store:      store,
		browser:    browser,
		drivers:    drivers,
		bus:        bus,
		opts:       opts,
		byProj:     make(map[string]*sync.Mutex),
		active:     make(map[string]*attachedDriver),
		obsCache:   make(map[string]cachedObservation),
		obsFlight:  make(map[string]*inflightObservation),
		driveCache: make(map[string]*domain.DriveStatus),
		stopCh:     make(chan struct{}),
	}
}

func (c *Coordinator) projectMutex(projectID string) *sync.Mutex {
	c.projMu.Lock()
	defer c.projMu.Unlock()
	m, ok := c.byProj[projectID]
	if !ok {
		m = &sync.Mutex{}
		c.byProj[projectID] = m
	}
	return m
}

func (c *Coordinator) publish(evtType domain.EventType, data any) {
	if c.bus == nil {
		return
	}
	c.bus.Publish(domain.WorkspaceEvent{
		Type:      evtType,
		Timestamp: time.Now().UTC(),
		Data:      data,
	})
}

func (c *Coordinator) invalidateObservation(sessionID string) {
	c.obsMu.Lock()
	delete(c.obsCache, sessionID)
	c.obsMu.Unlock()
}

// AcquireLock enforces Per-Project Single-Account Exclusive Lock.
// If forceSwitch is true and another session S_old holds the project's lock, S_old's notebook is saved
// and S_old's tab is closed before atomically transferring locked_session_id to sessionID.
func (c *Coordinator) AcquireLock(ctx context.Context, sessionID string, forceSwitch bool, openTab bool) (*domain.Session, error) {
	sess, err := c.store.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	pm := c.projectMutex(sess.ProjectID)
	pm.Lock()
	defer pm.Unlock()

	proj, err := c.store.GetProject(ctx, sess.ProjectID)
	if err != nil {
		return nil, err
	}

	// If another session currently holds the lock on this Project:
	if proj.LockedSessionID != "" && proj.LockedSessionID != sessionID {
		oldSess, _ := c.store.GetSession(ctx, proj.LockedSessionID)
		if !forceSwitch {
			holder := proj.LockedSessionID
			if oldSess != nil && oldSess.Name != "" {
				holder = fmt.Sprintf("%s (%s)", oldSess.Name, oldSess.ID)
			}
			return nil, domain.NewAppError(
				domain.CodeProjectLockedByOtherAccount,
				fmt.Sprintf("project %q is currently locked by another account session: %s", proj.Name, holder),
				"Pass force_switch=true ('Switch Lock' in GoLab UI) to flush-save and close the previous account's tab first.",
				http.StatusConflict,
			)
		}
		if oldSess != nil {
			c.flushAndDetachSession(ctx, oldSess, true)
		}
	}

	prev, updatedProj, lockedSess, err := c.store.AcquireProjectLock(ctx, sessionID, forceSwitch)
	if err != nil {
		return nil, err
	}
	c.invalidateObservation(sessionID)
	if prev != nil {
		c.invalidateObservation(prev.ID)
		if refreshedPrev, errP := c.store.GetSession(ctx, prev.ID); errP == nil {
			c.publish(domain.EventSessionUpdated, refreshedPrev)
		}
	}

	if openTab {
		if opened, errOpen := c.ensureTabAndSync(ctx, lockedSess); errOpen == nil && opened != nil {
			lockedSess = opened
		}
	}

	c.publish(domain.EventProjectUpdated, updatedProj)
	c.publish(domain.EventSessionUpdated, lockedSess)
	return lockedSess, nil
}

// ReleaseLock flushes save on the currently locked session (if editable) and clears projects.locked_session_id.
func (c *Coordinator) ReleaseLock(ctx context.Context, projectID string) error {
	pm := c.projectMutex(projectID)
	pm.Lock()
	defer pm.Unlock()

	proj, err := c.store.GetProject(ctx, projectID)
	if err != nil {
		return err
	}
	oldID := proj.LockedSessionID
	if oldID != "" {
		if oldSess, errS := c.store.GetSession(ctx, oldID); errS == nil && oldSess != nil {
			c.flushAndDetachSession(ctx, oldSess, false)
		}
	}
	if err := c.store.ReleaseProjectLock(ctx, projectID); err != nil {
		return err
	}
	if oldID != "" {
		c.invalidateObservation(oldID)
		if updatedOld, errS := c.store.GetSession(ctx, oldID); errS == nil {
			c.publish(domain.EventSessionUpdated, updatedOld)
		}
	}
	if updatedProj, errP := c.store.GetProject(ctx, projectID); errP == nil {
		c.publish(domain.EventProjectUpdated, updatedProj)
	}
	return nil
}

// ReleaseSessionLock unlocks the project if and only if sessionID currently holds its lock.
func (c *Coordinator) ReleaseSessionLock(ctx context.Context, sessionID string) error {
	sess, err := c.store.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}
	proj, err := c.store.GetProject(ctx, sess.ProjectID)
	if err != nil {
		return err
	}
	if proj.LockedSessionID != sessionID {
		return nil
	}
	return c.ReleaseLock(ctx, proj.ID)
}

func (c *Coordinator) flushAndDetachSession(ctx context.Context, sess *domain.Session, closeTab bool) {
	prof, _ := c.store.GetProfile(ctx, sess.ProfileID)
	if prof != nil && c.browser.IsProfileRunning(ctx, prof.CDPPort) {
		resolvedTabID := sess.TabID
		if drv, target, err := c.getOrConnectDriver(ctx, sess, prof, false); err == nil && drv != nil {
			if resolvedTabID == "" {
				resolvedTabID = target.ID
			}
			if sess.Permission.CanWrite() || sess.Permission == domain.PermissionUnknown {
				saveCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
				_ = drv.SaveNotebook(saveCtx)
				cancel()
			}
		}
		if closeTab && resolvedTabID != "" {
			_ = c.browser.CloseTab(ctx, prof.CDPPort, resolvedTabID)
			_ = c.store.UpdateSessionState(ctx, sess.ID, "", sess.Permission, domain.StateTabClosed)
		}
	}
	c.closeCachedDriver(sess.ID)
}

// DetachSession closes any cached CDP driver and optionally closes the browser tab.
func (c *Coordinator) DetachSession(ctx context.Context, sessionID string, closeTab bool) error {
	sess, err := c.store.GetSession(ctx, sessionID)
	if err != nil {
		c.closeCachedDriver(sessionID)
		return err
	}
	c.flushAndDetachSession(ctx, sess, closeTab)
	c.invalidateObservation(sessionID)
	return nil
}

func (c *Coordinator) closeCachedDriver(sessionID string) {
	c.drvMu.Lock()
	ad, ok := c.active[sessionID]
	if ok {
		delete(c.active, sessionID)
	}
	c.drvMu.Unlock()
	if ok && ad != nil && ad.driver != nil {
		_ = ad.driver.Close()
	}
}

// OpenSessionTab launches the account's Chrome profile if needed, opens or focuses the notebook tab,
// probes identity/permissions/CCU, and persists updated state.
func (c *Coordinator) OpenSessionTab(ctx context.Context, sessionID string) (*domain.Session, error) {
	sess, err := c.store.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	updated, err := c.ensureTabAndSync(ctx, sess)
	if err != nil {
		return nil, err
	}
	c.invalidateObservation(sessionID)
	c.publish(domain.EventSessionUpdated, updated)
	return updated, nil
}

func (c *Coordinator) ensureTabAndSync(ctx context.Context, sess *domain.Session) (*domain.Session, error) {
	prof, err := c.store.GetProfile(ctx, sess.ProfileID)
	if err != nil {
		return nil, err
	}

	if !c.browser.IsProfileRunning(ctx, prof.CDPPort) {
		pid, actualPort, errLaunch := c.browser.LaunchProfile(ctx, *prof, sess.NotebookURL)
		if errLaunch != nil {
			return nil, errLaunch
		}
		prof.ChromePID = pid
		prof.CDPPort = actualPort
		prof.IsRunning = true
		_ = c.store.SetProfileChromePID(ctx, prof.ID, pid, actualPort)
	}

	drv, target, err := c.getOrConnectDriver(ctx, sess, prof, true)
	if err != nil {
		return nil, err
	}
	_ = c.browser.FocusTab(ctx, prof.CDPPort, target.ID)

	probeCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	probe, errProbe := drv.ProbeTabState(probeCtx)
	perm := sess.Permission
	connState := domain.StateRuntimeDisconnected
	if errProbe == nil && probe != nil {
		if probe.Permission != "" {
			perm = probe.Permission
		}
		if probe.ConnectionState != "" {
			connState = probe.ConnectionState
		}
		c.syncProfileFromProbeAndCCU(probeCtx, prof, drv, probe)
	}

	_ = c.store.UpdateSessionState(ctx, sess.ID, target.ID, perm, connState)
	return c.store.GetSession(ctx, sess.ID)
}

func (c *Coordinator) syncProfileFromProbeAndCCU(ctx context.Context, prof *domain.Profile, drv domain.RuntimeDriver, probe *domain.TabProbe) {
	email := prof.AccountEmail
	disp := prof.DisplayName
	if probe != nil {
		if probe.Identity.EmailAddress != "" {
			email = probe.Identity.EmailAddress
		}
		if probe.Identity.DisplayName != "" {
			disp = probe.Identity.DisplayName
		}
	}
	tier := prof.Tier
	bal := prof.CCUBalance
	burn := prof.CCUBurnRate
	gpus := prof.EligibleGPUs
	tpus := prof.EligibleTPUs

	if ccu, err := drv.GetComputeUnits(ctx); err == nil && ccu != nil {
		if ccu.Tier != "" {
			tier = ccu.Tier
		}
		bal = ccu.Balance
		burn = ccu.ConsumptionRateHourly
		gpus = ccu.EligibleGPUs
		tpus = ccu.EligibleTPUs
	}

	_ = c.store.UpdateProfileRuntimeTelemetry(ctx, prof.ID, email, disp, tier, bal, burn, gpus, tpus)
	if updatedProf, err := c.store.GetProfile(ctx, prof.ID); err == nil && updatedProf != nil {
		updatedProf.IsRunning = true
		c.publish(domain.EventProfileUpdated, updatedProf)
	}
}

func (c *Coordinator) getOrConnectDriver(ctx context.Context, sess *domain.Session, prof *domain.Profile, openIfMissing bool) (domain.RuntimeDriver, domain.CDPTarget, error) {
	if !c.browser.IsProfileRunning(ctx, prof.CDPPort) {
		if !openIfMissing {
			return nil, domain.CDPTarget{}, domain.NewAppError(
				domain.CodeBrowserStopped,
				fmt.Sprintf("Chrome browser for account %q is stopped", prof.Name),
				"Launch the browser in GoLab UI or call colab_open_session_tab.",
				http.StatusConflict,
			)
		}
		pid, actualPort, err := c.browser.LaunchProfile(ctx, *prof, sess.NotebookURL)
		if err != nil {
			return nil, domain.CDPTarget{}, err
		}
		prof.ChromePID = pid
		prof.CDPPort = actualPort
		_ = c.store.SetProfileChromePID(ctx, prof.ID, pid, actualPort)
	}

	targets, err := c.browser.ListTargets(ctx, prof.CDPPort)
	if err != nil {
		return nil, domain.CDPTarget{}, err
	}

	var matched *domain.CDPTarget
	if sess.TabID != "" {
		for i := range targets {
			if targets[i].ID == sess.TabID && targets[i].Type == "page" {
				matched = &targets[i]
				break
			}
		}
	}
	if matched == nil {
		for i := range targets {
			t := &targets[i]
			if t.Type == "page" && (strings.Contains(t.URL, sess.NotebookID) || (sess.NotebookURL != "" && strings.Contains(t.URL, sess.NotebookURL))) {
				matched = t
				break
			}
		}
	}

	if matched == nil {
		if !openIfMissing {
			c.closeCachedDriver(sess.ID)
			return nil, domain.CDPTarget{}, domain.NewAppError(
				domain.CodeTabClosed,
				fmt.Sprintf("Notebook tab for session %q is closed", sess.Name),
				"Call colab_open_session_tab or click 'Open Browser Tab' in GoLab UI.",
				http.StatusConflict,
			)
		}
		opened, errOpen := c.browser.OpenOrFocusTab(ctx, prof.CDPPort, sess.NotebookURL, sess.NotebookID)
		if errOpen != nil {
			return nil, domain.CDPTarget{}, errOpen
		}
		matched = &opened
	}

	c.drvMu.Lock()
	existing, ok := c.active[sess.ID]
	if ok && existing.tabID == matched.ID && existing.cdpPort == prof.CDPPort && existing.driver != nil {
		drv := existing.driver
		c.drvMu.Unlock()
		return drv, *matched, nil
	}
	c.drvMu.Unlock()

	if ok && existing != nil && existing.driver != nil {
		_ = existing.driver.Close()
	}

	drv, err := c.drivers.NewDriver(ctx, prof.CDPPort, *matched)
	if err != nil {
		return nil, domain.CDPTarget{}, err
	}

	c.drvMu.Lock()
	c.active[sess.ID] = &attachedDriver{
		sessionID: sess.ID,
		cdpPort:   prof.CDPPort,
		tabID:     matched.ID,
		wsURL:     matched.WebSocketDebuggerURL,
		driver:    drv,
	}
	c.drvMu.Unlock()
	return drv, *matched, nil
}

// ResolveMCPTarget implements Spec §4.2 deterministic MCP Session Token routing & permission guardrails.
func (c *Coordinator) ResolveMCPTarget(ctx context.Context, connToken string, explicitSessionID string, requireWrite bool) (*domain.ResolvedTarget, error) {
	connToken = strings.TrimSpace(connToken)
	explicitSessionID = strings.TrimSpace(explicitSessionID)

	var sess *domain.Session
	var proj *domain.Project

	switch {
	case explicitSessionID != "":
		var err error
		if strings.HasPrefix(explicitSessionID, "gls_") {
			sess, err = c.store.GetSessionByToken(ctx, explicitSessionID)
		} else {
			sess, err = c.store.GetSession(ctx, explicitSessionID)
		}
		if err != nil {
			return nil, domain.NewAppError(domain.CodeNotFound, fmt.Sprintf("session %q not found", explicitSessionID), "Call colab_list_all_sessions to inspect valid session IDs.", http.StatusNotFound)
		}
		proj, err = c.store.GetProject(ctx, sess.ProjectID)
		if err != nil {
			return nil, err
		}
		if proj.LockedSessionID != sess.ID {
			return nil, domain.NewAppError(
				domain.CodeSessionNotLocked,
				fmt.Sprintf("session %q (%s) does not hold the exclusive lock for project %q", sess.Name, sess.ID, proj.Name),
				"Lock this account session in GoLab UI before running MCP tools against it.",
				http.StatusConflict,
			)
		}

	case strings.HasPrefix(connToken, "glp_"):
		var err error
		proj, err = c.store.GetProjectByToken(ctx, connToken)
		if err != nil {
			return nil, domain.NewAppError(domain.CodeInvalidToken, "invalid or rotated project token (glp_...)", "Copy the current Project Token from GoLab UI.", http.StatusUnauthorized)
		}
		if proj.LockedSessionID == "" {
			return nil, domain.NewAppError(
				domain.CodeProjectUnlocked,
				fmt.Sprintf("project %q has no locked account session", proj.Name),
				"Open GoLab UI and lock an account session on this project first.",
				http.StatusConflict,
			)
		}
		sess, err = c.store.GetSession(ctx, proj.LockedSessionID)
		if err != nil {
			return nil, err
		}

	case strings.HasPrefix(connToken, "gls_"):
		var err error
		sess, err = c.store.GetSessionByToken(ctx, connToken)
		if err != nil {
			return nil, domain.NewAppError(domain.CodeInvalidToken, "invalid or rotated session token (gls_...)", "Copy the current Session Token from GoLab UI.", http.StatusUnauthorized)
		}
		proj, err = c.store.GetProject(ctx, sess.ProjectID)
		if err != nil {
			return nil, err
		}
		if proj.LockedSessionID != sess.ID {
			return nil, domain.NewAppError(
				domain.CodeSessionNotLocked,
				fmt.Sprintf("session %q is not currently locked on project %q", sess.Name, proj.Name),
				"Lock this session in GoLab UI or switch to the Project Token (glp_...) for automatic failover.",
				http.StatusConflict,
			)
		}

	case connToken != "":
		return nil, domain.NewAppError(
			domain.CodeInvalidToken,
			"unrecognized MCP token prefix (expected glp_... or gls_...)",
			"Copy a valid Project Token (glp_...) or Session Token (gls_...) from GoLab UI.",
			http.StatusUnauthorized,
		)

	default:
		locked, err := c.store.ListLockedSessions(ctx)
		if err != nil {
			return nil, err
		}
		if len(locked) == 0 {
			return nil, domain.NewAppError(
				domain.CodeNoLockedSession,
				"no project currently has a locked account session",
				"Open GoLab UI (http://127.0.0.1:9120), select a Project, and lock an Account Session.",
				http.StatusConflict,
			)
		}
		if len(locked) > 1 {
			return nil, domain.NewAppError(
				domain.CodeAmbiguousMultiProjectSession,
				fmt.Sprintf("%d projects are currently locked concurrently; refusing to guess target session without a token", len(locked)),
				"Configure GOLAB_SESSION_TOKEN or ?token=glp_... for this IDE agent stream.",
				http.StatusConflict,
			)
		}
		sess = locked[0]
		proj, err = c.store.GetProject(ctx, sess.ProjectID)
		if err != nil {
			return nil, err
		}
	}

	prof, err := c.store.GetProfile(ctx, sess.ProfileID)
	if err != nil {
		return nil, err
	}

	// Ensure Chrome & tab are attached (locked sessions auto-open/reattach their canonical notebook tab)
	drv, target, err := c.getOrConnectDriver(ctx, sess, prof, true)
	if err != nil {
		return nil, err
	}

	probe, errProbe := drv.ProbeTabState(ctx)
	if errProbe == nil && probe != nil {
		sess.Permission = probe.Permission
		sess.ConnectionState = probe.ConnectionState
		sess.TabID = target.ID
		_ = c.store.UpdateSessionState(ctx, sess.ID, target.ID, probe.Permission, probe.ConnectionState)
		c.syncProfileFromProbeAndCCU(ctx, prof, drv, probe)
	}

	if err := enforcePermissionGuardrail(sess.Permission, requireWrite); err != nil {
		return nil, err
	}

	return &domain.ResolvedTarget{
		Project: proj,
		Session: sess,
		Profile: prof,
		Driver:  drv,
	}, nil
}

func enforcePermissionGuardrail(perm domain.PermissionLevel, requireWrite bool) error {
	switch perm {
	case domain.PermissionAuthRequired:
		return domain.NewAppError(
			domain.CodeAuthRequired,
			"Google Account sign-in is required in this Chrome profile",
			"Click 'Open Browser Tab' in GoLab UI and sign into Google.",
			http.StatusForbidden,
		)
	case domain.PermissionDenied:
		return domain.NewAppError(
			domain.CodePermissionDenied,
			"this Google Account does not have access to the notebook on Google Drive",
			"Share the notebook with Editor access to this account, or switch the Project Lock to another account.",
			http.StatusForbidden,
		)
	case domain.PermissionViewOnly, domain.PermissionCommenter:
		if requireWrite {
			return domain.NewAppError(
				domain.CodePermissionReadOnly,
				fmt.Sprintf("session has %q permission; write/mutation operations are blocked", perm),
				"Grant Editor access on Google Drive or switch lock to an Editor/Owner account.",
				http.StatusForbidden,
			)
		}
	}
	return nil
}

// ObserveSession returns a singleflight-coalesced, 2s-cached read-only snapshot of sessionID.
// CRITICAL: When connection_state == "busy", it NEVER executes blocking kernel Python calls!
func (c *Coordinator) ObserveSession(ctx context.Context, sessionID string) (*domain.ObservationSnapshot, error) {
	now := time.Now()
	c.obsMu.Lock()
	if cached, ok := c.obsCache[sessionID]; ok && now.Before(cached.expiresAt) {
		snap := cached.snap
		c.obsMu.Unlock()
		return snap, nil
	}
	if flight, ok := c.obsFlight[sessionID]; ok {
		c.obsMu.Unlock()
		select {
		case <-flight.done:
			return flight.snap, flight.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	flight := &inflightObservation{done: make(chan struct{})}
	c.obsFlight[sessionID] = flight
	c.obsMu.Unlock()

	snap, err := c.computeObservation(ctx, sessionID)

	c.obsMu.Lock()
	flight.snap = snap
	flight.err = err
	close(flight.done)
	delete(c.obsFlight, sessionID)
	if err == nil && snap != nil {
		c.obsCache[sessionID] = cachedObservation{
			snap:      snap,
			expiresAt: time.Now().Add(c.opts.ObservationTTL),
		}
	}
	c.obsMu.Unlock()

	return snap, err
}

func (c *Coordinator) computeObservation(ctx context.Context, sessionID string) (*domain.ObservationSnapshot, error) {
	sess, err := c.store.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	prof, err := c.store.GetProfile(ctx, sess.ProfileID)
	if err != nil {
		return nil, err
	}

	snap := &domain.ObservationSnapshot{
		SessionID:       sess.ID,
		ProjectID:       sess.ProjectID,
		ProfileID:       sess.ProfileID,
		IsLocked:        sess.IsLocked,
		ConnectionState: domain.StateBrowserStopped,
		Permission:      sess.Permission,
		ObservedAt:      time.Now().UTC(),
	}

	if !c.browser.IsProfileRunning(ctx, prof.CDPPort) {
		_ = c.store.UpdateSessionState(ctx, sess.ID, sess.TabID, sess.Permission, domain.StateBrowserStopped)
		return snap, nil
	}

	drv, target, err := c.getOrConnectDriver(ctx, sess, prof, false)
	if err != nil {
		snap.ConnectionState = domain.StateTabClosed
		_ = c.store.UpdateSessionState(ctx, sess.ID, "", sess.Permission, domain.StateTabClosed)
		return snap, nil
	}

	obsCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()

	probe, err := drv.ProbeTabState(obsCtx)
	if err != nil || probe == nil {
		snap.ConnectionState = domain.StateTabClosed
		return snap, nil
	}

	snap.ConnectionState = probe.ConnectionState
	snap.Permission = probe.Permission
	snap.NotebookMetadata = &probe.Identity
	_ = c.store.UpdateSessionState(ctx, sess.ID, target.ID, probe.Permission, probe.ConnectionState)

	if ccu, errCCU := drv.GetComputeUnits(obsCtx); errCCU == nil && ccu != nil {
		snap.ComputeUnits = ccu
	}
	c.syncProfileFromProbeAndCCU(obsCtx, prof, drv, probe)

	if probe.ConnectionState.IsKernelConnected() {
		if hw, errHW := drv.GetHardwareTelemetry(obsCtx); errHW == nil {
			snap.HardwareTelemetry = hw
		}
		// NEVER invoke kernel Python check when primary kernel is busy!
		allowKernelVerify := probe.ConnectionState == domain.StateIdle
		if ds, errD := drv.CheckDriveStatus(obsCtx, allowKernelVerify); errD == nil && ds != nil {
			c.obsMu.Lock()
			if allowKernelVerify && ds.TotalBytes > 0 {
				cp := *ds
				c.driveCache[sess.ID] = &cp
			} else if !allowKernelVerify && ds.Mounted {
				if prev := c.driveCache[sess.ID]; prev != nil {
					ds.TotalBytes = prev.TotalBytes
					ds.UsedBytes = prev.UsedBytes
					ds.FreeBytes = prev.FreeBytes
					ds.TotalGB = prev.TotalGB
					ds.UsedGB = prev.UsedGB
					ds.FreeGB = prev.FreeGB
				}
			}
			c.obsMu.Unlock()
			snap.DriveStatus = ds
		}
		if rc, errRC := drv.GetRunningCells(obsCtx); errRC == nil {
			snap.RunningCells = rc
		}
		if px, errPX := drv.GetProxyURL(obsCtx, 8080); errPX == nil {
			snap.ProxyInfo = px
		}
	}

	if probe.Permission == domain.PermissionOwner || probe.Permission == domain.PermissionEditor || probe.Permission == domain.PermissionCommenter || probe.Permission == domain.PermissionViewOnly {
		if rawCells, errC := drv.CallColabMcpTool(obsCtx, "get_cells", map[string]any{
			"cellIndexStart": 0,
			"cellIndexEnd":   200,
			"includeOutputs": false,
		}); errC == nil {
			var parsed struct {
				Cells []domain.Cell `json:"cells"`
			}
			if json.Unmarshal(rawCells, &parsed) == nil {
				items := make([]domain.OutlineItem, 0, len(parsed.Cells))
				codeN, mdN := 0, 0
				for i, cell := range parsed.Cells {
					src := strings.Join(cell.Source, "")
					lines := strings.Split(src, "\n")
					sum := ""
					if len(lines) > 0 {
						sum = strings.TrimSpace(lines[0])
					}
					if cell.CellType == "code" {
						codeN++
					} else {
						mdN++
					}
					items = append(items, domain.OutlineItem{
						Index:     i,
						CellID:    cell.ID,
						CellType:  cell.CellType,
						Summary:   sum,
						LineCount: len(lines),
					})
				}
				snap.CellsOutline = &domain.NotebookOutline{
					TotalCells:    len(parsed.Cells),
					CodeCells:     codeN,
					MarkdownCells: mdN,
					Outline:       items,
				}
			}
		}
	}

	return snap, nil
}

// RefreshAccountQuota inspects any open Colab tab in profileID (or cached store values if no tab is open)
// to return live Compute Units and account-wide active Colab VM sessions.
func (c *Coordinator) RefreshAccountQuota(ctx context.Context, profileID string) (*domain.AccountQuota, error) {
	prof, err := c.store.GetProfile(ctx, profileID)
	if err != nil {
		return nil, err
	}
	running := c.browser.IsProfileRunning(ctx, prof.CDPPort)
	out := &domain.AccountQuota{
		ProfileID:       prof.ID,
		AccountEmail:    prof.AccountEmail,
		DisplayName:     prof.DisplayName,
		IsRunningChrome: running,
		ActiveVMs:       []domain.ColabVMSession{},
		ObservedAt:      time.Now().UTC(),
	}
	if prof.LastTelemetryAt != nil || prof.CCUBalance > 0 || len(prof.EligibleGPUs) > 0 {
		out.ComputeUnits = &domain.ComputeUnitsInfo{
			Balance:               prof.CCUBalance,
			ConsumptionRateHourly: prof.CCUBurnRate,
			FormattedBalance:      fmt.Sprintf("%.2f", prof.CCUBalance),
			Tier:                  prof.Tier,
			EligibleGPUs:          prof.EligibleGPUs,
			EligibleTPUs:          prof.EligibleTPUs,
		}
	}
	if !running {
		return out, nil
	}

	targets, err := c.browser.ListTargets(ctx, prof.CDPPort)
	if err != nil {
		return out, nil
	}
	var colabTarget *domain.CDPTarget
	for i := range targets {
		if targets[i].Type == "page" && strings.Contains(targets[i].URL, "colab.research.google.com") {
			colabTarget = &targets[i]
			break
		}
	}
	if colabTarget == nil {
		return out, nil
	}

	drv, err := c.drivers.NewDriver(ctx, prof.CDPPort, *colabTarget)
	if err != nil {
		return out, nil
	}
	defer drv.Close()

	qCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()

	probe, _ := drv.ProbeTabState(qCtx)
	if probe != nil {
		if probe.Identity.EmailAddress != "" {
			out.AccountEmail = probe.Identity.EmailAddress
		}
		if probe.Identity.DisplayName != "" {
			out.DisplayName = probe.Identity.DisplayName
		}
	}
	if ccu, errC := drv.GetComputeUnits(qCtx); errC == nil && ccu != nil {
		out.ComputeUnits = ccu
	}
	if vms, errV := drv.ListAccountVMSessions(qCtx); errV == nil && vms != nil {
		out.ActiveVMs = vms
	}
	c.syncProfileFromProbeAndCCU(qCtx, prof, drv, probe)
	return out, nil
}

// StartReconciler runs a lightweight 5s background health/telemetry loop over locked sessions.
func (c *Coordinator) StartReconciler(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(c.opts.ReconcileInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-c.stopCh:
				return
			case <-ticker.C:
				locked, err := c.store.ListLockedSessions(ctx)
				if err != nil {
					continue
				}
				for _, s := range locked {
					obsCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
					snap, errObs := c.ObserveSession(obsCtx, s.ID)
					cancel()
					if errObs == nil && snap != nil {
						c.publish(domain.EventTelemetryUpdated, snap)
						if refreshed, errS := c.store.GetSession(ctx, s.ID); errS == nil && refreshed != nil {
							c.publish(domain.EventSessionUpdated, refreshed)
						}
					}
				}
			}
		}
	}()
}

// Close closes all cached CDP client connections without killing Chrome processes.
func (c *Coordinator) Close() error {
	c.stopOnce.Do(func() {
		close(c.stopCh)
	})
	c.drvMu.Lock()
	list := make([]*attachedDriver, 0, len(c.active))
	for k, v := range c.active {
		list = append(list, v)
		delete(c.active, k)
	}
	c.drvMu.Unlock()
	for _, ad := range list {
		if ad != nil && ad.driver != nil {
			_ = ad.driver.Close()
		}
	}
	return nil
}
