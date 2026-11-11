import { Activity, NotebookText, UserRound } from 'lucide-react';
import type { ComponentType } from 'react';
import { AccountsNav } from '@/features/accounts/AccountsNav';
import { AccountsRoute } from '@/features/accounts/AccountsRoute';
import { LogsNav } from '@/features/logs/LogsNav';
import { LogsPage } from '@/features/logs/LogsPage';
import { ProjectsNav } from '@/features/projects/ProjectsNav';
import { ProjectsRoute } from '@/features/projects/ProjectsRoute';
import type { RailItem } from '@/shared/layout/Sidebar';
import { useLocation } from '@/shared/lib/location';
import type { RouteProps } from '@/shared/lib/location';

export interface RouteEntry {
  id: 'projects' | 'accounts' | 'logs';
  label: string;
  href: string;
  icon: RailItem['icon'];
  component: ComponentType<RouteProps>;
  sidebar: ComponentType;
}

export const ROUTES: readonly RouteEntry[] = [
  {
    id: 'projects',
    label: 'Projects',
    href: '#/projects',
    icon: NotebookText,
    component: ProjectsRoute,
    sidebar: ProjectsNav,
  },
  {
    id: 'accounts',
    label: 'Accounts',
    href: '#/accounts',
    icon: UserRound,
    component: AccountsRoute,
    sidebar: AccountsNav,
  },
  {
    id: 'logs',
    label: 'Audit Logs',
    href: '#/logs',
    icon: Activity,
    component: LogsPage,
    sidebar: LogsNav,
  },
];

export const RAIL_ITEMS: readonly RailItem[] = ROUTES.map(({ id, label, href, icon }) => ({
  id,
  label,
  href,
  icon,
}));

/** Resolves `#/<section>/...rest`; unknown or empty sections fall back to Projects. */
export function useRoute(): { route: RouteEntry } & RouteProps {
  const { segments } = useLocation();
  const [section, ...rest] = segments;
  const route = ROUTES.find((candidate) => candidate.id === section);
  return route ? { route, segments: rest } : { route: ROUTES[0]!, segments: [] };
}
