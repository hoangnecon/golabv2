# GOLAB v2 AGENT SKILL HARNESS SPECIFICATION

**System Role**: Google Colab Direct-Tunnel Multi-Account Control Plane & MCP Controller  
**Protocol Version**: `v2.0.0`  
**Target Platform**: Google Colab Container Environments via Singleton Control-Plane Daemon (`127.0.0.1:9120`), Chrome DevTools Protocol (CDP), Colab Tunnel Gateway (`kernel.getKernelXhr()`), and Sidecar Jupyter Kernels (`POST /api/kernels` + `/channels` WebSocket).

---

## 1. Executive Summary & Control-Plane Architecture

GoLab v2 operates as a **Singleton Control-Plane Daemon** (`golab daemon`) listening on `127.0.0.1:9120`. Every IDE client connects either directly over **MCP Streamable HTTP** (`http://127.0.0.1:9120/mcp?token=<glp_|gls_>`) or via `golab mcp --token <glp_|gls_>` (which auto-spawns the singleton daemon if down and bridges `stdio` ⇄ HTTP).

```
[ Cursor / Claude / Antigravity ]
       │
       ├── MCP Streamable HTTP: http://127.0.0.1:9120/mcp?token=glp_...
       └── MCP Stdio Bridge:    golab mcp --token glp_...
                                      │
                                      ▼
                     [ GoLab Singleton Daemon (:9120) ]
                     ├─ SQLite WAL (~/.golab/state.db)
                     ├─ Per-Project Single-Account Exclusive Lock
                     ├─ Typed SSE Bus (/api/v1/events) + Audit Capture
                     └─ Read-Only Monochrome Web UI (/)
                                      │
                       CDP WebSocket (per Chrome Account Profile)
                                      ▼
                     [ Isolated Chrome Profile (:9222, :9223, ...) ]
                                      │
                       Colab Tunnel Gateway (kernel.getKernelXhr)
                                      ▼
                     [ Google Colab Container VM ]
                     ├─ colab-fileshim.py (:3453) — non-blocking /api/contents/*
                     ├─ Primary Notebook Kernel (/api/sessions + /api/kernels)
                     ├─ Sidecar Python 3 Kernel (when primary kernel is busy)
                     ├─ /api/colab/resources + /api/colab/drive + /api/colab/build-info
                     └─ Reverse Proxy (/_proxy/<port>/)
```

### Multi-Project & Multi-Account Governance Model

1. **Account (`Profile`)**: An isolated Chrome `--user-data-dir` + `--remote-debugging-port` signed into one Google Account, tracking live **Compute Units (`CCU`)**, burn rate (`CU/h`), hardware eligibility (`T4`, `L4`, `A100`, `G4`, `V5E1`, `V6E1`), and active Colab VMs (`kernel.listNotebookSessions()`).
2. **Project (`Notebook`)**: `1 Project = 1 Canonical Google Colab Notebook` (`drive/<fileId>`), shared with Editor permission across multiple Google Accounts for quota/GPU rotation.
3. **Session (`(Project, Account)` Binding)**: Represents opening that Project's canonical notebook inside a specific Account's Chrome profile.
4. **Per-Project Single-Account Exclusive Lock**: At most **one** Account Session may hold the lock on a Project (`projects.locked_session_id`) at any moment. Switching the lock (`force_switch=true`) flushes `SaveNotebook` on the previous Account and closes its Colab tab before opening the new Account's tab—preventing multi-account Google Drive merge conflicts while allowing **multiple distinct Projects** to run concurrently in parallel.
5. **Deterministic Token Routing**:
   - **Project Token (`glp_<32-char>`) — Recommended**: Routes automatically to whichever Account Session currently holds the Project's exclusive lock. When a user switches accounts in the GoLab UI (e.g., when Account A runs out of Compute Units), the IDE agent seamlessly continues on Account B without reconnecting.
   - **Session Token (`gls_<32-char>`)**: Pins strictly to a single `(Project, Account)` Session; fails closed with `GOLAB_SESSION_NOT_LOCKED` if unlocked.

---

## 2. Core Operational Directives & Guardrails

Agents operating GoLab v2 MUST obey the following rules:

