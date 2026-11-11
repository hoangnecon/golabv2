package golab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/hoangnecon/golabv2/internal/bridge"
	"github.com/hoangnecon/golabv2/internal/domain"
	"github.com/hoangnecon/golabv2/internal/driver/golab/scripts"
)

// Factory creates RuntimeDriver instances connected to specific Chrome CDP targets.
type Factory struct {
	browser domain.BrowserManager
}

// NewFactory creates a DriverFactory backed by BrowserManager.
func NewFactory(browser domain.BrowserManager) *Factory {
	return &Factory{browser: browser}
}

// NewDriver connects a multiplexed CDPClient to the specific target WebSocket and returns a RuntimeDriver.
func (f *Factory) NewDriver(ctx context.Context, cdpPort int, target domain.CDPTarget) (domain.RuntimeDriver, error) {
	client := bridge.NewTargetCDPClient(cdpPort, bridge.TargetInfo{
		ID:                   target.ID,
		Title:                target.Title,
		Type:                 target.Type,
		URL:                  target.URL,
		WebSocketDebuggerURL: target.WebSocketDebuggerURL,
	})
	if err := client.Connect(ctx); err != nil {
		return nil, domain.WrapAppError(
			domain.CodeTabClosed,
			fmt.Sprintf("failed to attach CDP to tab %s: %v", target.ID, err),
			"Re-open or focus the Colab tab in GoLab UI.",
			http.StatusBadGateway,
			err,
		)
	}
	return NewDriverWithBrowser(client, cdpPort, f.browser), nil
}

// Driver implements domain.RuntimeDriver backed by Chrome DevTools Protocol.
type Driver struct {
	cdp     *bridge.CDPClient
	cdpPort int
	browser domain.BrowserManager
}

// NewDriver constructs a Driver wrapping a CDP client.
func NewDriver(cdp *bridge.CDPClient) *Driver {
	return &Driver{cdp: cdp}
}

// NewDriverWithBrowser constructs a Driver with browser-level target inspection for OAuth popup handling.
func NewDriverWithBrowser(cdp *bridge.CDPClient, cdpPort int, browser domain.BrowserManager) *Driver {
	return &Driver{
		cdp:     cdp,
		cdpPort: cdpPort,
		browser: browser,
	}
}

// ConnectURL connects directly to a Chrome CDP HTTP endpoint (e.g. http://127.0.0.1:9222) and returns a Driver.
func ConnectURL(ctx context.Context, cdpURL string) (*Driver, error) {
	cdp := bridge.NewCDPClient(cdpURL)
	if err := cdp.Connect(ctx); err != nil {
		return nil, err
	}
	return NewDriver(cdp), nil
}

// CDP returns the underlying CDP client.
func (d *Driver) CDP() *bridge.CDPClient {
	return d.cdp
}

// Close closes the CDP WebSocket client without closing the user's browser tab.
func (d *Driver) Close() error {
	if d == nil || d.cdp == nil {
		return nil
	}
	return d.cdp.Close()
}

func (d *Driver) evaluateJSON(ctx context.Context, expression string, target any) error {
	val, err := d.cdp.Evaluate(ctx, expression)
	if err != nil {
		return err
	}

	var rawStr string
	if err := json.Unmarshal(val, &rawStr); err == nil {
		val = json.RawMessage(rawStr)
	}

	var errCheck struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(val, &errCheck); err == nil && errCheck.Error != "" {
		if strings.Contains(errCheck.Error, "No active Colab kernel") {
			return domain.NewAppError(
				domain.CodeKernelDisconnected,
				errCheck.Error,
				"Call colab_connect_runtime first or click Connect in the Colab tab.",
				http.StatusConflict,
			)
		}
		return errors.New(errCheck.Error)
	}

	if target != nil {
		if err := json.Unmarshal(val, target); err != nil {
			return fmt.Errorf("unmarshal driver result: %w", err)
		}
	}
	return nil
}

func bytesToGB(b int64) float64 {
	return math.Round((float64(b)/(1024*1024*1024))*100) / 100
}

