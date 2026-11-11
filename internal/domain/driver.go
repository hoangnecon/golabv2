package domain

import (
	"context"
	"encoding/json"
	"time"
)

// CDPTarget describes a Chrome DevTools Protocol page/popup target.
type CDPTarget struct {
	ID                   string `json:"id"`
	Title                string `json:"title"`
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

// FetchOptions configures an internal Colab tunnel XHR request.
type FetchOptions struct {
	Method  string
	Headers map[string]string
	Body    string
}

// FetchResponse represents the parsed response from Colab's kernel XHR gateway.
type FetchResponse struct {
	Status     int             `json:"status"`
	StatusText string          `json:"status_text"`
	OK         bool            `json:"ok"`
	Body       json.RawMessage `json:"body"`
	RawText    string          `json:"raw_text,omitempty"`
}

// TabProbe captures the current state of an attached browser tab.
type TabProbe struct {
	URL             string           `json:"url"`
	Title           string           `json:"title"`
	ConnectionState ConnectionState  `json:"connection_state"`
	Permission      PermissionLevel  `json:"permission"`
	ConnectState    string           `json:"connect_state"`
	KernelState     string           `json:"kernel_state"`
	IsExecuting     bool             `json:"is_executing"`
	Identity        NotebookIdentity `json:"identity"`
}

// AcceleratorOption represents a hardware accelerator (GPU/TPU/CPU).
type AcceleratorOption struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Checked  bool   `json:"checked"`
	Disabled bool   `json:"disabled"`
}

// RuntimeConfigReport details the available and active runtime hardware.
type RuntimeConfigReport struct {
	Accelerators []AcceleratorOption `json:"accelerators"`
	HighRAM      bool                `json:"high_ram"`
}

// RAMTelemetry contains system RAM metrics.
type RAMTelemetry struct {
	TotalBytes int64   `json:"total_bytes"`
	FreeBytes  int64   `json:"free_bytes"`
	UsedBytes  int64   `json:"used_bytes"`
	TotalGB    float64 `json:"total_gb"`
	FreeGB     float64 `json:"free_gb"`
	UsedGB     float64 `json:"used_gb"`
}

// DiskTelemetry contains disk usage metrics.
type DiskTelemetry struct {
	Label       string  `json:"label"`
	TotalBytes  int64   `json:"total_bytes"`
	UsedBytes   int64   `json:"used_bytes"`
	TotalGB     float64 `json:"total_gb"`
	UsedGB      float64 `json:"used_gb"`
	UsedPercent float64 `json:"used_percent"`
}

// GPUTelemetry contains NVIDIA GPU metrics.
type GPUTelemetry struct {
	Name              string  `json:"name"`
	MemoryUsedBytes   int64   `json:"memory_used_bytes"`
	MemoryTotalBytes  int64   `json:"memory_total_bytes"`
	MemoryUsedMB      float64 `json:"memory_used_mb"`
	MemoryTotalMB     float64 `json:"memory_total_mb"`
	MemoryFreeMB      float64 `json:"memory_free_mb"`
	GPUUtilization    float64 `json:"gpu_utilization"`
	MemoryUtilization float64 `json:"memory_utilization"`
	EverUsed          bool    `json:"ever_used"`
}

// HardwareTelemetry encapsulates VM RAM, Disk, and GPU metrics.
type HardwareTelemetry struct {
	RAM   RAMTelemetry    `json:"ram"`
	Disks []DiskTelemetry `json:"disks"`
	GPUs  []GPUTelemetry  `json:"gpus"`
}

// BuildInfo contains Colab container release metadata.
type BuildInfo struct {
	ReleaseTag string `json:"release_tag"`
}

