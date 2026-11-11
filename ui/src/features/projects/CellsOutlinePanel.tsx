import { ChevronDown } from 'lucide-react';
import type { NotebookOutline, RunningCellsReport } from '@/api/types';
import { Status, StatusDot } from '@/shared/ui/StatusDot';

interface CellsOutlinePanelProps {
  outline: NotebookOutline | undefined;
  running: RunningCellsReport | undefined;
}

/**
 * Native <details>/<summary> read-only Notebook Cells Snapshot (Spec v2 §7.2):
 * Zero Run / Edit / Save / Connect buttons — strictly live observability.
 */
export function CellsOutlinePanel({ outline, running }: CellsOutlinePanelProps) {
  const runningById = new Map((running?.running_cells ?? []).map((c) => [c.cell_id, c]));
  const items = outline?.outline ?? [];
  const activeCount = running?.total_running ?? 0;

  return (
    <details
      open={activeCount > 0}
      className="group overflow-hidden rounded-lg border border-line bg-surface"
    >
      <summary className="flex h-11 cursor-pointer select-none items-center justify-between gap-3 px-4 transition-colors hover:bg-hover">
        <div className="flex min-w-0 items-center gap-3">
          <span className="text-body font-medium text-fg">Notebook cells snapshot</span>
          <span className="font-mono text-caption text-subtle">
            {outline
              ? `${outline.total_cells} cells (${outline.code_cells} code · ${outline.markdown_cells} markdown)`
              : 'Runtime snapshot unavailable'}
          </span>
          {activeCount > 0 && <Status tone="active" label={`${activeCount} executing`} />}
        </div>
        <ChevronDown
          aria-hidden
          strokeWidth={1.75}
          className="size-4 shrink-0 text-muted transition-transform group-open:rotate-180"
        />
      </summary>

      <div className="border-t border-line">
        {items.length === 0 ? (
          <p className="m-0 p-4 text-caption text-subtle">
            Open the locked notebook tab in Chrome to inspect live cell outline and streamed stdout.
          </p>
        ) : (
          <ul className="divide-y divide-line">
            {items.map((cell) => {
              const exec = runningById.get(cell.cell_id);
              const isRunning = Boolean(exec?.is_running);
              return (
                <li key={cell.cell_id || cell.index} className="flex flex-col gap-1.5 px-4 py-2.5">
                  <div className="flex min-w-0 items-center gap-2.5">
                    <StatusDot
                      tone={isRunning ? 'active' : 'hollow'}
                      label={isRunning ? 'Executing' : 'Idle cell'}
                    />
                    <span className="shrink-0 font-mono text-caption text-subtle">
                      [{cell.index}] {cell.cell_type.toUpperCase()}
                    </span>
                    <span className="min-w-0 flex-1 truncate font-mono text-caption text-fg">
                      {cell.summary || '(empty cell)'}
                    </span>
                    <span className="shrink-0 font-mono text-caption text-subtle">
                      {isRunning && exec
                        ? `${exec.elapsed_seconds.toFixed(1)}s`
                        : `${cell.line_count} ln`}
                    </span>
                  </div>
                  {exec?.stdout && (
                    <pre
                      role="region"
                      aria-label={`Streamed stdout for cell ${cell.index}`}
                      tabIndex={0}
                      className="scroll-pane m-0 max-h-40 overflow-auto rounded border border-line bg-canvas p-2.5 font-mono text-caption text-muted whitespace-pre-wrap break-words"
                    >
                      <code>{exec.stdout}</code>
                    </pre>
                  )}
                </li>
              );
            })}
          </ul>
        )}
      </div>
    </details>
  );
}
