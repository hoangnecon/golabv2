import { request } from './http';
import type {
  AccountQuota,
  ActivityDetail,
  ActivityFilterParams,
  ActivityLog,
  ActivityPage,
  CreateProjectResponse,
  HealthResponse,
  LockSessionInput,
  NewProfileInput,
  NewProjectInput,
  NewSessionInput,
  ObservationSnapshot,
  Profile,
  ProfilePatchInput,
  Project,
  ProjectPatchInput,
  Session,
  WorkspaceSnapshot,
} from './types';

function normalizeProfile(p: Profile): Profile {
  return {
    ...p,
    eligible_gpus: p.eligible_gpus ?? [],
    eligible_tpus: p.eligible_tpus ?? [],
  };
}

function normalizeQuota(q: AccountQuota): AccountQuota {
  return {
    ...q,
    active_vms: q.active_vms ?? [],
    compute_units: q.compute_units
      ? {
          ...q.compute_units,
          eligible_gpus: q.compute_units.eligible_gpus ?? [],
          ineligible_gpus: q.compute_units.ineligible_gpus ?? [],
          eligible_tpus: q.compute_units.eligible_tpus ?? [],
          ineligible_tpus: q.compute_units.ineligible_tpus ?? [],
        }
      : undefined,
  };
}

function normalizeObservation(obs: ObservationSnapshot): ObservationSnapshot {
  return {
    ...obs,
    hardware_telemetry: obs.hardware_telemetry
      ? {
          ...obs.hardware_telemetry,
          disks: obs.hardware_telemetry.disks ?? [],
          gpus: obs.hardware_telemetry.gpus ?? [],
        }
      : undefined,
    cells_outline: obs.cells_outline
      ? {
          ...obs.cells_outline,
          outline: obs.cells_outline.outline ?? [],
        }
      : undefined,
    running_cells: obs.running_cells
      ? {
          ...obs.running_cells,
          running_cells: obs.running_cells.running_cells ?? [],
        }
      : undefined,
  };
}

export function normalizeSnapshot(snap: WorkspaceSnapshot): WorkspaceSnapshot {
  return {
    ...snap,
    profiles: (snap.profiles ?? []).map(normalizeProfile),
    projects: snap.projects ?? [],
    sessions: snap.sessions ?? [],
    recent_activity: snap.recent_activity ?? [],
  };
}

const get = <T>(path: string) => request<T>(path);
const post = <T>(path: string, body?: unknown) =>
  request<T>(path, { method: 'POST', body: body !== undefined ? JSON.stringify(body) : '{}' });
const patch = <T>(path: string, body: unknown) =>
  request<T>(path, { method: 'PATCH', body: JSON.stringify(body) });
const del = <T>(path: string) => request<T>(path, { method: 'DELETE' });

const enc = encodeURIComponent;

export const api = {
  system: {
    health: () => get<HealthResponse>('/api/v1/health'),
    workspace: async (): Promise<WorkspaceSnapshot> =>
      normalizeSnapshot(await get<WorkspaceSnapshot>('/api/v1/workspace')),
  },
  profiles: {
    async list(): Promise<Profile[]> {
      const res = await get<{ profiles: Profile[] | null }>('/api/v1/profiles');
      return (res.profiles ?? []).map(normalizeProfile);
    },
    async create(input: NewProfileInput): Promise<Profile> {
      return normalizeProfile(await post<Profile>('/api/v1/profiles', input));
    },
    async update(id: string, input: ProfilePatchInput): Promise<Profile> {
      return normalizeProfile(await patch<Profile>(`/api/v1/profiles/${enc(id)}`, input));
    },
    remove: (id: string) => del<{ deleted: boolean; id: string }>(`/api/v1/profiles/${enc(id)}`),
    async launch(id: string, initialUrl?: string): Promise<Profile> {
      return normalizeProfile(
        await post<Profile>(`/api/v1/profiles/${enc(id)}/launch`, { initial_url: initialUrl ?? '' }),
      );
    },
    async stop(id: string): Promise<Profile> {
      return normalizeProfile(await post<Profile>(`/api/v1/profiles/${enc(id)}/stop`));
    },
    async quota(id: string): Promise<AccountQuota> {
      return normalizeQuota(await get<AccountQuota>(`/api/v1/profiles/${enc(id)}/quota`));
    },
  },
  projects: {
    async list(): Promise<Project[]> {
      const res = await get<{ projects: Project[] | null }>('/api/v1/projects');
      return res.projects ?? [];
    },
    get: (id: string) => get<Project>(`/api/v1/projects/${enc(id)}`),
    create: (input: NewProjectInput) => post<CreateProjectResponse>('/api/v1/projects', input),
    update: (id: string, input: ProjectPatchInput) =>
      patch<Project>(`/api/v1/projects/${enc(id)}`, input),
    remove: (id: string, force = true) =>
      del<{ deleted: boolean; id: string }>(`/api/v1/projects/${enc(id)}?force=${force ? 'true' : 'false'}`),
    rotateToken: (id: string) => post<Project>(`/api/v1/projects/${enc(id)}/rotate-token`),
    unlock: (id: string) => post<Project>(`/api/v1/projects/${enc(id)}/unlock`),
  },
  sessions: {
    async list(projectId?: string): Promise<Session[]> {
      const path = projectId
        ? `/api/v1/sessions?project_id=${enc(projectId)}`
        : '/api/v1/sessions';
      const res = await get<{ sessions: Session[] | null }>(path);
      return res.sessions ?? [];
    },
    create: (input: NewSessionInput) => post<Session>('/api/v1/sessions', input),
    remove: (id: string, closeTab = true) =>
      del<{ deleted: boolean; id: string }>(
        `/api/v1/sessions/${enc(id)}?close_tab=${closeTab ? 'true' : 'false'}`,
      ),
    lock: (id: string, input: LockSessionInput = { force_switch: false, open_tab: true }) =>
      post<Session>(`/api/v1/sessions/${enc(id)}/lock`, input),
    unlock: (id: string) => post<Session>(`/api/v1/sessions/${enc(id)}/unlock`),
    openTab: (id: string) => post<Session>(`/api/v1/sessions/${enc(id)}/open-tab`),
    rotateToken: (id: string) => post<Session>(`/api/v1/sessions/${enc(id)}/rotate-token`),
    async observe(id: string): Promise<ObservationSnapshot> {
      return normalizeObservation(await get<ObservationSnapshot>(`/api/v1/sessions/${enc(id)}/observe`));
    },
  },
  activity: {
    async list(filter: ActivityFilterParams = {}): Promise<ActivityPage> {
      const params = new URLSearchParams();
      if (filter.cursor && filter.cursor > 0) params.set('cursor', String(filter.cursor));
      if (filter.limit && filter.limit > 0) params.set('limit', String(filter.limit));
      if (filter.project_id) params.set('project_id', filter.project_id);
      if (filter.session_id) params.set('session_id', filter.session_id);
      if (filter.profile_id) params.set('profile_id', filter.profile_id);
      if (filter.status) params.set('status', filter.status);
      if (filter.tool) params.set('tool', filter.tool);
      const qs = params.toString();
      const path = qs ? `/api/v1/activity?${qs}` : '/api/v1/activity';
      const res = await get<{ activity: ActivityLog[] | null; next_cursor: number }>(path);
      return {
        activity: res.activity ?? [],
        next_cursor: res.next_cursor ?? 0,
      };
    },
    get: (id: number) => get<ActivityDetail>(`/api/v1/activity/${id}`),
  },
  normalizeObservation,
} as const;