func bytesToMB(b int64) float64 {
	return math.Round((float64(b)/(1024*1024))*10) / 10
}

// ProbeTabState inspects identity, Drive permission, and runtime/kernel state on the attached tab.
func (d *Driver) ProbeTabState(ctx context.Context) (*domain.TabProbe, error) {
	var probe domain.TabProbe
	if err := d.evaluateJSON(ctx, scripts.ScriptProbeTabState, &probe); err != nil {
		return nil, err
	}
	return &probe, nil
}

// GetComputeUnits extracts account Compute Units balance, burn rate, tier, and eligible GPUs/TPUs.
func (d *Driver) GetComputeUnits(ctx context.Context) (*domain.ComputeUnitsInfo, error) {
	var info domain.ComputeUnitsInfo
	if err := d.evaluateJSON(ctx, scripts.ScriptGetComputeUnits, &info); err != nil {
		return nil, err
	}
	if info.EligibleGPUs == nil {
		info.EligibleGPUs = []string{}
	}
	if info.IneligibleGPUs == nil {
		info.IneligibleGPUs = []string{}
	}
	if info.EligibleTPUs == nil {
		info.EligibleTPUs = []string{}
	}
	if info.IneligibleTPUs == nil {
		info.IneligibleTPUs = []string{}
	}
	return &info, nil
}

// ListAccountVMSessions lists active Colab VM assignments across the signed-in Google Account.
func (d *Driver) ListAccountVMSessions(ctx context.Context) ([]domain.ColabVMSession, error) {
	var sessions []domain.ColabVMSession
	if err := d.evaluateJSON(ctx, scripts.ScriptListAccountVMSessions, &sessions); err != nil {
		return nil, err
	}
	if sessions == nil {
		sessions = []domain.ColabVMSession{}
	}
	return sessions, nil
}

