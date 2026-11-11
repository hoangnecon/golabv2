import type { LucideIcon } from 'lucide-react';
import type { ReactNode } from 'react';
import { cn } from '@/shared/lib/cn';
import { useShell } from './ShellProvider';

export interface RailItem {
  id: 'projects' | 'accounts' | 'logs';
  label: string;
  href: string;
  icon: LucideIcon;
}

export const ROW = 'flex h-8 items-center rounded transition-colors';

interface SidebarProps {
  title: string;
  children: ReactNode;
}

/**
 * Column 2 — Contextual Collapsible Sidebar (240px = w-60 when open, 0px + inert when closed).
 * Sits immediately beside the 56px Global Rail on desktop and overlays with scrim on compact viewports.
 */
export function Sidebar({ title, children }: SidebarProps) {
  const { sidebarOpen, toggleSidebar } = useShell();

  return (
    <>
      {sidebarOpen && (
        <div
          aria-hidden
          className="fixed inset-y-0 left-14 right-0 z-30 bg-scrim md:hidden"
          onClick={toggleSidebar}
        />
      )}
      <aside
        aria-label={`${title} navigation`}
        inert={!sidebarOpen}
        className={cn(
          'shrink-0 overflow-hidden border-r border-line bg-app transition-all duration-200 max-md:fixed max-md:inset-y-0 max-md:left-14 max-md:z-40',
          sidebarOpen ? 'w-60' : 'w-0 border-r-0',
        )}
      >
        <div className="flex h-full w-60 flex-col">
          <div className="scroll-pane min-h-0 flex-1 overflow-y-auto p-3">{children}</div>
        </div>
      </aside>
    </>
  );
}