### Rule 1: Session Context & Permission Verification
1. Call `colab_get_session_context` at session start (or `colab_list_all_sessions` when inspecting available Projects/Accounts) to confirm the locked `project_id`, `session_id`, `profile_id`, `permission` (`owner`, `editor`, `commenter`, `view_only`, `denied`, `auth_required`), and `compute_units`.
2. When `permission` is `view_only` or `commenter`, read/observation tools succeed, while mutating tools (`colab_add_cell`, `colab_update_cell`, `colab_edit_cell_lines`, `colab_move_cell`, `colab_delete_cell`, `colab_save_notebook`, `colab_mount_drive`) fail closed with `GOLAB_PERMISSION_READ_ONLY`.

### Rule 2: Ground-Truth Google Drive FUSE Verification & Mount
- **Shadow Directory Trap**: Creating `/content/drive/MyDrive` via `os.makedirs` writes to ephemeral local SSD without mounting Google Drive, causing silent data loss on VM reset.
- **Mandatory Procedure**:
  1. Before reading or writing anything under `content/drive`, call `colab_check_drive_status` (`verify_fuse=true`). GoLab verifies `fuse.drive` in `/proc/mounts` and `os.path.ismount('/content/drive')`.
  2. If `status == "corrupted_local_dir"` (`GOLAB_DRIVE_LOCAL_SHADOW_DIR`), move or clean up the unmounted local shadow files before mounting.
  3. If `mounted == false`, call `colab_mount_drive`:
     - **Sole-authored notebook (`isSoleAuthored === true`)**: GoLab clicks `#mount-drive-button` + `<mwc-dialog>` confirmation (`dfs_persistent`) automatically and verifies FUSE + disk quota.
     - **Shared multi-account notebook (`isSoleAuthored === false`)**: GoLab triggers `dfs_ephemeral`, inspects CDP popup targets for `accounts.google.com`, auto-clicks consent when possible, or returns `GOLAB_DRIVE_OAUTH_INTERACTION_REQUIRED` with popup metadata so the user can approve 2FA/OAuth in one click.

### Rule 3: Out-of-Band Non-Blocking File I/O
- Always use `colab_list_directory`, `colab_stat_path`, `colab_read_file`, `colab_write_file`, `colab_create_directory`, `colab_move_file`, and `colab_delete_file` instead of writing Python cells for file transfers.
- These hit `colab-fileshim.py` (`/api/contents/*`) directly over `kernelXhr` in **20ms–50ms** without blocking or waiting behind long-running training cells.

### Rule 4: Sidecar-Backed Background Jobs & Python Probes
- Google Colab container VMs return `404` on Jupyter's legacy `POST /api/terminals` endpoint. Those broken terminal tools have been permanently replaced.
- For out-of-band shell commands, data downloads, or monitoring while a foreground cell is busy:
  - Use `colab_start_background_job` (`command`, `cwd`), `colab_poll_background_job` (`job_id`, `tail_lines`), and `colab_kill_background_job` (`job_id`), which execute via a sidecar Python 3 kernel (`POST /api/kernels` + `/channels` WebSocket) and stream logs to `/tmp/.golab_jobs/<job_id>.log`.
  - Use `colab_exec_python` (`prefer_sidecar_when_busy=true`) for quick environment checks without queueing behind busy notebook cells.

### Rule 5: Pre-Run Hardware & Compute Units Check
- Before starting GPU training, call `colab_get_hardware_telemetry` (GPU model, free VRAM MB, host RAM GB, disk usage %) and `colab_get_compute_units` (`balance`, `consumption_rate_hourly`, `eligible_gpus`).

### Rule 6: Surgical Line-Range Cell Editing
- Never overwrite a large cell to change a few lines. Call `colab_get_notebook_outline` → `colab_get_cell_with_lines` → `colab_edit_cell_lines` (`start_line`, `end_line` 1-based inclusive).

---

## 3. Standardized `ResponseEnvelope` Schema

All **43** GoLab v2 MCP tools return a uniform JSON `ResponseEnvelope`:

```json
{
  "status": "success",
  "data": { ... },
  "metadata": {
    "latency_ms": 28,
    "project_id": "2c84e1b0-...",
    "session_id": "9a14d2f0-...",
    "profile_id": "default",
    "kernel_state": "idle",
    "permission": "owner",
    "timestamp": "2026-11-11T03:30:00.000000000Z"
  }
}
```