// DriveStatus reports ground-truth Google Drive FUSE mount state and quota.
type DriveStatus struct {
	Mounted         bool    `json:"mounted"`
	Status          string  `json:"status"` // "mounted", "unmounted", "drive_auth_pending", "corrupted_local_dir"
	MountPath       string  `json:"mount_path"`
	IsFUSEVerified  bool    `json:"is_fuse_verified"`
	ButtonState     string  `json:"button_state,omitempty"`
	CredentialsOK   bool    `json:"credentials_ok"`
	TotalBytes      int64   `json:"total_bytes,omitempty"`
	UsedBytes       int64   `json:"used_bytes,omitempty"`
	FreeBytes       int64   `json:"free_bytes,omitempty"`
	TotalGB         float64 `json:"total_gb,omitempty"`
	UsedGB          float64 `json:"used_gb,omitempty"`
	FreeGB          float64 `json:"free_gb,omitempty"`
	Message         string  `json:"message"`
	ActionRequired  string  `json:"action_required,omitempty"`
	AuthPopupTabID  string  `json:"auth_popup_tab_id,omitempty"`
	AuthPopupURL    string  `json:"auth_popup_url,omitempty"`
}

// DriveMountResult reports the outcome of colab_mount_drive.
type DriveMountResult struct {
	Mounted        bool        `json:"mounted"`
	Flow           string      `json:"flow"` // "sole_authored_persistent" or "shared_ephemeral"
	Status         string      `json:"status"`
	DriveStatus    DriveStatus `json:"drive_status"`
	AuthPopupTabID string      `json:"auth_popup_tab_id,omitempty"`
	AuthPopupURL   string      `json:"auth_popup_url,omitempty"`
	Message        string      `json:"message"`
}

// PythonExecResult captures silent Python execution output (primary or sidecar kernel).
type PythonExecResult struct {
	UsedSidecar bool   `json:"used_sidecar"`
	KernelID    string `json:"kernel_id,omitempty"`
	Stdout      string `json:"stdout"`
	Stderr      string `json:"stderr,omitempty"`
	Result      string `json:"result,omitempty"`
	Error       string `json:"error,omitempty"`
}

// BackgroundJob represents an out-of-band shell/Python job managed in /tmp/.golab_jobs/.
type BackgroundJob struct {
	JobID       string  `json:"job_id"`
	Command     string  `json:"command,omitempty"`
	PID         int     `json:"pid"`
	Running     bool    `json:"running"`
	ExitCode    *int    `json:"exit_code,omitempty"`
	LogTail     string  `json:"log_tail"`
	LogBytes    int64   `json:"log_bytes"`
	LogPath     string  `json:"log_path"`
	ElapsedSec  float64 `json:"elapsed_sec,omitempty"`
	UsedSidecar bool    `json:"used_sidecar"`
}

// Cell represents a single notebook cell.
type Cell struct {
	ID             string           `json:"id"`
	CellType       string           `json:"cell_type"` // "code" or "markdown"
	Source         []string         `json:"source"`
	ExecutionCount *int             `json:"execution_count,omitempty"`
	Outputs        []map[string]any `json:"outputs,omitempty"`
}

// OutlineItem represents a concise overview of a single cell.
type OutlineItem struct {
	Index     int    `json:"index"`
	CellID    string `json:"cell_id"`
	CellType  string `json:"cell_type"`
	Summary   string `json:"summary"`
	LineCount int    `json:"line_count"`
}

// NotebookOutline contains structural metadata of the notebook.
type NotebookOutline struct {
	TotalCells    int           `json:"total_cells"`
	CodeCells     int           `json:"code_cells"`
	MarkdownCells int           `json:"markdown_cells"`
	Outline       []OutlineItem `json:"outline"`
}

// RunningCellInfo represents the execution state and live logs of a cell.
type RunningCellInfo struct {
	Index          int     `json:"index"`
	CellID         string  `json:"cell_id"`
	IsRunning      bool    `json:"is_running"`
	ElapsedSeconds float64 `json:"elapsed_seconds"`
	ExecutionCount string  `json:"execution_count"`
	Stdout         string  `json:"stdout"`
}

// RunningCellsReport provides an overview of currently executing cells.
type RunningCellsReport struct {
	HasRunningCells bool              `json:"has_running_cells"`
	TotalRunning    int               `json:"total_running"`
	RunningCells    []RunningCellInfo `json:"running_cells"`
}

// ProxyEndpoint contains Colab external proxy URL and bearer token for an exposed VM port.
type ProxyEndpoint struct {
	Port            int    `json:"port"`
	URL             string `json:"url"`
	Token           string `json:"token,omitempty"`
	TokenExpiresSec int    `json:"token_expires_sec,omitempty"`
}

