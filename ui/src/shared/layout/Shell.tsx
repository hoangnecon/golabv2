import { Moon, PanelLeftClose, PanelLeftOpen, Sun } from 'lucide-react';
import type { ReactNode } from 'react';
import { cn } from '@/shared/lib/cn';
import { IconButton, IconLink } from '@/shared/ui/IconButton';
import { StatusDot } from '@/shared/ui/StatusDot';
import { Sidebar } from './Sidebar';
import type { RailItem } from './Sidebar';
import { useShell } from './ShellProvider';

interface ShellProps {
  items: readonly RailItem[];
  activeId: RailItem['id'];
  sidebarTitle: string;
  sidebar: ReactNode;
  online: boolean;
  daemonVersion?: string;
  children: ReactNode;
}

/**
 * 3-Column Monochrome Control-Plane Shell (Spec v2 §7.1):
 *   Col 1: 56px (`w-14`) fixed Global Rail (Projects, Accounts, Audit Logs + Sidebar Toggle, SSE Link, Theme Toggle)
 *   Col 2: 240px (`w-60`) collapsible Contextual Sidebar
 *   Col 3: Fluid Main Workspace (`cq-inline` container-queried, optionally split with Right Inspector inside Page)
 */
export function Shell({
  items,
  activeId,
  sidebarTitle,
  sidebar,
  online,
  daemonVersion,
  children,
}: ShellProps) {
  const { sidebarOpen, toggleSidebar, dismissOnCompact, theme, toggleTheme } = useShell();
  const ToggleIcon = sidebarOpen ? PanelLeftClose : PanelLeftOpen;
  const ThemeIcon = theme === 'dark' ? Sun : Moon;

  return (
    <div className="flex h-dvh overflow-hidden bg-app text-fg">
      {/* Column 1: 56px Global Rail */}
      <nav
        aria-label="Global sections"
        className="z-50 flex w-14 shrink-0 flex-col items-center justify-between border-r border-line bg-app py-3"
      >
        <div className="flex flex-col items-center gap-2">
          <a
            href="#/projects"
            aria-label="GoLab Control Plane"
            title={daemonVersion ? `GoLab ${daemonVersion}` : 'GoLab Control Plane'}
            className="flex size-9 items-center justify-center rounded-lg border border-line-strong bg-surface font-mono text-caption font-semibold tracking-tight text-fg transition-colors hover:bg-hover"
          >
            GL
          </a>
          <IconButton
            label={sidebarOpen ? 'Collapse contextual sidebar (Ctrl+B)' : 'Expand contextual sidebar (Ctrl+B)'}
            onClick={toggleSidebar}
          >
            <ToggleIcon aria-hidden className="size-4" strokeWidth={1.75} />
          </IconButton>
          <div className="my-1 h-px w-6 bg-line" aria-hidden />
          <ul className="flex flex-col items-center gap-1.5">
            {items.map(({ id, label, href, icon: Icon }) => {
              const active = id === activeId;
              return (
                <li key={id}>
                  <IconLink
                    label={label}
                    href={href}
                    aria-current={active ? 'page' : undefined}
                    onClick={dismissOnCompact}
                    className={cn(
                      'size-9 rounded-lg',
                      active
                        ? 'bg-fg text-canvas hover:bg-fg/85 hover:text-canvas'
                        : 'text-muted hover:bg-hover hover:text-fg',
                    )}
                  >
                    <Icon aria-hidden className="size-4" strokeWidth={1.75} />
                  </IconLink>
                </li>
              );
            })}
          </ul>
        </div>

        <div className="flex flex-col items-center gap-2">
          <span
            className="flex size-8 items-center justify-center rounded border border-line bg-surface"
            title={online ? 'Daemon SSE stream connected' : 'Daemon SSE stream disconnected'}
          >
            <StatusDot
              tone={online ? 'solid' : 'error'}
              label={online ? 'Daemon SSE connected' : 'Daemon SSE disconnected'}
            />
          </span>
          <IconButton
            label={theme === 'dark' ? 'Switch to light monochrome theme' : 'Switch to dark monochrome theme'}
            onClick={toggleTheme}
          >
            <ThemeIcon aria-hidden className="size-4" strokeWidth={1.75} />
          </IconButton>
        </div>
      </nav>

      {/* Column 2: 240px Collapsible Contextual Sidebar */}
      <Sidebar title={sidebarTitle}>{sidebar}</Sidebar>

      {/* Column 3: Fluid Main Workspace */}
      <main className="cq-inline relative flex min-w-0 flex-1 flex-col overflow-hidden bg-canvas">
        {children}
      </main>
    </div>
  );
}
