import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useReducer,
} from 'react';
import type { ReactNode } from 'react';
import { api, normalizeSnapshot } from '@/api/endpoints';
import { errorMessage } from '@/api/http';
import type {
  AccountQuota,
  ActivityFilterParams,
  ActivityLog,
  ActivityPage,
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
  WorkspaceEvent,
  WorkspaceSnapshot,
} from '@/api/types';
import { useNotice } from '@/shared/notice/NoticeProvider';
import { useEventStream } from './useEventStream';

interface State {
  profiles: Profile[];
  projects: Project[];
  sessions: Session[];
  activity: ActivityLog[];
  recentLogsCount: number;
  observations: Record<string, ObservationSnapshot>;
  quotas: Record<string, AccountQuota>;
  daemonVersion: string;
  mcpHttpUrl: string;
  loaded: boolean;
  online: boolean;
}

type Action =
  | { type: 'snapshot'; snapshot: WorkspaceSnapshot }
  | { type: 'profile_upserted'; profile: Profile }
  | { type: 'profile_deleted'; id: string }
  | { type: 'project_upserted'; project: Project }
  | { type: 'project_deleted'; id: string }
  | { type: 'session_upserted'; session: Session }
  | { type: 'session_deleted'; id: string; projectId?: string }
  | { type: 'telemetry_upserted'; observation: ObservationSnapshot }
  | { type: 'quota_upserted'; quota: AccountQuota }
  | { type: 'activity_merged'; entries: ActivityLog[]; incrementTotal?: boolean }
  | { type: 'loaded' }
  | { type: 'online'; online: boolean };

const MAX_ACTIVITY_BUFFER = 800;

const initialState: State = {
  profiles: [],
  projects: [],
  sessions: [],
  activity: [],
  recentLogsCount: 0,
  observations: {},
  quotas: {},
  daemonVersion: '',
  mcpHttpUrl: 'http://127.0.0.1:9120/mcp',
  loaded: false,
  online: true,
};

function upsertById<T extends { id: string }>(items: T[], next: T): T[] {
  const index = items.findIndex((item) => item.id === next.id);
  if (index === -1) return [...items, next];
  const copy = [...items];
  copy[index] = next;
  return copy;
}

function mergeActivity(current: ActivityLog[], incoming: ActivityLog[]): ActivityLog[] {
  const byId = new Map<number, ActivityLog>();
  for (const row of current) byId.set(row.id, row);
  for (const row of incoming) byId.set(row.id, row);
  return [...byId.values()].sort((a, b) => b.id - a.id).slice(0, MAX_ACTIVITY_BUFFER);
}

function syncSessionsLockForProject(sessions: Session[], project: Project): Session[] {
  return sessions.map((sess) => {
    if (sess.project_id !== project.id) return sess;
    const shouldLock = project.locked_session_id === sess.id;
    return sess.is_locked === shouldLock ? sess : { ...sess, is_locked: shouldLock };
  });
}

function syncProjectsLockForSession(projects: Project[], session: Session): Project[] {
  return projects.map((proj) => {
    if (proj.id !== session.project_id) return proj;
    if (session.is_locked && proj.locked_session_id !== session.id) {
      return { ...proj, locked_session_id: session.id };
    }
    if (!session.is_locked && proj.locked_session_id === session.id) {
      return { ...proj, locked_session_id: '' };
    }
    return proj;
  });
}

