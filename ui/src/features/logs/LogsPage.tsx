import { Activity, ArrowDown, Pause, Play } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import type { ActivityLog } from '@/api/types';
import { useControlPlane } from '@/data/ControlPlaneProvider';
import { CallDetail } from '@/domain/logs/CallDetail';
import { CallsTable } from '@/domain/logs/CallsTable';
import { Page } from '@/shared/layout/Page';
import { useLocation } from '@/shared/lib/location';
import type { RouteProps } from '@/shared/lib/location';
import { Button } from '@/shared/ui/Button';
import { EmptyState } from '@/shared/ui/EmptyState';

function mergeById(a: ActivityLog[], b: ActivityLog[]): ActivityLog[] {
  const byId = new Map<number, ActivityLog>();
  for (const item of a) byId.set(item.id, item);
  for (const item of b) byId.set(item.id, item);
  return [...byId.values()].sort((x, y) => y.id - x.id);
}

/**
 * Global MCP Audit Logs (`#/logs`):
 * - Faceted filters in the Contextual Sidebar (`LogsNav`) persisted in URL query params.
 * - Live SSE `activity.appended` stream with `Pause / Resume`.
 * - Cursor pagination (`Load older`) via `GET /api/v1/activity?cursor=...`.
 * - Right Split JSON Payload Inspector (`CallDetail`) on row click.
 */
export function LogsPage(_props: RouteProps) {
  const { activity, recentLogsCount, fetchActivityPage } = useControlPlane();
  const { params } = useLocation();

  const [frozen, setFrozen] = useState<ActivityLog[] | null>(null);
  const [olderPages, setOlderPages] = useState<ActivityLog[]>([]);
  const [nextCursor, setNextCursor] = useState<number | null>(null);
  const [loadingOlder, setLoadingOlder] = useState(false);
  const [selectedCallId, setSelectedCallId] = useState<number | null>(null);

  const q = (params.get('q') ?? '').trim().toLowerCase();
  const project = params.get('project') ?? '';
  const account = params.get('account') ?? '';
  const session = params.get('session') ?? '';
  const status = params.get('status') ?? '';
  const tool = params.get('tool') ?? '';

  // Reset cursor state and load filtered server page whenever server-side facets change
  useEffect(() => {
    let cancelled = false;
    setOlderPages([]);
    setNextCursor(null);

    void fetchActivityPage({
      limit: 50,
      project_id: project || undefined,
      profile_id: account || undefined,
      session_id: session || undefined,
      status: status || undefined,
      tool: tool || undefined,
    }).then((page) => {
      if (cancelled || !page) return;
      setOlderPages(page.activity);
      setNextCursor(page.next_cursor > 0 ? page.next_cursor : 0);
    });

    return () => {
      cancelled = true;
    };
  }, [project, account, session, status, tool, fetchActivityPage]);

  const combined = useMemo(() => {
    const liveSource = frozen ?? activity;
    return mergeById(liveSource, olderPages);
  }, [frozen, activity, olderPages]);

  const visible = useMemo(() => {
    return combined.filter(
      (entry) =>
        (!project || entry.project_id === project) &&
        (!account || entry.profile_id === account) &&
        (!session || entry.session_id === session) &&
        (!status || entry.status === status) &&
        (!tool || entry.tool_name === tool) &&
        (!q ||
          entry.tool_name.toLowerCase().includes(q) ||
          entry.error_code.toLowerCase().includes(q) ||
          entry.error_message.toLowerCase().includes(q) ||
          entry.token_prefix.toLowerCase().includes(q)),
    );
  }, [combined, project, account, session, status, tool, q]);

  const selectedCall =
    selectedCallId !== null ? visible.find((c) => c.id === selectedCallId) : undefined;

  async function handleLoadOlder() {
    const cursor =
      nextCursor && nextCursor > 0
        ? nextCursor
        : visible.length > 0
          ? visible[visible.length - 1]!.id
          : 0;
    if (cursor <= 0) return;
    setLoadingOlder(true);
    const page = await fetchActivityPage({
      cursor,
      limit: 50,
      project_id: project || undefined,
      profile_id: account || undefined,
      session_id: session || undefined,
      status: status || undefined,
      tool: tool || undefined,
    });
    if (page) {
      setOlderPages((prev) => mergeById(prev, page.activity));
      setNextCursor(page.next_cursor);
    }
    setLoadingOlder(false);
  }

  const canLoadOlder =
    nextCursor !== null ? nextCursor > 0 : combined.length > 0 && combined.length < recentLogsCount;

  return (
    <Page
      trail={[{ label: 'Audit Logs' }]}
      meta={
        combined.length > 0
          ? `${visible.length} shown · ${Math.max(recentLogsCount, combined.length)} total`
          : undefined
      }
      action={
        <Button
          size="sm"
          variant={frozen ? 'primary' : 'secondary'}
          icon={frozen ? Play : Pause}
          onClick={() => setFrozen(frozen ? null : combined)}
        >
          {frozen ? 'Resume live stream' : 'Pause stream'}
        </Button>
      }
      inspector={selectedCall ? <CallDetail entry={selectedCall} /> : undefined}
      inspectorTitle={selectedCall ? `Call #${selectedCall.id}` : 'Payload inspector'}
      onCloseInspector={() => setSelectedCallId(null)}
    >
      <div className="flex flex-col gap-4">
        {visible.length === 0 ? (
          <EmptyState
            icon={Activity}
            message={
              combined.length === 0
                ? 'No MCP tool calls recorded yet. Calls made over HTTP /mcp or golab mcp appear here instantly.'
                : 'No MCP tool calls match the active filters in the sidebar.'
            }
          />
        ) : (
          <>
            <CallsTable
              entries={visible}
              selectedId={selectedCallId}
              onSelect={(entry) =>
                setSelectedCallId((prev) => (prev === entry.id ? null : entry.id))
              }
            />
            {canLoadOlder && (
              <div className="flex justify-center pt-1">
                <Button
                  size="sm"
                  variant="secondary"
                  icon={ArrowDown}
                  disabled={loadingOlder}
                  onClick={() => void handleLoadOlder()}
                >
                  {loadingOlder ? 'Loading older calls…' : 'Load older calls'}
                </Button>
              </div>
            )}
          </>
        )}
      </div>
    </Page>
  );
}
