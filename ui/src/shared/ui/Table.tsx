import type { ComponentProps, ReactNode } from 'react';
import { cn } from '@/shared/lib/cn';

interface TableCardProps {
  children: ReactNode;
  /** Accessible name for keyboard horizontal scrolling region. */
  label?: string;
  className?: string;
}

/**
 * Responsive data table container:
 * - Keyboard-focusable scroll region (`tabIndex={0}`, `role="region"`, `aria-label`)
 * - Inline container query context (`cq-inline`)
 * - Sticky header cells on `Th`
 */
export function TableCard({ children, label = 'Data table', className }: TableCardProps) {
  return (
    <div
      role="region"
      aria-label={label}
      tabIndex={0}
      className={cn('cq-inline scroll-pane overflow-x-auto rounded-lg border border-line bg-surface', className)}
    >
      <table className="w-full border-collapse text-left text-body">{children}</table>
    </div>
  );
}

export function TBody({ children }: { children: ReactNode }) {
  return <tbody className="divide-y divide-line">{children}</tbody>;
}

export function Th({ className, ...props }: ComponentProps<'th'>) {
  return (
    <th
      scope="col"
      className={cn(
        'sticky top-0 z-10 h-10 whitespace-nowrap border-b border-line bg-surface px-4 text-caption font-medium text-subtle',
        className,
      )}
      {...props}
    />
  );
}

export function Td({ className, ...props }: ComponentProps<'td'>) {
  return <td className={cn('h-12 whitespace-nowrap px-4 align-middle', className)} {...props} />;
}