Error envelope (with actionable `hint`):

```json
{
  "status": "error",
  "error": {
    "code": "GOLAB_PERMISSION_READ_ONLY",
    "message": "session has \"view_only\" permission; write/mutation operations are blocked",
    "hint": "Grant Editor access on Google Drive or switch lock to an Editor/Owner account."
  },
  "metadata": {
    "latency_ms": 14,
    "timestamp": "2026-11-11T03:30:01.000000000Z"
  }
}
```

### Canonical Error Codes

| Error Code | Meaning |
| :--- | :--- |
| `GOLAB_INVALID_TOKEN` | Missing, malformed, or rotated `glp_` / `gls_` token. |
| `GOLAB_PROJECT_UNLOCKED` | Target Project has no locked Account Session (`locked_session_id == ""`). |
| `GOLAB_SESSION_NOT_LOCKED` | Target Session does not hold its Project's exclusive lock. |
| `GOLAB_PROJECT_LOCKED_BY_OTHER_ACCOUNT` | Lock acquisition attempted without `force_switch=true` while another Account holds the lock. |
| `GOLAB_AMBIGUOUS_MULTI_PROJECT_SESSION` | Multiple Projects are locked concurrently and no `glp_`/`gls_` token was supplied. |
| `GOLAB_NO_LOCKED_SESSION` | Zero Projects are locked and no `session_id` was provided. |
| `GOLAB_AUTH_REQUIRED` | Account Chrome tab redirected to `accounts.google.com` sign-in. |
| `GOLAB_PERMISSION_DENIED` | Account lacks Google Drive access (`You need access`). |
| `GOLAB_PERMISSION_READ_ONLY` | Write tool invoked on a `view_only` or `commenter` session. |
| `GOLAB_BROWSER_STOPPED` | Account's Chrome process is not running. |
| `GOLAB_TAB_CLOSED` | Notebook tab is closed in the Account's browser. |
| `GOLAB_KERNEL_DISCONNECTED` | Colab VM runtime is disconnected (`colab_connect_runtime` needed). |
| `GOLAB_DRIVE_UNMOUNTED` | File I/O targeted `content/drive/*` while Google Drive FUSE is unmounted. |
| `GOLAB_DRIVE_LOCAL_SHADOW_DIR` | `/content/drive` contains local files without an active `fuse.drive` mount. |
| `GOLAB_DRIVE_OAUTH_INTERACTION_REQUIRED` | Shared-notebook Drive mount opened an OAuth consent/2FA popup requiring user approval. |

---

## 4. Complete 43 Canonical MCP Tools Matrix

