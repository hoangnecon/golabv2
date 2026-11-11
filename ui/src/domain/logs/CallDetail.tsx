import { Copy } from 'lucide-react';
import type { ActivityLog, Payload } from '@/api/types';
import { useActivityDetail } from '@/data/useActivityDetail';
import { useWorkspace } from '@/data/useWorkspace';
import { NONE, formatBytes, formatLatency, formatTimestamp } from '@/shared/lib/format';
import { paths } from '@/shared/lib/paths';
import { useNotice } from '@/shared/notice/NoticeProvider';
import { CodeBlock } from '@/shared/ui/CodeBlock';
import { FormError } from '@/shared/ui/Field';
import { IconButton } from '@/shared/ui/IconButton';
import { PropertyList } from '@/shared/ui/PropertyList';
import { Status } from '@/shared/ui/StatusDot';

function payloadText(payload: Payload): string {
  if (payload.truncated && typeof payload.body === 'string') return payload.body;
  return JSON.stringify(payload.body, null, 2) ?? '';
}

function extractEnvelopeErrorHint(payload: Payload | null): string | null {
  if (!payload || payload.truncated || typeof payload.body !== 'object' || payload.body === null) {
    return null;
  }
  const rec = payload.body as Record<string, unknown>;
  const errObj = rec.error;
  if (typeof errObj === 'object' && errObj !== null) {
    const hint = (errObj as Record<string, unknown>).hint;
    if (typeof hint === 'string' && hint.trim()) return hint.trim();
  }
  return null;
}

interface PayloadPanelProps {
  title: 'Request JSON' | 'Response JSON';
  payload: Payload | null;
}

function PayloadPanel({ title, payload }: PayloadPanelProps) {
  const { notify, notifyError } = useNotice();
  const text = payload ? payloadText(payload) : '';

  const copy = () => {
    navigator.clipboard.writeText(text).then(
      () => notify(`${title} copied`),
      () => notifyError('Clipboard is not available'),
    );
  };

  return (
    <section aria-label={title} className="min-w-0 overflow-hidden rounded-lg border border-line bg-canvas">
      <header className="flex h-9 items-center gap-2 border-b border-line pl-3 pr-2">
        <h3 className="font-mono text-caption font-medium text-fg">{title}</h3>
        {payload && (
          <span className="font-mono text-caption text-subtle">
            {payload.truncated
              ? `First ${formatBytes(new TextEncoder().encode(text).length)} of ${formatBytes(payload.bytes)}`
              : formatBytes(payload.bytes)}
          </span>
        )}
        {payload && (
          <IconButton label={`Copy ${title}`} size="sm" onClick={copy} className="ml-auto">
            <Copy aria-hidden className="size-4" strokeWidth={1.75} />
          </IconButton>
        )}
      </header>
      {payload ? (
        <CodeBlock code={text} plain={payload.truncated} label={title} />
      ) : (
        <p className="p-3 text-caption text-subtle">Payload not recorded</p>
      )}
    </section>
  );
}

/**
 * Right Split JSON Payload Inspector for a single MCP tool invocation.
 * Displays call metadata, structured error code + remediation hint, and full Request/Response JSON.
 */
export function CallDetail({ entry }: { entry: ActivityLog }) {
  const state = useActivityDetail(entry.id);
  const { projectById, accountById, sessionById } = useWorkspace();

  const project = projectById(entry.project_id);
  const account = accountById(entry.profile_id);
  const session = sessionById(entry.session_id);
  const responseHint = state.status === 'ready' ? extractEnvelopeErrorHint(state.detail.response) : null;

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <div className="flex flex-col gap-1 border-b border-line pb-3">
        <div className="flex items-center justify-between gap-2">
          <Status
            tone={entry.status === 'error' ? 'error' : 'solid'}
            label={entry.status === 'error' ? entry.error_code || 'Error' : 'Success'}
          />
          <span className="font-mono text-caption text-subtle">#{entry.id}</span>
        </div>
        <div className="break-all font-mono text-body font-semibold text-fg">{entry.tool_name}</div>
      </div>

      <div className="rounded-lg border border-line bg-canvas px-3">
        <PropertyList
          items={[
            { term: 'Time', value: <span className="font-mono text-caption">{formatTimestamp(entry.created_at)}</span> },
            { term: 'Latency', value: <span className="font-mono text-caption">{formatLatency(entry.latency_ms)}</span> },
            {
              term: 'Project',
              value: project ? (
                <a href={paths.project(project.id)} className="truncate hover:underline">
                  {project.name}
                </a>
              ) : (
                <span className="font-mono text-caption text-subtle">{entry.project_id || NONE}</span>
              ),
            },
            {
              term: 'Account',
              value: account ? (
                <a href={paths.account(account.id)} className="truncate hover:underline">
                  {account.name}
                </a>
              ) : (
                <span className="font-mono text-caption text-subtle">{entry.profile_id || NONE}</span>
              ),
            },
            {
              term: 'Session',
              value: (
                <span className="truncate font-mono text-caption">
                  {session ? session.name : entry.session_id || NONE}
                </span>
              ),
            },
            {
              term: 'Token',
              value: <span className="font-mono text-caption">{entry.token_prefix || NONE}</span>,
            },
          ]}
        />
      </div>

      {entry.status === 'error' && (
        <div role="alert" className="flex flex-col gap-1.5 rounded-lg border border-line-strong bg-raised p-3">
          <div className="font-mono text-caption font-semibold text-fg">
            ▪ {entry.error_code || 'TOOL_CALL_ERROR'}
          </div>
          <p className="m-0 break-words font-mono text-caption text-fg">
            {entry.error_message || 'Tool execution failed'}
          </p>
          {responseHint && <p className="m-0 break-words text-caption text-muted">Hint: {responseHint}</p>}
        </div>
      )}

      {state.status === 'loading' && <p className="text-caption text-subtle">Loading captured payloads…</p>}
      {state.status === 'error' && <FormError>{state.message}</FormError>}
      {state.status === 'ready' &&
        (state.detail.has_payload ? (
          <div className="flex flex-col gap-3">
            <PayloadPanel title="Request JSON" payload={state.detail.request} />
            <PayloadPanel title="Response JSON" payload={state.detail.response} />
          </div>
        ) : (
          <p className="text-caption text-subtle">Payloads were not recorded for this call.</p>
        ))}
    </div>
  );
}
