import type { ReactNode } from 'react';
import { cn } from '@/shared/lib/cn';

/**
 * Detail-page body: 2/3 main column + 1/3 right rail at xl.
 */
export function Detail({ main, rail }: { main: ReactNode; rail: ReactNode }) {
  return (
    <div className="grid grid-cols-1 items-start gap-x-6 gap-y-8 xl:grid-cols-3">
      <div className="flex min-w-0 flex-col gap-8 xl:col-span-2">{main}</div>
      <div className="flex min-w-0 flex-col gap-8 max-xl:order-first">{rail}</div>
    </div>
  );
}

interface SectionProps {
  title: string;
  count?: number;
  action?: ReactNode;
  children: ReactNode;
}

/** Heading row (32px) + content. */
export function Section({ title, count, action, children }: SectionProps) {
  return (
    <section className="flex min-w-0 flex-col gap-2.5">
      <div className="flex h-8 items-center justify-between gap-2">
        <h2 className="flex min-w-0 items-center gap-2 text-body font-semibold text-fg">
          <span className="truncate">{title}</span>
          {count !== undefined && <span className="font-mono text-caption font-normal tabular-nums text-subtle">{count}</span>}
        </h2>
        {action && <div className="flex shrink-0 items-center gap-2">{action}</div>}
      </div>
      {children}
    </section>
  );
}

export function Panel({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={cn('overflow-hidden rounded-lg border border-line bg-surface', className)}>{children}</div>;
}

export function PanelBody({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={cn('px-4', className)}>{children}</div>;
}

export function PanelActions({ children, separated }: { children: ReactNode; separated?: boolean }) {
  return (
    <div className={cn('flex flex-col gap-0.5 p-2', separated && 'border-t border-line')}>{children}</div>
  );
}

export interface Metric {
  label: string;
  value: ReactNode;
  detail?: ReactNode;
  footer?: ReactNode;
}

/**
 * Container-queried 1px-gutter metric strip (`.telemetry-grid` in index.css:
 * 1 col narrow -> 2 cols >=34rem -> 3 cols >=56rem).
 */
export function Metrics({ items }: { items: readonly Metric[] }) {
  return (
    <dl className="telemetry-grid overflow-hidden rounded-lg border border-line bg-line">
      {items.map(({ label, value, detail, footer }) => (
        <div key={label} className="flex min-w-0 flex-col gap-1 bg-surface px-4 pb-3 pt-3.5">
          <dt className="font-mono text-caption uppercase tracking-wide text-subtle">{label}</dt>
          <dd className="m-0 flex min-h-6 min-w-0 items-center truncate text-title font-semibold text-fg">
            {value}
          </dd>
          {detail && <div className="truncate font-mono text-caption text-muted">{detail}</div>}
          {footer && <div className="mt-1 flex items-center">{footer}</div>}
        </div>
      ))}
    </dl>
  );
}
