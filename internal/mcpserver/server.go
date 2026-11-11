package mcpserver

import (
	"context"

	"github.com/hoangnecon/golabv2/internal/domain"
	"github.com/hoangnecon/golabv2/internal/service"
	"github.com/hoangnecon/golabv2/internal/service/jobs"
	"github.com/hoangnecon/golabv2/internal/service/kernel"
	"github.com/hoangnecon/golabv2/internal/service/notebook"
	"github.com/hoangnecon/golabv2/internal/service/proxy"
	"github.com/hoangnecon/golabv2/internal/service/storage"
	"github.com/hoangnecon/golabv2/internal/service/telemetry"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const ServerVersion = "v2.0.0"

// Dependencies bundles pure-domain ports and services injected into the MCP Server.
type Dependencies struct {
	Coordinator  domain.SessionCoordinator
	WorkspaceSvc *service.WorkspaceService
	ProfileSvc   *service.ProfileService
	ProjectSvc   *service.ProjectService
	SessionSvc   *service.SessionService
	ActivitySvc  *service.ActivityService
	Version      string
}

// RuntimeServices constructs per-session domain tool services around a resolved RuntimeDriver.
type RuntimeServices struct {
	Target    *domain.ResolvedTarget
	Kernel    *kernel.Service
	Telemetry *telemetry.Service
	Storage   *storage.Service
	Notebook  *notebook.Service
	Jobs      *jobs.Service
	Proxy     *proxy.Service
}

// Server wraps a per-connection GoLab v2 MCP Server bound to an optional glp_/gls_ connection token.
type Server struct {
	mcp       *mcp.Server
	deps      Dependencies
	connToken string
}

// NewServer constructs a GoLab v2 MCP Server with all 43 canonical tools and outermost audit capture.
func NewServer(deps Dependencies, connToken string) *Server {
	ver := deps.Version
	if ver == "" {
		ver = ServerVersion
	}
	s := &Server{
		deps:      deps,
		connToken: connToken,
	}
	s.mcp = mcp.NewServer(&mcp.Implementation{
		Name:    "golab",
		Version: ver,
	}, nil)

	s.registerTools()
	s.mcp.AddReceivingMiddleware(s.captureToolCalls)
	return s
}

// MCP returns the underlying go-sdk *mcp.Server instance.
func (s *Server) MCP() *mcp.Server {
	return s.mcp
}

func (s *Server) resolveServices(ctx context.Context, explicitSessionID string, requireWrite bool) (*RuntimeServices, error) {
	if s.deps.Coordinator == nil {
		err := domain.NewAppError(domain.CodeInternalError, "session coordinator is not initialized", "", 500)
		setCallAuditError(ctx, err)
		return nil, err
	}
	target, err := s.deps.Coordinator.ResolveMCPTarget(ctx, s.connToken, explicitSessionID, requireWrite)
	if err != nil {
		setCallAuditError(ctx, err)
		return nil, err
	}
	setCallAuditTarget(ctx, target)
	return &RuntimeServices{
		Target:    target,
		Kernel:    kernel.NewService(target.Driver),
		Telemetry: telemetry.NewService(target.Driver),
		Storage:   storage.NewService(target.Driver),
		Notebook:  notebook.NewService(target.Driver),
		Jobs:      jobs.NewService(target.Driver),
		Proxy:     proxy.NewService(target.Driver),
	}, nil
}

func (s *Server) failTool(ctx context.Context, target *domain.ResolvedTarget, err error, latencyMs int64) (*mcp.CallToolResult, any, error) {
	setCallAuditError(ctx, err)
	return errorResult(target, err, latencyMs)
}

func (s *Server) failToolWithData(ctx context.Context, target *domain.ResolvedTarget, data any, err error, latencyMs int64) (*mcp.CallToolResult, any, error) {
	setCallAuditError(ctx, err)
	return errorResultWithData(target, data, err, latencyMs)
}

func (s *Server) registerTools() {
	// Group 1 — Session & Account Governance (4 tools)
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "colab_get_session_context",
		Description: "Returns the currently resolved GoLab Project, Account Profile, locked Session state, Notebook URL, permission level, and Compute Units summary for this MCP stream.",
	}, s.colabGetSessionContext)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "colab_list_all_sessions",
		Description: "Lists all GoLab Projects, Account Profiles, and Notebook Sessions managed by the daemon along with their Per-Project Lock status and connection states.",
	}, s.colabListAllSessions)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "colab_open_session_tab",
		Description: "Ensures the Account's Chrome profile is running and opens/focuses the Colab Notebook browser tab for the resolved or specified locked session, refreshing identity and permissions.",
	}, s.colabOpenSessionTab)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "colab_get_account_quota",
		Description: "Queries live Google Colab Compute Units balance, hourly burn rate, hardware accelerator eligibility (GPUs/TPUs), and active VM assignments across the account.",
	}, s.colabGetAccountQuota)

	// Group 2 — Runtime & Kernel Lifecycle (8 tools)
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "colab_get_kernel_status",
		Description: "Returns real-time status of all Colab kernels on the VM: execution state ('idle', 'busy', 'starting'), active notebook session, and connection count.",
	}, s.colabGetKernelStatus)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "colab_connect_runtime",
		Description: "Connects the Colab notebook to a hosted Google Colab VM runtime and waits until the kernel is ready.",
	}, s.colabConnectRuntime)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "colab_disconnect_runtime",
		Description: "Unassigns and releases the active Colab VM runtime (confirming Colab's disconnect dialog) to stop Compute Unit consumption.",
	}, s.colabDisconnectRuntime)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "colab_interrupt_kernel",
		Description: "Sends an immediate SIGINT interrupt signal to stop a running cell while preserving variables in Python RAM.",
	}, s.colabInterruptKernel)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "colab_restart_kernel",
		Description: "Restarts the Python kernel, clearing stalled processes and freeing GPU VRAM.",
	}, s.colabRestartKernel)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "colab_get_available_accelerators",
		Description: "Inspects Colab runtime configuration to return available hardware accelerators (CPU, T4, L4, A100, TPU) and High-RAM eligibility for this account.",
	}, s.colabGetAvailableAccelerators)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "colab_set_accelerator",
		Description: "Configures the notebook's hardware accelerator (e.g. 'T4', 'L4', 'A100', 'NONE') and High-RAM setting via Colab's runtime settings.",
	}, s.colabSetAccelerator)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "colab_exec_python",
		Description: "Executes silent out-of-band Python code without adding or mutating notebook cells; automatically uses a sidecar Python kernel when the primary notebook kernel is busy.",
	}, s.colabExecPython)

	// Group 3 — Telemetry & Google Drive Mount (5 tools)
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "colab_get_hardware_telemetry",
		Description: "Retrieves instant non-blocking VM telemetry via /api/colab/resources: GPU VRAM, utilization, system RAM, and SSD disk usage.",
	}, s.colabGetHardwareTelemetry)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "colab_get_compute_units",
		Description: "Fetches live Colab Compute Units balance, hourly burn rate, and eligible GPUs/TPUs via paidResourceMonitor.fetchCcuInfo().",
	}, s.colabGetComputeUnits)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "colab_check_drive_status",
		Description: "Verifies ground-truth Google Drive FUSE mount (/proc/mounts fuse.drive), distinguishing real Drive mounts from unmounted shadow local directories on ephemeral SSD.",
	}, s.colabCheckDriveStatus)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "colab_mount_drive",
		Description: "Mounts Google Drive at /content/drive using Colab's native two-path FUSE workflow (sole-authored auto-confirm vs shared-notebook OAuth popup detection).",
	}, s.colabMountDrive)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "colab_get_build_info",
		Description: "Retrieves Google Colab VM container image release tags and runtime build metadata.",
	}, s.colabGetBuildInfo)

	// Group 4 — Storage: VM SSD & MyDrive (7 tools)
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "colab_list_directory",
		Description: "Lists files and subdirectories via /api/contents (non-blocking during training), with ground-truth FUSE Drive verification on content/drive/... paths.",
	}, s.colabListDirectory)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "colab_stat_path",
		Description: "Inspects existence, type, size, writability, and modification timestamp of a path on VM SSD or mounted Google Drive without downloading file contents.",
	}, s.colabStatPath)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "colab_read_file",
		Description: "Reads a text or base64 file from Colab VM SSD or mounted Google Drive via /api/contents without blocking the training kernel.",
	}, s.colabReadFile)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "colab_write_file",
		Description: "Atomically creates or overwrites a text or base64 file on Colab VM SSD or verified Google Drive FUSE mount.",
	}, s.colabWriteFile)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "colab_create_directory",
		Description: "Creates a directory on Colab VM SSD or mounted Google Drive.",
	}, s.colabCreateDirectory)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "colab_move_file",
		Description: "Renames or moves a file or directory on Colab VM SSD or mounted Google Drive.",
	}, s.colabMoveFile)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "colab_delete_file",
		Description: "Deletes a file or directory on Colab VM SSD or mounted Google Drive.",
	}, s.colabDeleteFile)

	// Group 5 — Notebook DOM & Cell Precision Editing (9 tools)
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "colab_get_notebook_outline",
		Description: "Returns a compact structural outline of all notebook cells (index, cell_id, type, line count, and first-line summary).",
	}, s.colabGetNotebookOutline)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "colab_get_cells",
		Description: "Fetches code and markdown cells in the given index range, optionally including outputs.",
	}, s.colabGetCells)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "colab_get_cell_with_lines",
		Description: "Returns a single cell's source annotated with 1-based line numbers alongside its raw source and outputs.",
	}, s.colabGetCellWithLines)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "colab_search_cells",
		Description: "Searches across all notebook cells for a case-insensitive substring and returns matching cell IDs, indices, and 1-based line numbers.",
	}, s.colabSearchCells)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "colab_add_cell",
		Description: "Inserts a new 'code' or 'markdown' cell at the specified 0-based index (requires Owner or Editor permission).",
	}, s.colabAddCell)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "colab_update_cell",
		Description: "Replaces the full content of an existing cell by cell_id (requires Owner or Editor permission).",
	}, s.colabUpdateCell)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "colab_edit_cell_lines",
		Description: "Surgically replaces a 1-based inclusive line range [start_line, end_line] inside a cell without rewriting unchanged lines.",
	}, s.colabEditCellLines)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "colab_move_cell",
		Description: "Moves an existing cell to a new 0-based index position in the notebook.",
	}, s.colabMoveCell)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "colab_delete_cell",
		Description: "Deletes a cell from the notebook by cell_id (requires Owner or Editor permission).",
	}, s.colabDeleteCell)

	// Group 6 — Cell Execution, Outputs & Persistence (5 tools)
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "colab_run_code_cell",
		Description: "Executes a code cell by cell_id on the notebook runtime.",
	}, s.colabRunCodeCell)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "colab_run_all_cells",
		Description: "Schedules sequential execution of all code cells in the notebook.",
	}, s.colabRunAllCells)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "colab_get_running_cells",
		Description: "Inspects live cell execution states, elapsed durations, and streaming stdout previews without blocking on kernel execution.",
	}, s.colabGetRunningCells)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "colab_get_cell_output",
		Description: "Extracts structured stdout, stderr, text results, tracebacks, and PNG image count for a cell, optionally exporting base64 PNG plots to save_images_dir.",
	}, s.colabGetCellOutput)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "colab_save_notebook",
		Description: "Flushes all notebook cell edits and outputs to Google Drive (Ctrl+S equivalent; requires Owner or Editor permission).",
	}, s.colabSaveNotebook)

	// Group 7 — Out-of-Band Background Jobs & Port Proxy (5 tools)
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "colab_start_background_job",
		Description: "Starts a detached background shell process on the Colab VM via sidecar kernel (/tmp/.golab_jobs/), working even while the primary training cell is busy.",
	}, s.colabStartBackgroundJob)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "colab_poll_background_job",
		Description: "Polls PID liveness, exit code, and log tail lines of a background job via sidecar kernel without interrupting training.",
	}, s.colabPollBackgroundJob)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "colab_kill_background_job",
		Description: "Sends SIGTERM/SIGKILL to a background job process group on the Colab VM and returns its final status.",
	}, s.colabKillBackgroundJob)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "colab_proxy_forward",
		Description: "Forwards an HTTP request to an internal port running on the Colab VM through Colab's /_proxy/<port>/ gateway.",
	}, s.colabProxyForward)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "colab_get_proxy_url",
		Description: "Retrieves the external HTTPS proxy URL and authentication token for a port listening on the Colab VM.",
	}, s.colabGetProxyURL)
}