function reducer(state: State, action: Action): State {
  switch (action.type) {
    case 'snapshot': {
      const snap = action.snapshot;
      return {
        ...state,
        profiles: snap.profiles,
        projects: snap.projects,
        sessions: snap.sessions,
        activity: mergeActivity(state.activity, snap.recent_activity),
        recentLogsCount: Math.max(snap.recent_logs_count, snap.recent_activity.length),
        daemonVersion: snap.daemon_version || state.daemonVersion,
        mcpHttpUrl: snap.mcp_http_url || state.mcpHttpUrl,
        loaded: true,
      };
    }
    case 'profile_upserted': {
      const normalized: Profile = {
        ...action.profile,
        eligible_gpus: action.profile.eligible_gpus ?? [],
        eligible_tpus: action.profile.eligible_tpus ?? [],
      };
      return { ...state, profiles: upsertById(state.profiles, normalized) };
    }
    case 'profile_deleted': {
      const nextQuotas = { ...state.quotas };
      delete nextQuotas[action.id];
      return {
        ...state,
        profiles: state.profiles.filter((p) => p.id !== action.id),
        sessions: state.sessions.filter((s) => s.profile_id !== action.id),
        quotas: nextQuotas,
      };
    }
    case 'project_upserted': {
      const nextProjects = upsertById(state.projects, action.project);
      return {
        ...state,
        projects: nextProjects,
        sessions: syncSessionsLockForProject(state.sessions, action.project),
      };
    }
    case 'project_deleted': {
      return {
        ...state,
        projects: state.projects.filter((p) => p.id !== action.id),
        sessions: state.sessions.filter((s) => s.project_id !== action.id),
      };
    }
    case 'session_upserted': {
      const sess = action.session;
      const nextSessions = upsertById(state.sessions, sess).map((item) => {
        if (sess.is_locked && item.project_id === sess.project_id && item.id !== sess.id && item.is_locked) {
          return { ...item, is_locked: false };
        }
        return item;
      });
      return {
        ...state,
        sessions: nextSessions,
        projects: syncProjectsLockForSession(state.projects, sess),
      };
    }
    case 'session_deleted': {
      const nextObs = { ...state.observations };
      delete nextObs[action.id];
      return {
        ...state,
        sessions: state.sessions.filter((s) => s.id !== action.id),
        projects: state.projects.map((p) =>
          p.locked_session_id === action.id ? { ...p, locked_session_id: '' } : p,
        ),
        observations: nextObs,
      };
    }
    case 'telemetry_upserted': {
      const obs = api.normalizeObservation(action.observation);
      return {
        ...state,
        observations: { ...state.observations, [obs.session_id]: obs },
      };
    }
    case 'quota_upserted': {
      return {
        ...state,
        quotas: { ...state.quotas, [action.quota.profile_id]: action.quota },
      };
    }
    case 'activity_merged': {
      const hadIds = new Set(state.activity.map((r) => r.id));
      let newlyAdded = 0;
      for (const row of action.entries) {
        if (!hadIds.has(row.id)) newlyAdded += 1;
      }
      return {
        ...state,
        activity: mergeActivity(state.activity, action.entries),
        recentLogsCount: action.incrementTotal
          ? state.recentLogsCount + newlyAdded
          : Math.max(state.recentLogsCount, state.activity.length + newlyAdded),
      };
    }
    case 'loaded':
      return { ...state, loaded: true };
    case 'online':
      return state.online === action.online ? state : { ...state, online: action.online };
  }
}

export interface ControlPlaneActions {
  refreshWorkspace: () => Promise<void>;
  // Form actions reject on failure so dialogs display inline FormError
  createProfile: (input: NewProfileInput) => Promise<Profile>;
  updateProfile: (id: string, input: ProfilePatchInput) => Promise<Profile>;
  createProject: (input: NewProjectInput) => Promise<Project>;
  updateProject: (id: string, input: ProjectPatchInput) => Promise<Project>;
  createSession: (input: NewSessionInput) => Promise<Session>;
  // Row governance actions handle errors via notice and return boolean / nullable result
  deleteProfile: (id: string) => Promise<boolean>;
  launchProfile: (id: string, initialUrl?: string) => Promise<boolean>;
  stopProfile: (id: string) => Promise<boolean>;
  refreshQuota: (id: string, silent?: boolean) => Promise<AccountQuota | null>;
  deleteProject: (id: string, force?: boolean) => Promise<boolean>;
  rotateProjectToken: (id: string) => Promise<Project | null>;
  unlockProject: (id: string) => Promise<boolean>;
  deleteSession: (id: string, closeTab?: boolean) => Promise<boolean>;
  lockSession: (id: string, input?: LockSessionInput) => Promise<Session | null>;
  unlockSession: (id: string) => Promise<boolean>;
  openSessionTab: (id: string) => Promise<boolean>;
  rotateSessionToken: (id: string) => Promise<Session | null>;
  fetchSessionObservation: (id: string, silent?: boolean) => Promise<ObservationSnapshot | null>;
  fetchActivityPage: (filter?: ActivityFilterParams) => Promise<ActivityPage | null>;
}

export type ControlPlane = State & ControlPlaneActions;

const ControlPlaneContext = createContext<ControlPlane | null>(null);