Every session-scoped tool accepts an optional `session_id` override (otherwise resolved automatically from the connection's `glp_` / `gls_` token).

### Group 1: Session, Account & Governance Context (4 Tools)
| Tool Name | Parameters | Description |
| :--- | :--- | :--- |
| `colab_get_session_context` | `session_id?` | Returns the resolved Project, locked Session, Account Profile, permission level, and live Compute Units. |
| `colab_list_all_sessions` | `project_id?` | Lists all registered Projects, Account Profiles, and `(Project, Account)` Sessions (works even when unlocked). |
| `colab_open_session_tab` | `session_id?` | Launches the Account's Chrome profile if stopped and opens/focuses the canonical notebook tab. |
| `colab_get_account_quota` | `profile_id?`, `session_id?` | Returns live Compute Units (`balance`, `consumption_rate_hourly`), eligible GPUs/TPUs, and active Colab VMs (`listNotebookSessions`). |

### Group 2: Runtime & Kernel Lifecycle (8 Tools)
| Tool Name | Parameters | Description |
| :--- | :--- | :--- |
| `colab_get_kernel_status` | `session_id?` | Returns layered connection state (`idle`, `busy`, `runtime_disconnected`), kernel ID, and Jupyter sessions. |
| `colab_connect_runtime` | `session_id?` | Allocates and connects the Colab hosted VM runtime. |
| `colab_disconnect_runtime` | `session_id?` | Disconnects and releases the Colab VM to stop Compute Unit burn immediately. |
| `colab_interrupt_kernel` | `kernel_id?`, `session_id?` | Sends out-of-band `SIGINT` via `/api/kernels/<id>/interrupt`, preserving Python RAM state. |
| `colab_restart_kernel` | `kernel_id?`, `session_id?` | Restarts the kernel via `/api/kernels/<id>/restart` and clears GPU VRAM. |
| `colab_get_available_accelerators` | `session_id?` | Lists available CPU/GPU/TPU accelerators (`T4`, `L4`, `A100`, `G4`, `V5E1`, `V6E1`) and High-RAM options. |
| `colab_set_accelerator` | `accelerator`, `high_ram?`, `session_id?` | Switches runtime hardware accelerator (`none`, `T4`, `L4`, `A100`, etc.) and memory shape. |
| `colab_exec_python` | `code`, `prefer_sidecar_when_busy?`, `session_id?` | Executes Python silently on the primary kernel when idle, or on a sidecar kernel when busy, without adding cells. |

### Group 3: Hardware, Compute Units & Google Drive FUSE (5 Tools)
| Tool Name | Parameters | Description |
| :--- | :--- | :--- |
| `colab_get_hardware_telemetry` | `session_id?` | Fetches VM RAM, disk usage, and GPU VRAM/utilization from `/api/colab/resources`. |
| `colab_get_compute_units` | `session_id?` | Queries `paidResourceMonitor.fetchCcuInfo()` for exact Compute Units balance, hourly burn rate, tier, and GPU/TPU eligibility. |
| `colab_check_drive_status` | `verify_fuse?`, `session_id?` | Verifies real `fuse.drive` mount in `/proc/mounts`, quota (`total_gb`, `used_gb`, `free_gb`), and detects `corrupted_local_dir` shadow folders. |
| `colab_mount_drive` | `session_id?` | Executes two-path Google Drive FUSE mount (`sole_authored_persistent` vs `shared_ephemeral` with OAuth popup watcher). |
| `colab_get_build_info` | `session_id?` | Returns Colab container `release_tag` via `/api/colab/build-info`. |

### Group 4: Non-Blocking Storage & Filesystem via `colab-fileshim` (7 Tools)
| Tool Name | Parameters | Description |
| :--- | :--- | :--- |
| `colab_list_directory` | `path?`, `session_id?` | Lists directory entries with sizes, types, and timestamps via `/api/contents/*` (guarded against unmounted Drive). |
| `colab_stat_path` | `path`, `session_id?` | Inspects file/directory existence, size, and modification timestamp without downloading body bytes. |
| `colab_read_file` | `path`, `format?` (`text`\|`base64`), `session_id?` | Reads text or binary files out-of-band even while a training cell is running. |
| `colab_write_file` | `path`, `content`, `format?`, `session_id?` | Atomically writes text or base64 binary files on VM SSD or mounted Google Drive. |
| `colab_create_directory` | `path`, `session_id?` | Creates a directory on VM SSD or mounted Google Drive. |
| `colab_move_file` | `old_path`, `new_path`, `session_id?` | Moves or renames files and directories via `/api/contents/*` `PATCH`. |
| `colab_delete_file` | `path`, `session_id?` | Deletes a file or directory on VM SSD or mounted Google Drive. |

### Group 5: Notebook Cell Inspection & Surgical Editing (9 Tools)
| Tool Name | Parameters | Description |
| :--- | :--- | :--- |
| `colab_get_notebook_outline` | `session_id?` | Returns skeletal notebook outline (`index`, `cell_id`, `cell_type`, `line_count`, first-line summary). |
| `colab_get_cells` | `start_index?`, `end_index?`, `include_outputs?`, `session_id?` | Fetches full cell sources (and optional outputs) across a 0-based index range. |
| `colab_get_cell_with_lines` | `cell_id`, `session_id?` | Returns a cell's source formatted with 1-based line numbers for exact slicing. |
| `colab_search_cells` | `query`, `session_id?` | Searches case-insensitively across all notebook cells with line numbers and snippets. |
| `colab_add_cell` | `index`, `cell_type` (`code`\|`markdown`), `content`, `session_id?` | Inserts a new code or markdown cell at `index` (`owner`/`editor` required). |
| `colab_update_cell` | `cell_id`, `content`, `session_id?` | Replaces the complete source of `cell_id` (`owner`/`editor` required). |
| `colab_edit_cell_lines` | `cell_id`, `start_line`, `end_line`, `new_content`, `session_id?` | Replaces 1-based inclusive `[start_line, end_line]` inside `cell_id` (`owner`/`editor` required). |
| `colab_move_cell` | `cell_id`, `new_index`, `session_id?` | Moves `cell_id` to `new_index` (`owner`/`editor` required). |
| `colab_delete_cell` | `cell_id`, `session_id?` | Deletes `cell_id` from the notebook (`owner`/`editor` required). |

### Group 6: Notebook Execution & Persistence (5 Tools)
| Tool Name | Parameters | Description |
| :--- | :--- | :--- |
| `colab_run_code_cell` | `cell_id`, `session_id?` | Executes a single code cell via Colab's native frontend execution service and returns structured output. |
| `colab_run_all_cells` | `session_id?` | Schedules sequential execution of all notebook cells. |
| `colab_get_running_cells` | `only_active?`, `session_id?` | Inspects currently running cells, elapsed execution seconds, and live streaming `stdout`. |
| `colab_get_cell_output` | `cell_id`, `save_images_dir?`, `session_id?` | Extracts `stdout`, `stderr`, Python tracebacks, and optionally decodes inline PNG plots to `save_images_dir`. |
| `colab_save_notebook` | `session_id?` | Commits all notebook edits and outputs to Google Drive (`nb.saveNotebook()`, `owner`/`editor` required). |

### Group 7: Sidecar Background Jobs & VM Reverse Proxy (5 Tools)
| Tool Name | Parameters | Description |
| :--- | :--- | :--- |
| `colab_start_background_job` | `command`, `cwd?`, `session_id?` | Spawns a detached background process via sidecar kernel, logging output to `/tmp/.golab_jobs/<job_id>.log`. |
| `colab_poll_background_job` | `job_id`, `tail_lines?`, `session_id?` | Polls PID liveness, exit code, elapsed seconds, byte count, and trailing log lines of `job_id`. |
| `colab_kill_background_job` | `job_id`, `session_id?` | Terminates `job_id`'s process group (`SIGTERM` → `SIGKILL`). |
| `colab_proxy_forward` | `port`, `subpath?`, `method?`, `headers?`, `body?`, `session_id?` | Forwards an authenticated HTTP request through `/_proxy/<port>/<subpath>` inside the Colab VM. |
| `colab_get_proxy_url` | `port`, `session_id?` | Resolves the external `https://<port>-m-s-....prod.r.colab.dev` URL via `kernel.proxyPort(port)`. |

---

## 5. Automated 6-Gate Verification Suite

Every change to GoLab v2 MUST pass `./scripts/verify.sh` (`make verify`), which enforces all 6 automated gates:

1. **Gate 1 (AST Clean Architecture)**: `internal/domain` has zero internal imports; `store`, `process`, `bridge`, `driver`, `session`, `service`, `api`, and `mcpserver` strictly obey Clean Architecture dependency boundaries.
2. **Gate 2 (Go `-race` Unit & Integration Tests)**: SQLite WAL schema/locks, two-path Drive FUSE & sidecar jobs, session token routing, and 43-tool MCP audit capture.
3. **Gate 3 (Go ↔ TypeScript API Contract)**: `node scripts/check-api-contract.mjs` statically verifies every UI `endpoints.ts` route and `types.ts` DTO against `internal/api/server.go` and `internal/domain/*.go`.
4. **Gate 4 (Strict `R = G = B` Monochrome & UI Boundaries)**: `ui/scripts/check-design.mjs` bans every non-neutral colour (`R ≠ G` or `G ≠ B`), `setInterval` polling, and cross-feature imports.
5. **Gate 5 (TypeScript, Vite Bundle & Embedded Go Binary)**: `tsc --noEmit` + `vite build` + `go build ./cmd/server`.
6. **Gate 6 (Real Binary + Headless Chrome E2E)**: `go test -race -count=1 -v ./e2e/...` drives the compiled `golab` binary, 2-Account Mock CDP servers, concurrent HTTP + `stdio` MCP clients, and real Headless Chrome (`getComputedStyle` `R === G === B` walk across every visible DOM element in Light and Dark themes).
