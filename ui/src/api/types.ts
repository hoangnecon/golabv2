export type Tier = 'free' | 'pro' | 'pro_plus';

export type PermissionLevel =
  | 'owner'
  | 'editor'
  | 'commenter'
  | 'view_only'
  | 'denied'
  | 'auth_required'
  | 'unknown';

export type ConnectionState =
  | 'browser_stopped'
  | 'tab_closed'
  | 'auth_required'
  | 'permission_denied'
  | 'runtime_disconnected'
  | 'connecting'
  | 'idle'
  | 'busy';

export type WorkspaceEventType =
  | 'workspace.snapshot'
  | 'profile.updated'
  | 'profile.deleted'
  | 'project.updated'
  | 'project.deleted'
  | 'session.updated'
  | 'session.deleted'
  | 'telemetry.updated'
  | 'activity.appended';

export interface Profile {
  id: string;
  name: string;
  account_email: string;
  display_name: string;
  tier: Tier;
  ccu_balance: number;
  ccu_burn_rate: number;
  eligible_gpus: string[];
  eligible_tpus: string[];
  last_telemetry_at?: string;
  user_data_dir: string;
  cdp_port: number;
  chrome_pid: number;
  is_running: boolean;
  created_at: string;
  updated_at: string;
}

export interface ComputeUnitsInfo {
  assignments_count: number;
  balance: number;
  consumption_rate_hourly: number;
  formatted_balance: string;
  tier: Tier;
  eligible_gpus: string[];
  ineligible_gpus: string[];
  eligible_tpus: string[];
  ineligible_tpus: string[];
}

export interface ColabVMSession {
  file_id: string;
  title: string;
  kernel_id: string;
  session_id: string;
  accelerator: string;
  accelerator_model: string;
  shape: string;
  managed_id: string;
  proxy_url?: string;
  token_expires_sec?: number;
}

export interface AccountQuota {
  profile_id: string;
  account_email: string;
  display_name: string;
  is_running_chrome: boolean;
  compute_units?: ComputeUnitsInfo;
  active_vms: ColabVMSession[];
  observed_at: string;
}

export interface Project {
  id: string;
  name: string;
  notebook_id: string;
  notebook_url: string;
  owner_profile_id: string;
  project_token: string;
  locked_session_id: string;
  created_at: string;
  updated_at: string;
}

export interface Session {
  id: string;
  project_id: string;
  profile_id: string;
  name: string;
  notebook_id: string;
  notebook_url: string;
  session_token: string;
  tab_id: string;
  is_locked: boolean;
  permission: PermissionLevel;
  connection_state: ConnectionState;
  created_at: string;
  last_attached_at?: string;
}

export interface NotebookIdentity {
  email_address: string;
  display_name: string;
  is_authenticated: boolean;
  drive_file_id?: string;
  title: string;
  role: string;
  permission: PermissionLevel;
  editable: boolean;
  saveable: boolean;
  is_sole_authored: boolean;
  has_other_writers: boolean;
  has_merge_conflict: boolean;
  has_unsaved_edits: boolean;
  accelerator_name?: string;
  runtime_uptime_seconds?: number;
  runtime_shape?: string;
}

export interface RAMTelemetry {
  total_bytes: number;
  free_bytes: number;
  used_bytes: number;
  total_gb: number;
  free_gb: number;
  used_gb: number;
}

export interface DiskTelemetry {
  label: string;
  total_bytes: number;
  used_bytes: number;
  total_gb: number;
  used_gb: number;
  used_percent: number;
}

export interface GPUTelemetry {
  name: string;
  memory_used_bytes: number;
  memory_total_bytes: number;
  memory_used_mb: number;
  memory_total_mb: number;
  memory_free_mb: number;
  gpu_utilization: number;
  memory_utilization: number;
  ever_used: boolean;
}

export interface HardwareTelemetry {
  ram: RAMTelemetry;
  disks: DiskTelemetry[];
  gpus: GPUTelemetry[];
}

export interface DriveStatus {
  mounted: boolean;
  status: string;
  mount_path: string;
  is_fuse_verified: boolean;
  button_state?: string;
  credentials_ok: boolean;
  total_bytes?: number;
  used_bytes?: number;
  free_bytes?: number;
  total_gb?: number;
  used_gb?: number;
  free_gb?: number;
  message: string;
  action_required?: string;
  auth_popup_tab_id?: string;
  auth_popup_url?: string;
}

export interface OutlineItem {
  index: number;
  cell_id: string;
  cell_type: string;
  summary: string;
  line_count: number;
}

export interface NotebookOutline {
  total_cells: number;
  code_cells: number;
  markdown_cells: number;
  outline: OutlineItem[];
}

export interface RunningCellInfo {
  index: number;
  cell_id: string;
  is_running: boolean;
  elapsed_seconds: number;
  execution_count: string;
  stdout: string;
}

export interface RunningCellsReport {
  has_running_cells: boolean;
  total_running: number;
  running_cells: RunningCellInfo[];
}

export interface ProxyEndpoint {
  port: number;
  url: string;
  token?: string;
  token_expires_sec?: number;
}

export interface ObservationSnapshot {
  session_id: string;
  project_id: string;
  profile_id: string;
  is_locked: boolean;
  connection_state: ConnectionState;
  permission: PermissionLevel;
  notebook_metadata?: NotebookIdentity;
  compute_units?: ComputeUnitsInfo;
  hardware_telemetry?: HardwareTelemetry;
  drive_status?: DriveStatus;
  cells_outline?: NotebookOutline;
  running_cells?: RunningCellsReport;
  proxy_info?: ProxyEndpoint;
  observed_at: string;
}

export interface ActivityLog {
  id: number;
  session_id: string;
  project_id: string;
  profile_id: string;
  token_prefix: string;
  tool_name: string;
  status: 'success' | 'error';
  error_code: string;
  error_message: string;
  latency_ms: number;
  has_payload: boolean;
  created_at: string;
}

export interface Payload {
  body: unknown;
  bytes: number;
  truncated: boolean;
}

export interface ActivityDetail extends ActivityLog {
  request: Payload | null;
  response: Payload | null;
}

export interface ActivityFilterParams {
  cursor?: number;
  limit?: number;
  project_id?: string;
  session_id?: string;
  profile_id?: string;
  status?: string;
  tool?: string;
}

export interface ActivityPage {
  activity: ActivityLog[];
  next_cursor: number;
}

export interface WorkspaceSnapshot {
  profiles: Profile[];
  projects: Project[];
  sessions: Session[];
  recent_activity: ActivityLog[];
  recent_logs_count: number;
  daemon_version: string;
  mcp_http_url: string;
  timestamp: string;
}

export interface WorkspaceEvent {
  type: WorkspaceEventType;
  timestamp: string;
  data: unknown;
}

export interface HealthResponse {
  status: string;
  pid: number;
  version: string;
  uptime_sec: number;
  db_mode: string;
  mcp_http_url: string;
}

export interface NewProfileInput {
  name: string;
  account_email?: string;
  tier: Tier;
}

export interface ProfilePatchInput {
  name?: string;
  account_email?: string;
  tier?: Tier;
}

export interface NewProjectInput {
  name?: string;
  notebook_url: string;
  owner_profile_id: string;
  lock?: boolean;
}

export interface ProjectPatchInput {
  name?: string;
  owner_profile_id?: string;
}

export interface CreateProjectResponse {
  project: Project;
  session: Session | null;
}

export interface NewSessionInput {
  project_id: string;
  profile_id: string;
  name?: string;
  lock?: boolean;
}

export interface LockSessionInput {
  force_switch?: boolean;
  open_tab?: boolean;
}