export function ControlPlaneProvider({ children }: { children: ReactNode }) {
  const { notify, notifyError } = useNotice();
  const [state, dispatch] = useReducer(reducer, initialState);

  const refreshWorkspace = useCallback(async () => {
    const snap = await api.system.workspace();
    dispatch({ type: 'snapshot', snapshot: snap });
  }, []);

  useEffect(() => {
    let cancelled = false;
    api.system
      .workspace()
      .then((snap) => {
        if (!cancelled) dispatch({ type: 'snapshot', snapshot: snap });
      })
      .catch((err: unknown) => {
        if (!cancelled) notifyError(errorMessage(err));
      })
      .finally(() => {
        if (!cancelled) dispatch({ type: 'loaded' });
      });
    return () => {
      cancelled = true;
    };
  }, [notifyError]);

  const onStreamEvent = useCallback((evt: WorkspaceEvent) => {
    switch (evt.type) {
      case 'workspace.snapshot':
        if (evt.data && typeof evt.data === 'object') {
          dispatch({
            type: 'snapshot',
            snapshot: normalizeSnapshot(evt.data as WorkspaceSnapshot),
          });
        }
        break;
      case 'profile.updated':
        if (evt.data && typeof evt.data === 'object') {
          dispatch({ type: 'profile_upserted', profile: evt.data as Profile });
        }
        break;
      case 'profile.deleted':
        if (evt.data && typeof evt.data === 'object') {
          const rec = evt.data as { id?: string };
          if (rec.id) dispatch({ type: 'profile_deleted', id: rec.id });
        }
        break;
      case 'project.updated':
        if (evt.data && typeof evt.data === 'object') {
          dispatch({ type: 'project_upserted', project: evt.data as Project });
        }
        break;
      case 'project.deleted':
        if (evt.data && typeof evt.data === 'object') {
          const rec = evt.data as { id?: string };
          if (rec.id) dispatch({ type: 'project_deleted', id: rec.id });
        }
        break;
      case 'session.updated':
        if (evt.data && typeof evt.data === 'object') {
          dispatch({ type: 'session_upserted', session: evt.data as Session });
        }
        break;
      case 'session.deleted':
        if (evt.data && typeof evt.data === 'object') {
          const rec = evt.data as { id?: string; project_id?: string };
          if (rec.id) dispatch({ type: 'session_deleted', id: rec.id, projectId: rec.project_id });
        }
        break;
      case 'telemetry.updated':
        if (evt.data && typeof evt.data === 'object') {
          dispatch({ type: 'telemetry_upserted', observation: evt.data as ObservationSnapshot });
        }
        break;
      case 'activity.appended':
        if (evt.data && typeof evt.data === 'object') {
          dispatch({
            type: 'activity_merged',
            entries: [evt.data as ActivityLog],
            incrementTotal: true,
          });
        }
        break;
    }
  }, []);

  const onStreamStatus = useCallback((online: boolean) => {
    dispatch({ type: 'online', online });
  }, []);

  useEventStream('/api/v1/events', onStreamEvent, onStreamStatus);

  const actions = useMemo<ControlPlaneActions>(() => {
    const attempt = async (task: () => Promise<void>, successMsg?: string): Promise<boolean> => {
      try {
        await task();
        if (successMsg) notify(successMsg);
        return true;
      } catch (err) {
        notifyError(errorMessage(err));
        return false;
      }
    };

    return {
      refreshWorkspace,
      createProfile: async (input) => {
        const created = await api.profiles.create(input);
        dispatch({ type: 'profile_upserted', profile: created });
        return created;
      },
      updateProfile: async (id, input) => {
        const updated = await api.profiles.update(id, input);
        dispatch({ type: 'profile_upserted', profile: updated });
        return updated;
      },
      createProject: async (input) => {
        const res = await api.projects.create(input);
        dispatch({ type: 'project_upserted', project: res.project });
        if (res.session) {
          dispatch({ type: 'session_upserted', session: res.session });
        }
        return res.project;
      },
      updateProject: async (id, input) => {
        const updated = await api.projects.update(id, input);
        dispatch({ type: 'project_upserted', project: updated });
        return updated;
      },
      createSession: async (input) => {
        const created = await api.sessions.create(input);
        dispatch({ type: 'session_upserted', session: created });
        return created;
      },
      deleteProfile: (id) =>
        attempt(async () => {
          await api.profiles.remove(id);
          dispatch({ type: 'profile_deleted', id });
        }, 'Account removed'),
      launchProfile: (id, initialUrl) =>
        attempt(async () => {
          const updated = await api.profiles.launch(id, initialUrl);
          dispatch({ type: 'profile_upserted', profile: updated });
        }, 'Browser launched'),
      stopProfile: (id) =>
        attempt(async () => {
          const updated = await api.profiles.stop(id);
          dispatch({ type: 'profile_upserted', profile: updated });
        }, 'Browser stopped'),
      refreshQuota: async (id, silent = false) => {
        try {
          const quota = await api.profiles.quota(id);
          dispatch({ type: 'quota_upserted', quota });
          const refreshedProfiles = await api.profiles.list();
          for (const p of refreshedProfiles) {
            if (p.id === id) dispatch({ type: 'profile_upserted', profile: p });
          }
          if (!silent) notify('Account quota refreshed');
          return quota;
        } catch (err) {
          if (!silent) notifyError(errorMessage(err));
          return null;
        }
      },
      deleteProject: (id, force = true) =>
        attempt(async () => {
          await api.projects.remove(id, force);
          dispatch({ type: 'project_deleted', id });
        }, 'Project deleted'),
      rotateProjectToken: async (id) => {
        try {
          const updated = await api.projects.rotateToken(id);
          dispatch({ type: 'project_upserted', project: updated });
          notify('Project token rotated');
          return updated;
        } catch (err) {
          notifyError(errorMessage(err));
          return null;
        }
      },
      unlockProject: (id) =>
        attempt(async () => {
          const updated = await api.projects.unlock(id);
          dispatch({ type: 'project_upserted', project: updated });
        }, 'Project unlocked'),
      deleteSession: (id, closeTab = true) =>
        attempt(async () => {
          await api.sessions.remove(id, closeTab);
          dispatch({ type: 'session_deleted', id });
        }, 'Account session detached'),
      lockSession: async (id, input = { force_switch: false, open_tab: true }) => {
        try {
          const locked = await api.sessions.lock(id, input);
          dispatch({ type: 'session_upserted', session: locked });
          notify(input.force_switch ? 'Switched project lock' : 'Session locked for agent');
          return locked;
        } catch (err) {
          notifyError(errorMessage(err));
          return null;
        }
      },
      unlockSession: (id) =>
        attempt(async () => {
          const unlocked = await api.sessions.unlock(id);
          dispatch({ type: 'session_upserted', session: unlocked });
        }, 'Session unlocked'),
      openSessionTab: (id) =>
        attempt(async () => {
          const updated = await api.sessions.openTab(id);
          dispatch({ type: 'session_upserted', session: updated });
        }, 'Opened notebook tab in Chrome'),
      rotateSessionToken: async (id) => {
        try {
          const updated = await api.sessions.rotateToken(id);
          dispatch({ type: 'session_upserted', session: updated });
          notify('Session token rotated');
          return updated;
        } catch (err) {
          notifyError(errorMessage(err));
          return null;
        }
      },
      fetchSessionObservation: async (id, silent = true) => {
        try {
          const obs = await api.sessions.observe(id);
          dispatch({ type: 'telemetry_upserted', observation: obs });
          return obs;
        } catch (err) {
          if (!silent) notifyError(errorMessage(err));
          return null;
        }
      },
      fetchActivityPage: async (filter) => {
        try {
          const page = await api.activity.list(filter);
          if (!filter?.project_id && !filter?.session_id && !filter?.profile_id && !filter?.status && !filter?.tool) {
            dispatch({ type: 'activity_merged', entries: page.activity });
          }
          return page;
        } catch (err) {
          notifyError(errorMessage(err));
          return null;
        }
      },
    };
  }, [notify, notifyError, refreshWorkspace]);

  const value = useMemo<ControlPlane>(() => ({ ...state, ...actions }), [state, actions]);

  return <ControlPlaneContext.Provider value={value}>{children}</ControlPlaneContext.Provider>;
}

export function useControlPlane(): ControlPlane {
  const context = useContext(ControlPlaneContext);
  if (!context) throw new Error('useControlPlane must be used inside ControlPlaneProvider');
  return context;
}