// FetchKernelXHR executes an authenticated HTTP request through Colab's tunnel gateway.
func (d *Driver) FetchKernelXHR(ctx context.Context, path string, opts domain.FetchOptions) (*domain.FetchResponse, error) {
	if opts.Method == "" {
		opts.Method = "GET"
	}
	headersJSON, _ := json.Marshal(opts.Headers)
	bodyJSON, _ := json.Marshal(opts.Body)

	jsExpr := scripts.ScriptFetchKernelXHR(opts.Method, string(headersJSON), string(bodyJSON), path)
	var resp domain.FetchResponse
	if err := d.evaluateJSON(ctx, jsExpr, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CallColabMcpTool invokes one of Colab's native frontend MCP tools (get_cells, add_code_cell, etc.).
func (d *Driver) CallColabMcpTool(ctx context.Context, toolName string, args map[string]any) (json.RawMessage, error) {
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}

	toolFieldMap := map[string]string{
		"get_cells":     "getCellsTool",
		"add_code_cell": "addCodeCellTool",
		"add_text_cell": "addTextCellTool",
		"update_cell":   "updateCellTool",
		"delete_cell":   "deleteCellTool",
		"move_cell":     "moveCellTool",
		"run_code_cell": "runCodeCellTool",
	}

	field, ok := toolFieldMap[toolName]
	if !ok {
		return nil, fmt.Errorf("unsupported native tool: %s", toolName)
	}

	jsExpr := scripts.ScriptCallMcpTool(field, string(argsJSON))
	var resObj struct {
		Success bool            `json:"success"`
		Result  json.RawMessage `json:"result"`
		Error   string          `json:"error"`
	}
	if err := d.evaluateJSON(ctx, jsExpr, &resObj); err != nil {
		return nil, fmt.Errorf("call mcp tool %s: %w", toolName, err)
	}
	if resObj.Error != "" {
		return nil, errors.New(resObj.Error)
	}
	return resObj.Result, nil
}

// CallMcpTool is kept as a convenience alias for CallColabMcpTool.
func (d *Driver) CallMcpTool(ctx context.Context, toolName string, args map[string]any) (json.RawMessage, error) {
	return d.CallColabMcpTool(ctx, toolName, args)
}

func (d *Driver) ConnectRuntime(ctx context.Context) error {
	return d.evaluateJSON(ctx, scripts.ScriptConnectRuntime, nil)
}

func (d *Driver) DisconnectRuntime(ctx context.Context) error {
	return d.evaluateJSON(ctx, scripts.ScriptDisconnectRuntime, nil)
}

func (d *Driver) InterruptKernel(ctx context.Context) error {
	return d.evaluateJSON(ctx, scripts.ScriptInterruptKernel, nil)
}

func (d *Driver) RestartKernel(ctx context.Context) error {
	return d.evaluateJSON(ctx, scripts.ScriptRestartKernel, nil)
}

func (d *Driver) GetAvailableAccelerators(ctx context.Context) (*domain.RuntimeConfigReport, error) {
	var report domain.RuntimeConfigReport
	if err := d.evaluateJSON(ctx, scripts.ScriptGetAvailableAccelerators, &report); err != nil {
		return nil, err
	}
	return &report, nil
}

func (d *Driver) SetAccelerator(ctx context.Context, value string, highRAM bool) error {
	return d.evaluateJSON(ctx, scripts.ScriptSetAccelerator(value, highRAM), nil)
}

// ExecPython executes Python silently on the primary kernel when idle, or on a sidecar kernel when busy.
func (d *Driver) ExecPython(ctx context.Context, code string, preferSidecarWhenBusy bool) (*domain.PythonExecResult, error) {
	var res domain.PythonExecResult
	if err := d.evaluateJSON(ctx, scripts.ScriptExecPython(code, preferSidecarWhenBusy), &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// GetHardwareTelemetry fetches VM RAM, Disk, and GPU metrics via /api/colab/resources.
func (d *Driver) GetHardwareTelemetry(ctx context.Context) (*domain.HardwareTelemetry, error) {
	resp, err := d.FetchKernelXHR(ctx, "/api/colab/resources", domain.FetchOptions{Method: "GET"})
	if err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("resources request failed with status %d: %s", resp.Status, resp.RawText)
	}

	var raw struct {
		Memory struct {
			TotalBytes int64 `json:"totalBytes"`
			FreeBytes  int64 `json:"freeBytes"`
		} `json:"memory"`
		Disks []struct {
			Filesystem struct {
				Label      string `json:"label"`
				TotalBytes int64  `json:"totalBytes"`
				UsedBytes  int64  `json:"usedBytes"`
			} `json:"filesystem"`
		} `json:"disks"`
		GPUs []struct {
			Name              string  `json:"name"`
			MemoryUsedBytes   int64   `json:"memoryUsedBytes"`
			MemoryTotalBytes  int64   `json:"memoryTotalBytes"`
			GPUUtilization    float64 `json:"gpuUtilization"`
			MemoryUtilization float64 `json:"memoryUtilization"`
			EverUsed          bool    `json:"everUsed"`
		} `json:"gpus"`
	}
	if err := json.Unmarshal(resp.Body, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal resources response: %w", err)
	}

	usedRAM := raw.Memory.TotalBytes - raw.Memory.FreeBytes
	out := &domain.HardwareTelemetry{
		RAM: domain.RAMTelemetry{
			TotalBytes: raw.Memory.TotalBytes,
			FreeBytes:  raw.Memory.FreeBytes,
			UsedBytes:  usedRAM,
			TotalGB:    bytesToGB(raw.Memory.TotalBytes),
			FreeGB:     bytesToGB(raw.Memory.FreeBytes),
			UsedGB:     bytesToGB(usedRAM),
		},
		Disks: make([]domain.DiskTelemetry, 0, len(raw.Disks)),
		GPUs:  make([]domain.GPUTelemetry, 0, len(raw.GPUs)),
	}

	for _, dk := range raw.Disks {
		fs := dk.Filesystem
		var pct float64
		if fs.TotalBytes > 0 {
			pct = math.Round((float64(fs.UsedBytes)/float64(fs.TotalBytes))*1000) / 10
		}
		out.Disks = append(out.Disks, domain.DiskTelemetry{
			Label:       fs.Label,
			TotalBytes:  fs.TotalBytes,
			UsedBytes:   fs.UsedBytes,
			TotalGB:     bytesToGB(fs.TotalBytes),
			UsedGB:      bytesToGB(fs.UsedBytes),
			UsedPercent: pct,
		})
	}
	for _, g := range raw.GPUs {
		usedMB := bytesToMB(g.MemoryUsedBytes)
		totMB := bytesToMB(g.MemoryTotalBytes)
		out.GPUs = append(out.GPUs, domain.GPUTelemetry{
			Name:              g.Name,
			MemoryUsedBytes:   g.MemoryUsedBytes,
			MemoryTotalBytes:  g.MemoryTotalBytes,
			MemoryUsedMB:      usedMB,
			MemoryTotalMB:     totMB,
			MemoryFreeMB:      totMB - usedMB,
			GPUUtilization:    g.GPUUtilization,
			MemoryUtilization: g.MemoryUtilization,
			EverUsed:          g.EverUsed,
		})
	}
	return out, nil
}

// GetBuildInfo retrieves Colab container release metadata via /api/colab/build-info.
func (d *Driver) GetBuildInfo(ctx context.Context) (*domain.BuildInfo, error) {
	resp, err := d.FetchKernelXHR(ctx, "/api/colab/build-info", domain.FetchOptions{Method: "GET"})
	if err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("build info failed with status %d: %s", resp.Status, resp.RawText)
	}
	var raw struct {
		ReleaseTag string `json:"release_tag"`
	}
	if err := json.Unmarshal(resp.Body, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal build info: %w", err)
	}
	return &domain.BuildInfo{ReleaseTag: raw.ReleaseTag}, nil
}

// CheckDriveStatus performs ground-truth Google Drive FUSE mount verification.
// When allowKernelVerify is false (e.g., during non-blocking /observe when busy), only DOM + /api/colab/drive XHR is run.
// When allowKernelVerify is true, /proc/mounts and shutil.disk_usage are checked (using sidecar if primary kernel is busy),
// catching unauthenticated local shadow directories (/content/drive created via os.makedirs).
func (d *Driver) CheckDriveStatus(ctx context.Context, allowKernelVerify bool) (*domain.DriveStatus, error) {
	var fast domain.DriveStatus
	if err := d.evaluateJSON(ctx, scripts.ScriptCheckDriveFast, &fast); err != nil {
		return nil, err
	}

	applyPendingOAuthPopup := func(ds *domain.DriveStatus) *domain.DriveStatus {
		if ds == nil || ds.Mounted || ds.Status == "corrupted_local_dir" || d.browser == nil || d.cdpPort <= 0 {
			return ds
		}
		targets, err := d.browser.ListTargets(ctx, d.cdpPort)
		if err != nil {
			return ds
		}
		for _, t := range targets {
			if strings.Contains(t.URL, "accounts.google.com") {
				ds.Status = "drive_auth_pending"
				ds.AuthPopupTabID = t.ID
				ds.AuthPopupURL = t.URL
				ds.Message = "Shared notebook requires Google Drive OAuth confirmation in the Account's browser window."
				ds.ActionRequired = "Click 'Open Browser Tab' in GoLab UI and approve the Google Drive OAuth prompt."
				break
			}
		}
		return ds
	}

	if !allowKernelVerify {
		applyPendingOAuthPopup(&fast)
		if !fast.Mounted && fast.ActionRequired == "" {
			fast.ActionRequired = "Call colab_mount_drive to mount Google Drive FUSE (/content/drive)."
		}
		return &fast, nil
	}

	pyRes, err := d.ExecPython(ctx, scripts.PythonCheckDriveFUSE, true)
	if err != nil || pyRes.Error != "" {
		// Fall back to fast check if kernel is not connected yet
		applyPendingOAuthPopup(&fast)
		return &fast, nil
	}

	var fuseInfo struct {
		HasFUSE   bool  `json:"has_fuse"`
		IsMount   bool  `json:"is_mount"`
		DirExists bool  `json:"dir_exists"`
		NonEmpty  bool  `json:"non_empty"`
		Total     int64 `json:"total"`
		Used      int64 `json:"used"`
		Free      int64 `json:"free"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(pyRes.Stdout)), &fuseInfo); err != nil {
		applyPendingOAuthPopup(&fast)
		return &fast, nil
	}

	if fuseInfo.HasFUSE || fuseInfo.IsMount {
		return &domain.DriveStatus{
			Mounted:        true,
			Status:         "mounted",
			MountPath:      "/content/drive",
			IsFUSEVerified: true,
			ButtonState:    fast.ButtonState,
			CredentialsOK:  true,
			TotalBytes:     fuseInfo.Total,
			UsedBytes:      fuseInfo.Used,
			FreeBytes:      fuseInfo.Free,
			TotalGB:        bytesToGB(fuseInfo.Total),
			UsedGB:         bytesToGB(fuseInfo.Used),
			FreeGB:         bytesToGB(fuseInfo.Free),
			Message:        "Google Drive FUSE (/content/drive) is mounted and verified via /proc/mounts.",
		}, nil
	}

	if fuseInfo.DirExists && fuseInfo.NonEmpty {
		return &domain.DriveStatus{
			Mounted:        false,
			Status:         "corrupted_local_dir",
			MountPath:      "/content/drive",
			IsFUSEVerified: true,
			ButtonState:    fast.ButtonState,
			CredentialsOK:  false,
			Message:        "Local shadow directory /content/drive contains files without a FUSE mount. Writing here only writes to ephemeral VM SSD.",
			ActionRequired: "Move or remove local files inside /content/drive before calling colab_mount_drive.",
		}, nil
	}

	out := &domain.DriveStatus{
		Mounted:        false,
		Status:         "unmounted",
		MountPath:      "/content/drive",
		IsFUSEVerified: true,
		ButtonState:    fast.ButtonState,
		CredentialsOK:  fast.CredentialsOK,
		Message:        "Google Drive is not mounted.",
		ActionRequired: "Call colab_mount_drive to mount Google Drive FUSE (/content/drive).",
	}
	return applyPendingOAuthPopup(out), nil
}

// MountDrive executes the two-path Google Drive FUSE mount:
// - Case A (isSoleAuthored == true): clicks Mount Drive button + <mwc-dialog> confirm (dfs_persistent).
// - Case B (isSoleAuthored == false, shared notebook): triggers dfs_ephemeral + watches/auto-consents OAuth popup,
//   or returns GOLAB_DRIVE_OAUTH_INTERACTION_REQUIRED with popup tab details if manual 2FA/re-auth is required.
func (d *Driver) MountDrive(ctx context.Context) (*domain.DriveMountResult, error) {
	cur, err := d.CheckDriveStatus(ctx, true)
	if err == nil && cur != nil {
		if cur.Mounted {
			return &domain.DriveMountResult{
				Mounted:     true,
				Flow:        "already_mounted",
				Status:      "mounted",
				DriveStatus: *cur,
				Message:     "Google Drive is already mounted.",
			}, nil
		}
		if cur.Status == "corrupted_local_dir" {
			return &domain.DriveMountResult{
					Mounted:     false,
					Flow:        "blocked",
					Status:      "corrupted_local_dir",
					DriveStatus: *cur,
					Message:     cur.Message,
				}, domain.NewAppError(
					domain.CodeDriveLocalShadowDir,
					cur.Message,
					cur.ActionRequired,
					http.StatusConflict,
				)
		}
	}

	var probe struct {
		IsSoleAuthored bool `json:"is_sole_authored"`
	}
	if err := d.evaluateJSON(ctx, scripts.ScriptMountDriveProbe, &probe); err != nil {
		return nil, err
	}

	if probe.IsSoleAuthored {
		if err := d.evaluateJSON(ctx, scripts.ScriptMountDriveSoleAuthored, nil); err != nil {
			return nil, err
		}
		deadline := time.Now().Add(20 * time.Second)
		for {
			st, _ := d.CheckDriveStatus(ctx, true)
			if st != nil && st.Mounted {
				return &domain.DriveMountResult{
					Mounted:     true,
					Flow:        "sole_authored_persistent",
					Status:      "mounted",
					DriveStatus: *st,
					Message:     "Mounted Google Drive via sole-authored persistent flow.",
				}, nil
			}
			if time.Now().After(deadline) || ctx.Err() != nil {
				finalSt := domain.DriveStatus{Status: "unmounted", MountPath: "/content/drive"}
				if st != nil {
					finalSt = *st
				}
				return &domain.DriveMountResult{
					Mounted:     false,
					Flow:        "sole_authored_persistent",
					Status:      finalSt.Status,
					DriveStatus: finalSt,
					Message:     "Timed out waiting for sole-authored Google Drive FUSE mount.",
				}, domain.NewAppError(domain.CodeDriveUnmounted, "Timed out waiting for Google Drive mount.", "Open the Colab tab in GoLab UI and verify Google Drive access.", http.StatusGatewayTimeout)
			}
			// Fast short poll for deterministic tests and quick live mounts
			select {
			case <-ctx.Done():
			case <-time.After(250 * time.Millisecond):
			}
		}
	}

	// Case B: Shared / multi-account notebook -> dfs_ephemeral + OAuth popup inspection
	if err := d.evaluateJSON(ctx, scripts.ScriptMountDriveSharedTrigger, nil); err != nil {
		return nil, err
	}

	var popupID, popupURL string
	if d.browser != nil && d.cdpPort > 0 {
		targets, _ := d.browser.ListTargets(ctx, d.cdpPort)
		for _, t := range targets {
			if strings.Contains(t.URL, "accounts.google.com") {
				popupID = t.ID
				popupURL = t.URL
				if t.WebSocketDebuggerURL != "" {
					popClient := bridge.NewTargetCDPClient(d.cdpPort, bridge.TargetInfo{
						ID:                   t.ID,
						Type:                 t.Type,
						URL:                  t.URL,
						WebSocketDebuggerURL: t.WebSocketDebuggerURL,
					})
					if popClient.Connect(ctx) == nil {
						_, _ = popClient.Evaluate(ctx, scripts.ScriptAutoConsentOAuthPopup)
						_ = popClient.Close()
					}
				}
				break
			}
		}
	}

	// Check whether auto-consent completed the mount within a short window
	for i := 0; i < 4; i++ {
		st, _ := d.CheckDriveStatus(ctx, true)
		if st != nil && st.Mounted {
			return &domain.DriveMountResult{
				Mounted:     true,
				Flow:        "shared_ephemeral",
				Status:      "mounted",
				DriveStatus: *st,
				Message:     "Mounted Google Drive via shared ephemeral OAuth flow.",
			}, nil
		}
		if popupID != "" && i == 0 {
			// If an OAuth popup was detected without a connectable WS or still pending, surface immediately if unmounted
			if d.browser != nil {
				targets, _ := d.browser.ListTargets(ctx, d.cdpPort)
				stillOpen := false
				for _, t := range targets {
					if t.ID == popupID {
						stillOpen = true
						break
					}
				}
				if stillOpen {
					break
				}
			}
		}
		select {
		case <-ctx.Done():
		case <-time.After(150 * time.Millisecond):
		}
	}

	pendingStatus := domain.DriveStatus{
		Mounted:        false,
		Status:         "drive_auth_pending",
		MountPath:      "/content/drive",
		IsFUSEVerified: true,
		AuthPopupTabID: popupID,
		AuthPopupURL:   popupURL,
		Message:        "Shared notebook requires Google Drive OAuth confirmation in the Account's browser window.",
		ActionRequired: "Click 'Open Browser Tab' in GoLab UI and approve the Google Drive OAuth prompt.",
	}
	res := &domain.DriveMountResult{
		Mounted:        false,
		Flow:           "shared_ephemeral",
		Status:         "drive_auth_pending",
		DriveStatus:    pendingStatus,
		AuthPopupTabID: popupID,
		AuthPopupURL:   popupURL,
		Message:        pendingStatus.Message,
	}
	return res, domain.NewAppError(
		domain.CodeDriveOAuthRequired,
		pendingStatus.Message,
		pendingStatus.ActionRequired,
		http.StatusAccepted,
	)
}

func (d *Driver) runJobAction(ctx context.Context, action, jobID, command, cwd string, tailLines int) (*domain.BackgroundJob, error) {
	code := scripts.PythonBackgroundJobScript(action, jobID, command, cwd, tailLines)
	pyRes, err := d.ExecPython(ctx, code, true)
	if err != nil {
		return nil, err
	}
	if pyRes.Error != "" {
		return nil, errors.New(pyRes.Error)
	}
	var job domain.BackgroundJob
	if err := json.Unmarshal([]byte(strings.TrimSpace(pyRes.Stdout)), &job); err != nil {
		return nil, fmt.Errorf("parse background job output (%q): %w", pyRes.Stdout, err)
	}
	job.UsedSidecar = pyRes.UsedSidecar
	return &job, nil
}

// StartBackgroundJob launches an out-of-band shell/Python job logged to /tmp/.golab_jobs/<id>.log.
func (d *Driver) StartBackgroundJob(ctx context.Context, command string, cwd string) (*domain.BackgroundJob, error) {
	if strings.TrimSpace(command) == "" {
		return nil, domain.NewAppError(domain.CodeInvalidInput, "command cannot be empty", "Provide a shell or python command to execute.", http.StatusBadRequest)
	}
	return d.runJobAction(ctx, "start", "", command, cwd, 80)
}

// PollBackgroundJob reads PID status, exit code, and trailing log lines of a background job.
func (d *Driver) PollBackgroundJob(ctx context.Context, jobID string, tailLines int) (*domain.BackgroundJob, error) {
	if strings.TrimSpace(jobID) == "" {
		return nil, domain.NewAppError(domain.CodeInvalidInput, "job_id cannot be empty", "", http.StatusBadRequest)
	}
	if tailLines <= 0 {
		tailLines = 80
	}
	return d.runJobAction(ctx, "poll", jobID, "", "", tailLines)
}

// KillBackgroundJob sends SIGTERM/SIGKILL to a background job's process group.
func (d *Driver) KillBackgroundJob(ctx context.Context, jobID string) (*domain.BackgroundJob, error) {
	if strings.TrimSpace(jobID) == "" {
		return nil, domain.NewAppError(domain.CodeInvalidInput, "job_id cannot be empty", "", http.StatusBadRequest)
	}
	return d.runJobAction(ctx, "kill", jobID, "", "", 80)
}

func (d *Driver) GetRunningCells(ctx context.Context) (*domain.RunningCellsReport, error) {
	var list []domain.RunningCellInfo
	if err := d.evaluateJSON(ctx, scripts.ScriptGetRunningCells, &list); err != nil {
		return nil, err
	}

	activeCount := 0
	for _, c := range list {
		if c.IsRunning {
			activeCount++
		}
	}
	return &domain.RunningCellsReport{
		HasRunningCells: activeCount > 0,
		TotalRunning:    activeCount,
		RunningCells:    list,
	}, nil
}

func (d *Driver) SaveNotebook(ctx context.Context) error {
	return d.evaluateJSON(ctx, scripts.ScriptSaveNotebook, nil)
}

func (d *Driver) RunAll(ctx context.Context) error {
	return d.evaluateJSON(ctx, scripts.ScriptRunAll, nil)
}

func (d *Driver) GetProxyURL(ctx context.Context, port int) (*domain.ProxyEndpoint, error) {
	if port <= 0 || port > 65535 {
		return nil, domain.NewAppError(domain.CodeInvalidInput, fmt.Sprintf("invalid port: %d", port), "Port must be between 1 and 65535.", http.StatusBadRequest)
	}
	var ep domain.ProxyEndpoint
	if err := d.evaluateJSON(ctx, scripts.ScriptGetProxyURL(port), &ep); err != nil {
		return nil, err
	}
	return &ep, nil
}
