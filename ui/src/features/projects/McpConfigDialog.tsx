import { Copy, RefreshCw } from 'lucide-react';
import { useState } from 'react';
import { useControlPlane } from '@/data/ControlPlaneProvider';
import type { ProjectView } from '@/data/useWorkspace';
import { useNotice } from '@/shared/notice/NoticeProvider';
import { Button } from '@/shared/ui/Button';
import { CodeBlock } from '@/shared/ui/CodeBlock';
import { useConfirm } from '@/shared/ui/ConfirmDialog';
import { Dialog, DialogActions } from '@/shared/ui/Dialog';
import { IconButton } from '@/shared/ui/IconButton';

interface McpConfigDialogProps {
  project: ProjectView;
  onClose: () => void;
}

export function McpConfigDialog({ project, onClose }: McpConfigDialogProps) {
  const { mcpHttpUrl, rotateProjectToken } = useControlPlane();
  const { notify, notifyError } = useNotice();
  const { confirm, dialog: confirmDialog } = useConfirm();
  const [rotating, setRotating] = useState(false);

  const baseHttp = mcpHttpUrl || 'http://127.0.0.1:9120/mcp';
  const streamableUrl = `${baseHttp}?token=${project.project_token}`;
  const stdioSnippet = JSON.stringify(
    {
      mcpServers: {
        [`golab-${project.name.toLowerCase().replace(/[^a-z0-9]+/g, '-')}`]: {
          command: 'golab',
          args: ['mcp', '--token', project.project_token],
        },
      },
    },
    null,
    2,
  );

  function copy(text: string, label: string) {
    navigator.clipboard.writeText(text).then(
      () => notify(`${label} copied`),
      () => notifyError('Clipboard is not available'),
    );
  }

  function handleRotate() {
    confirm({
      title: 'Rotate project token',
      message:
        'Existing MCP clients using the current glp_ project token will be rejected until updated with the new token.',
      confirmLabel: 'Rotate token',
      onConfirm: async () => {
        setRotating(true);
        await rotateProjectToken(project.id);
        setRotating(false);
      },
    });
  }

  return (
    <>
      <Dialog title="MCP connection & project token" wide onClose={onClose}>
        <div className="flex flex-col gap-4">
          <p className="m-0 text-caption text-muted">
            Bind an AI agent exclusively to <strong>{project.name}</strong> using its Project Token (
            <code className="font-mono">glp_…</code>). Switching the active Account Lock in GoLab seamlessly routes
            all future tool calls to the newly locked Account without restarting your IDE.
          </p>

          <div className="flex flex-col gap-1.5">
            <span className="font-mono text-caption uppercase tracking-wide text-subtle">
              Project Token (glp_)
            </span>
            <div className="flex items-center gap-2 rounded border border-line-strong bg-canvas px-3 py-2">
              <code className="min-w-0 flex-1 truncate font-mono text-caption text-fg">
                {project.project_token}
              </code>
              <IconButton
                size="sm"
                label="Copy project token"
                onClick={() => copy(project.project_token, 'Project token')}
              >
                <Copy aria-hidden className="size-4" strokeWidth={1.75} />
              </IconButton>
            </div>
          </div>

          <div className="flex flex-col gap-1.5">
            <span className="font-mono text-caption uppercase tracking-wide text-subtle">
              Streamable HTTP Endpoint
            </span>
            <div className="flex items-center gap-2 rounded border border-line-strong bg-canvas px-3 py-2">
              <code className="min-w-0 flex-1 truncate font-mono text-caption text-fg">{streamableUrl}</code>
              <IconButton
                size="sm"
                label="Copy Streamable HTTP URL"
                onClick={() => copy(streamableUrl, 'Streamable HTTP URL')}
              >
                <Copy aria-hidden className="size-4" strokeWidth={1.75} />
              </IconButton>
            </div>
          </div>

          <div className="flex flex-col gap-1.5">
            <div className="flex items-center justify-between">
              <span className="font-mono text-caption uppercase tracking-wide text-subtle">
                Stdio Bridge (.mcp.json)
              </span>
              <Button size="sm" variant="ghost" icon={Copy} onClick={() => copy(stdioSnippet, 'Stdio MCP JSON')}>
                Copy JSON
              </Button>
            </div>
            <div className="overflow-hidden rounded-lg border border-line bg-canvas">
              <CodeBlock code={stdioSnippet} label="Stdio MCP configuration JSON" />
            </div>
          </div>

          <DialogActions>
            <Button
              variant="secondary"
              size="sm"
              icon={RefreshCw}
              disabled={rotating}
              onClick={handleRotate}
            >
              Rotate token
            </Button>
            <Button variant="primary" size="sm" onClick={onClose}>
              Done
            </Button>
          </DialogActions>
        </div>
      </Dialog>
      {confirmDialog}
    </>
  );
}
