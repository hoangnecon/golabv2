import { useMemo } from 'react';
import type {
  AccountQuota,
  ActivityLog,
  ObservationSnapshot,
  Profile,
  Project,
  Session,
} from '@/api/types';
import { useControlPlane } from './ControlPlaneProvider';

export interface ProjectView extends Project {
  sessions: Session[];
  lockedSession: Session | undefined;
  lockedAccount: Profile | undefined;
  ownerAccount: Profile | undefined;
}

export interface Workspace {
  projects: ProjectView[];
  profiles: Profile[];
  sessions: Session[];
  activity: ActivityLog[];
  projectById: (id: string | undefined) => ProjectView | undefined;
  accountById: (id: string | undefined) => Profile | undefined;
  sessionById: (id: string | undefined) => Session | undefined;
  sessionsOfAccount: (profileId: string) => Session[];
  observationOf: (sessionId: string | undefined) => ObservationSnapshot | undefined;
  quotaOf: (profileId: string | undefined) => AccountQuota | undefined;
  callsOfProject: (projectId: string) => ActivityLog[];
  callsOfAccount: (profileId: string) => ActivityLog[];
}

/** Read-only derived workspace view joining canonical UUID projects, profiles, sessions, and audit logs. */
export function useWorkspace(): Workspace {
  const { projects, profiles, sessions, activity, observations, quotas } = useControlPlane();

  return useMemo<Workspace>(() => {
    const profileMap = new Map(profiles.map((p) => [p.id, p]));
    const sessionMap = new Map(sessions.map((s) => [s.id, s]));

    const sessionsByProject = new Map<string, Session[]>();
    for (const sess of sessions) {
      const list = sessionsByProject.get(sess.project_id);
      if (list) list.push(sess);
      else sessionsByProject.set(sess.project_id, [sess]);
    }

    const projectViews: ProjectView[] = projects.map((proj) => {
      const projSessions = sessionsByProject.get(proj.id) ?? [];
      const lockedSession =
        projSessions.find((s) => s.id === proj.locked_session_id || s.is_locked) ?? undefined;
      const lockedAccount = lockedSession ? profileMap.get(lockedSession.profile_id) : undefined;
      const ownerAccount = profileMap.get(proj.owner_profile_id);
      return {
        ...proj,
        sessions: projSessions,
        lockedSession,
        lockedAccount,
        ownerAccount,
      };
    });

    const projectMap = new Map(projectViews.map((p) => [p.id, p]));

    return {
      projects: projectViews,
      profiles,
      sessions,
      activity,
      projectById: (id) => (id ? projectMap.get(id) : undefined),
      accountById: (id) => (id ? profileMap.get(id) : undefined),
      sessionById: (id) => (id ? sessionMap.get(id) : undefined),
      sessionsOfAccount: (profileId) => sessions.filter((s) => s.profile_id === profileId),
      observationOf: (sessionId) => (sessionId ? observations[sessionId] : undefined),
      quotaOf: (profileId) => (profileId ? quotas[profileId] : undefined),
      callsOfProject: (projectId) => {
        const projSessionIds = new Set((sessionsByProject.get(projectId) ?? []).map((s) => s.id));
        return activity.filter(
          (entry) => entry.project_id === projectId || (entry.session_id && projSessionIds.has(entry.session_id)),
        );
      },
      callsOfAccount: (profileId) => {
        const acctSessionIds = new Set(
          sessions.filter((s) => s.profile_id === profileId).map((s) => s.id),
        );
        return activity.filter(
          (entry) => entry.profile_id === profileId || (entry.session_id && acctSessionIds.has(entry.session_id)),
        );
      },
    };
  }, [projects, profiles, sessions, activity, observations, quotas]);
}
