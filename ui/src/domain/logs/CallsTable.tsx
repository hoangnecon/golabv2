import { ChevronRight } from 'lucide-react';
import type { ActivityLog } from '@/api/types';
import { useWorkspace } from '@/data/useWorkspace';
import { cn } from '@/shared/lib/cn';
import { NONE, formatLatency, formatTimestamp } from '@/shared/lib/format';
import { paths } from '@/shared/lib/paths';
import { Status, StatusDot } from '@/shared/ui/StatusDot';
import { TBody, TableCard, Td, Th } from '@/shared/ui/Table';

interface CallsTableProps {
  entries: ActivityLog[];
  selectedId?: number | null;
  onSelect: (entry: ActivityLog) => void;
  hideProject?: boolean;
  hideAccount?: boolean;
}

/**
 * High-density monochrome MCP tool-call audit table.
 * Selecting any row opens/updates the Right Split JSON Payload Inspector.
 */
export function CallsTable({
  entries,
  selectedId,
  onSelect,
  hideProject,
  hideAccount,
}: CallsTableProps) {
  const { projectById, accountById } = useWorkspace();

  return (
    <TableCard label="MCP tool-call audit log">
      <thead>
        <tr>
          <Th className="w-36">Status</Th>
          <Th>Tool</Th>
          {!hideProject && <Th>Project</Th>}
          {!hideAccount && <Th>Account</Th>}
          <Th className="w-32">Token</Th>
          <Th className="w-32">Time</Th>
          <Th className="w-24 text-right">Latency</Th>
          <Th className="w-10 pl-0">
            <span className="sr-only">Inspect</span>
          </Th>
        </tr>
      </thead>
      <TBody>
        {entries.map((entry) => {
          const failed = entry.status === 'error';
          const selected = selectedId === entry.id;
          const project = projectById(entry.project_id);
          const account = accountById(entry.profile_id);

          return (
            <tr
              key={entry.id}
              onClick={(event) => {
                if ((event.target as HTMLElement).closest('a, button')) return;
                onSelect(entry);
              }}
              className={cn(
                'cursor-pointer transition-colors hover:bg-hover',
                selected && 'bg-hover',
              )}
            >
              <Td>
                {failed ? (
                  <Status tone="error" label={entry.error_code || 'Error'} />
                ) : (
                  <span className="inline-flex items-center gap-1.5 font-mono text-caption text-muted">
                    <StatusDot tone="solid" label="Success" />
                    <span>OK</span>
                  </span>
                )}
              </Td>
              <Td className="font-mono text-caption font-medium text-fg">{entry.tool_name}</Td>
              {!hideProject && (
                <Td className="text-muted">
                  {project ? (
                    <a href={paths.project(project.id)} className="transition-colors hover:text-fg hover:underline">
                      {project.name}
                    </a>
                  ) : (
                    NONE
                  )}
                </Td>
              )}
              {!hideAccount && (
                <Td className="text-muted">
                  {account ? (
                    <a href={paths.account(account.id)} className="transition-colors hover:text-fg hover:underline">
                      {account.name}
                    </a>
                  ) : (
                    NONE
                  )}
                </Td>
              )}
              <Td className="font-mono text-caption text-subtle">{entry.token_prefix || NONE}</Td>
              <Td className="font-mono text-caption text-muted">{formatTimestamp(entry.created_at)}</Td>
              <Td className="text-right font-mono text-caption tabular-nums text-muted">
                {formatLatency(entry.latency_ms)}
              </Td>
              <Td className="pl-0 text-right">
                <button
                  type="button"
                  aria-pressed={selected}
                  aria-label={`Inspect call #${entry.id} ${entry.tool_name}`}
                  onClick={() => onSelect(entry)}
                  className="inline-flex size-6 items-center justify-center rounded text-muted transition-colors hover:bg-raised hover:text-fg"
                >
                  <ChevronRight aria-hidden className="size-4" strokeWidth={1.75} />
                </button>
              </Td>
            </tr>
          );
        })}
      </TBody>
    </TableCard>
  );
}
