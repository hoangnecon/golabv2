import { X } from 'lucide-react';
import { Fragment } from 'react';
import type { ReactNode } from 'react';
import { IconButton } from '@/shared/ui/IconButton';

export interface Crumb {
  label: string;
  href?: string;
}

interface PageProps {
  trail: readonly Crumb[];
  meta?: ReactNode;
  action?: ReactNode;
  /** Optional Right Split Inspector (380px–440px) for tool-call payload inspection. */
  inspector?: ReactNode;
  inspectorTitle?: string;
  onCloseInspector?: () => void;
  children: ReactNode;
}

/**
 * 56px Page Header + Fluid Workspace Body + Optional Right Split Inspector.
 */
export function Page({
  trail,
  meta,
  action,
  inspector,
  inspectorTitle = 'Inspector',
  onCloseInspector,
  children,
}: PageProps) {
  const current = trail[trail.length - 1];
  const parents = trail.slice(0, -1);

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
      <header className="flex h-14 shrink-0 items-center justify-between gap-3 border-b border-line bg-surface px-6 max-md:px-4">
        <nav aria-label="Breadcrumb" className="flex min-w-0 flex-1 items-center gap-2 text-body">
          {parents.map((crumb) => (
            <Fragment key={crumb.label}>
              <a
                href={crumb.href}
                className="max-w-44 shrink-0 truncate text-muted transition-colors hover:text-fg"
              >
                {crumb.label}
              </a>
              <span aria-hidden className="text-line-strong">
                /
              </span>
            </Fragment>
          ))}
          <h1 className="min-w-0 truncate text-title font-semibold text-fg">{current?.label}</h1>
          {meta && <div className="flex shrink-0 items-center gap-2 text-caption text-subtle">{meta}</div>}
        </nav>
        {action && <div className="flex shrink-0 items-center gap-2">{action}</div>}
      </header>

      <div className="flex min-h-0 flex-1 overflow-hidden">
        <div className="cq-inline scroll-pane min-h-0 min-w-0 flex-1 overflow-y-auto p-6 max-md:p-4">
          {children}
        </div>

        {inspector && (
          <aside
            aria-label={inspectorTitle}
            className="flex h-full w-96 shrink-0 flex-col border-l border-line bg-surface max-lg:fixed max-lg:inset-y-0 max-lg:right-0 max-lg:z-40 max-lg:w-full max-lg:max-w-md max-lg:shadow-overlay xl:w-96"
          >
            <div className="flex h-12 shrink-0 items-center justify-between border-b border-line px-4">
              <h2 className="truncate font-mono text-caption font-medium uppercase tracking-wide text-fg">
                {inspectorTitle}
              </h2>
              {onCloseInspector && (
                <IconButton size="sm" label="Close inspector" onClick={onCloseInspector}>
                  <X aria-hidden className="size-4" strokeWidth={1.75} />
                </IconButton>
              )}
            </div>
            <div className="scroll-pane min-h-0 flex-1 overflow-y-auto p-4">{inspector}</div>
          </aside>
        )}
      </div>
    </div>
  );
}