// RuntimeDriver abstracts direct CDP and Colab tunnel communication with a specific browser tab.
type RuntimeDriver interface {
	Close() error

	// Identity, Permissions, Quota & Active VMs
	ProbeTabState(ctx context.Context) (*TabProbe, error)
	GetComputeUnits(ctx context.Context) (*ComputeUnitsInfo, error)
	ListAccountVMSessions(ctx context.Context) ([]ColabVMSession, error)

	// Low-level Tunnel & Native Colab MCP Tooling
	FetchKernelXHR(ctx context.Context, path string, opts FetchOptions) (*FetchResponse, error)
	CallColabMcpTool(ctx context.Context, toolName string, args map[string]any) (json.RawMessage, error)

	// Runtime & Kernel Lifecycle
	ConnectRuntime(ctx context.Context) error
	DisconnectRuntime(ctx context.Context) error
	InterruptKernel(ctx context.Context) error
	RestartKernel(ctx context.Context) error
	GetAvailableAccelerators(ctx context.Context) (*RuntimeConfigReport, error)
	SetAccelerator(ctx context.Context, value string, highRAM bool) error
	ExecPython(ctx context.Context, code string, preferSidecarWhenBusy bool) (*PythonExecResult, error)

	// Telemetry & Two-Path Google Drive FUSE Mount
	GetHardwareTelemetry(ctx context.Context) (*HardwareTelemetry, error)
	GetBuildInfo(ctx context.Context) (*BuildInfo, error)
	CheckDriveStatus(ctx context.Context, allowKernelVerify bool) (*DriveStatus, error)
	MountDrive(ctx context.Context) (*DriveMountResult, error)

	// Out-of-Band Background Jobs (Sidecar-backed)
	StartBackgroundJob(ctx context.Context, command string, cwd string) (*BackgroundJob, error)
	PollBackgroundJob(ctx context.Context, jobID string, tailLines int) (*BackgroundJob, error)
	KillBackgroundJob(ctx context.Context, jobID string) (*BackgroundJob, error)

	// Notebook DOM Automation & Port Proxy
	GetRunningCells(ctx context.Context) (*RunningCellsReport, error)
	SaveNotebook(ctx context.Context) error
	RunAll(ctx context.Context) error
	GetProxyURL(ctx context.Context, port int) (*ProxyEndpoint, error)
}

// GolabDriver is kept as a type alias for RuntimeDriver for clean transitions.
type GolabDriver = RuntimeDriver

// DriverFactory creates a RuntimeDriver bound to a specific Chrome CDP port and target WebSocket URL.
type DriverFactory interface {
	NewDriver(ctx context.Context, cdpPort int, target CDPTarget) (RuntimeDriver, error)
}

// BrowserManager oversees isolated Chrome profile processes and their CDP targets.
type BrowserManager interface {
	LaunchProfile(ctx context.Context, profile Profile, initialURL string) (pid int, actualPort int, err error)
	StopProfile(ctx context.Context, profile Profile) error
	IsProfileRunning(ctx context.Context, cdpPort int) bool
	ListTargets(ctx context.Context, cdpPort int) ([]CDPTarget, error)
	OpenOrFocusTab(ctx context.Context, cdpPort int, targetURL string, matchSubstr string) (CDPTarget, error)
	FocusTab(ctx context.Context, cdpPort int, tabID string) error
	CloseTab(ctx context.Context, cdpPort int, tabID string) error
	ShutdownAdapter() error
}

// WorkspaceSnapshot is the atomic bootstrap state returned by GET /api/v1/workspace and SSE initial frame.
type WorkspaceSnapshot struct {
	Profiles        []*Profile     `json:"profiles"`
	Projects        []*Project     `json:"projects"`
	Sessions        []*Session     `json:"sessions"`
	RecentActivity  []*ActivityLog `json:"recent_activity"`
	RecentLogsCount int64          `json:"recent_logs_count"`
	DaemonVersion   string         `json:"daemon_version"`
	MCPHTTPURL      string         `json:"mcp_http_url"`
	Timestamp       time.Time      `json:"timestamp"`
}
