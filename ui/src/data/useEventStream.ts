import { useEffect, useRef } from 'react';
import type { WorkspaceEvent } from '@/api/types';

const RETRY_MS = 2500;

function isWorkspaceEvent(value: unknown): value is WorkspaceEvent {
  if (typeof value !== 'object' || value === null) return false;
  const rec = value as Record<string, unknown>;
  return typeof rec.type === 'string';
}

/**
 * Subscribes to GET /api/v1/events typed SSE stream. Reconnects cleanly when
 * EventSource transitions to CLOSED without using polling intervals.
 */
export function useEventStream(
  url: string,
  onEvent: (event: WorkspaceEvent) => void,
  onStatus: (online: boolean) => void,
) {
  const eventRef = useRef(onEvent);
  const statusRef = useRef(onStatus);

  useEffect(() => {
    eventRef.current = onEvent;
    statusRef.current = onStatus;
  });

  useEffect(() => {
    let source: EventSource | null = null;
    let retry: ReturnType<typeof setTimeout> | undefined;
    let disposed = false;

    const connect = () => {
      source = new EventSource(url);
      source.onopen = () => statusRef.current(true);
      source.onmessage = (frame: MessageEvent<string>) => {
        try {
          const parsed: unknown = JSON.parse(frame.data);
          if (isWorkspaceEvent(parsed)) {
            eventRef.current(parsed);
          }
        } catch {
          // Ignore non-JSON heartbeat frames.
        }
      };
      source.onerror = () => {
        statusRef.current(false);
        if (source?.readyState === EventSource.CLOSED && !disposed) {
          retry = setTimeout(connect, RETRY_MS);
        }
      };
    };

    connect();
    return () => {
      disposed = true;
      clearTimeout(retry);
      source?.close();
    };
  }, [url]);
}
